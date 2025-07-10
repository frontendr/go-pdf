package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/joho/godotenv"
	"github.com/pkg/profile"
	log "github.com/sirupsen/logrus"

	"go-pdf/utils"
)

func init() {
	fmt.Println("Starting")
	err := godotenv.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error loading .env file")
		os.Exit(1)
	}
}

func setupLogging(logFile string, logLevel string) {
	parsedLogLevel, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("Invalid log level: %s", err)
	}
	log.SetLevel(parsedLogLevel)

	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(io.MultiWriter(os.Stdout, file))
		fmt.Println(" - Log file:", logFile)
	}
	fmt.Println(" - Log level:", logLevel)
}

func main() {
	// Collect environment variables
	pdfEndpoint := utils.GetEnv("PDF_ENDPOINT", "/pdf")
	listenAddress := utils.GetEnv("LISTEN_ADDRESS", "0.0.0.0:3005")
	profilingEnabled := utils.GetEnvBool("PROFILING_ENABLED", false)
	pagePoolSize := utils.GetEnvInt("PAGE_POOL_SIZE", 5)
	logLevel := utils.GetEnv("LOG_LEVEL", "info")
	logFile := utils.GetEnv("LOG_FILE", "")

	// Optionally override the settings with command line arguments:
	flag.IntVar(&pagePoolSize, "pool", pagePoolSize, "Page pool size")
	flag.StringVar(&pdfEndpoint, "path", pdfEndpoint, "PDF endpoint or path e.g. /pdf")
	flag.StringVar(&listenAddress, "host", listenAddress, "Listen address e.g. 0.0.0.0:3005")
	flag.BoolVar(&profilingEnabled, "profiling", profilingEnabled, "Enable profiling")
	flag.StringVar(&logLevel, "log", logLevel, "Log level e.g. 'debug' or 'info'")
	flag.StringVar(&logFile, "log-file", logFile, "Log file e.g. 'log.log'")
	flag.Parse()

	setupLogging(logFile, logLevel)

	// Start the optional profiler
	if profilingEnabled {
		defer profile.Start(profile.MemProfile).Stop()
	}

	browser := rod.New().MustConnect()
	defer browser.MustClose()

	pool := rod.NewPagePool(pagePoolSize)
	// pagePoolSize == cap(pool)

	createPage := func() (*rod.Page, error) {
		return browser.MustIncognito().MustPage(), nil
	}

	logPoolSize := func(logger *log.Entry) {
		logger.Debugf("Pool size: %d/%d", pagePoolSize-len(pool), pagePoolSize)
	}

	// Keep track of request IDs for logging
	lastRequestId := 0

	// Handle the root URL or any other URL
	http.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		renderTemplate(response, "templates/index.html", map[string]string{
			"PDF_ENDPOINT":   pdfEndpoint,
			"LISTEN_ADDRESS": listenAddress,
		})
	})

	// Handler for the PDF endpoint
	pdfHandler := func(response http.ResponseWriter, request *http.Request) {
		start := time.Now()

		lastRequestId++
		requestId := lastRequestId

		logger := log.WithField("request", requestId)
		logger.Infof("New request: %s %s%s", request.Method, request.Host, request.RequestURI)

		// Ensure the request method is GET or POST.
		if request.Method != "GET" && request.Method != "POST" {
			msg := fmt.Sprintf("Invalid HTTP method %s", request.Method)
			http.Error(response, msg, http.StatusBadRequest)
			logger.Warnln(msg)
			return
		}

		// Get a page from the pool
		page, err := pool.Get(createPage)
		if err != nil {
			msg := "Error creating page"
			http.Error(response, msg, http.StatusInternalServerError)
			logger.Fatalf("%s: %s", msg, err)
			return
		}
		logger.Debugf("Got page from pool after %s", time.Since(start))
		logPoolSize(logger)

		defer func() {
			// Put the page back in the pool
			pool.Put(page)
			logPoolSize(logger)
		}()

		if request.Method == "POST" {
			// The HTML to render is in the request body.
			body, err := io.ReadAll(request.Body)
			if err != nil {
				msg := "Error reading request body"
				http.Error(response, msg, http.StatusInternalServerError)
				logger.Warnf("%s: %s", msg, err)
				return
			}
			dataUrl := "data:text/html;charset=utf-8," + url.PathEscape(string(body))
			logger.Infoln("Rendering page from data URL")

			err = request.Body.Close()
			if err != nil {
				logger.Warnf("Error closing request body: %s", err)
			}
			logger.Infoln("Navigating to data URL")
			page.MustNavigate(dataUrl)
		}

		if request.Method == "GET" {
			query := request.URL.Query()
			if !query.Has("url") {
				msg := "Missing request parameter 'url'"
				logger.Warnln(msg)
				http.Error(response, msg, http.StatusBadRequest)
				return
			}

			pageUrl := query.Get("url")
			logger.Infof("Navigating to URL: %s", pageUrl)
			page.MustNavigate(pageUrl)
		}

		start = time.Now()
		page.MustWaitLoad().MustWaitStable().MustWaitIdle()
		logger.Debugf("Page loaded in %v", time.Since(start))

		pdf := pageToPDF(page, getPDFOptionsFromRequest(request), logger)
		defer func() {
			err := pdf.Close()
			if err != nil {
				logger.Warnf("Error closing PDF: %s", err)
			}
		}()

		response.Header().Set("Content-Type", "application/pdf")

		data, err := io.ReadAll(pdf)
		if err != nil {
			msg := "Error reading PDF"
			logger.Warnf("%s: %s", msg, err)
			http.Error(response, msg, http.StatusInternalServerError)
			return
		}
		logger.Debugf("PDF size: %d bytes", len(data))
		if _, err := response.Write(data); err != nil {
			msg := "Error writing PDF"
			logger.Warnf("%s: %s", msg, err)
			http.Error(response, msg, http.StatusInternalServerError)
			return
		}
		logger.Infof("Completed request after %s", time.Since(start))
	}

	http.HandleFunc(pdfEndpoint, pdfHandler)
	if !strings.HasSuffix(pdfEndpoint, "/") {
		// Also handle requests with a trailing slash:
		http.HandleFunc(pdfEndpoint+"/", pdfHandler)
	} else {
		// Also handle requests without a trailing slash:
		http.HandleFunc(pdfEndpoint[:len(pdfEndpoint)-1], pdfHandler)
	}

	fmt.Printf(" - Listening at: %s\n", listenAddress)
	fmt.Println("Usage:")
	fmt.Printf(" - GET %s?url=https://example.com to render a page\n", pdfEndpoint)
	fmt.Printf(" - POST %s with HTML to render a page\n", pdfEndpoint)
	if profilingEnabled {
		fmt.Println("Profiling enabled")
	}
	// fmt.Println("Press Ctrl+C to quit")

	err := http.ListenAndServe(listenAddress, nil)
	if errors.Is(err, http.ErrServerClosed) {
		log.Infof("server closed")
	} else if err != nil {
		log.Fatalf("error starting server: %s", err)
	}
}

func getPDFOptionsFromRequest(r *http.Request) *proto.PagePrintToPDF {
	query := r.URL.Query()

	return &proto.PagePrintToPDF{
		Scale:               utils.StringToFloat64(utils.GetQueryParam(query, "scale", "1.0")),
		PageRanges:          utils.GetQueryParam(query, "pageRanges"),
		HeaderTemplate:      utils.GetQueryParam(query, "headerTemplate", ""),
		FooterTemplate:      utils.GetQueryParam(query, "footerTemplate", ""),
		PrintBackground:     utils.GetQueryParamBool(query, "printBackground", true),
		DisplayHeaderFooter: utils.GetQueryParamBool(query, "displayHeaderFooter", false),
		MarginTop:           utils.StringToFloat64(utils.GetQueryParam(query, "marginTop", "0")),
		MarginBottom:        utils.StringToFloat64(utils.GetQueryParam(query, "marginBottom", "0")),
		MarginLeft:          utils.StringToFloat64(utils.GetQueryParam(query, "marginLeft", "0")),
		MarginRight:         utils.StringToFloat64(utils.GetQueryParam(query, "marginRight", "0")),
		PaperWidth:          utils.StringToFloat64(utils.GetQueryParam(query, "paperWidth", utils.GetQueryParam(query, "width", "8.27"))),
		PaperHeight:         utils.StringToFloat64(utils.GetQueryParam(query, "paperHeight", utils.GetQueryParam(query, "height", "11.7"))),
		Landscape:           utils.GetQueryParamBool(query, "landscape", false),
		PreferCSSPageSize:   utils.GetQueryParamBool(query, "preferCSSPageSize", false),
	}
}

func pageToPDF(page *rod.Page, pdfOptions *proto.PagePrintToPDF, logger *log.Entry) *rod.StreamReader {
	now := time.Now()

	if logger.Level <= log.DebugLevel {
		titleElement, err := page.Element("head title")
		if err == nil {
			title := titleElement.MustText()
			logger.Debugf("Printing page '%s'", title)
		} else {
			logger.Debugf("Printing page")
		}
	}

	pdf, err := page.PDF(pdfOptions)
	if err != nil {
		logger.Panicf("Error rendering PDF: %s", err)
	}
	logger.Debugf("Rendered PDF in %v", time.Since(now))

	return pdf
}

func renderTemplate(w http.ResponseWriter, templatePath string, values map[string]string) {
	indexContent, err := os.ReadFile(templatePath)
	if err != nil {
		http.Error(w, "Error reading template", http.StatusInternalServerError)
		log.Errorf("Error reading template %s: %v", templatePath, err)
		return
	}
	modifiedContent := string(indexContent)
	for key, val := range values {
		modifiedContent = strings.Replace(modifiedContent, "{{"+key+"}}", val, -1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(modifiedContent))
	if err != nil {
		log.Errorf("Error writing response: %v", err)
	}
}
