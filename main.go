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
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
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
		_, _ = fmt.Fprintln(os.Stderr, "Error loading .env file")
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
		// Create the directory if it doesn't exist
		dir := filepath.Dir(logFile)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				log.Fatalf("Failed to create log directory: %s", err)
			}
		}

		file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(io.MultiWriter(os.Stdout, file))
		fmt.Println(" - Log file:", logFile)
	}
	fmt.Println(" - Log level:", logLevel)
}

// recoverPanic is a middleware that recovers from panics and logs them
func recoverPanic(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Errorf("PANIC recovered: %v\nStack trace:\n%s", err, debug.Stack())
				errorMsg := fmt.Sprintf("Internal server error: %v", err)
				http.Error(w, errorMsg, http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}

// isClosedConnectionError checks if the error is caused by a closed network connection
func isClosedConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "use of closed network connection") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "EOF")
}

func main() {
	// Collect environment variables
	pdfEndpoint := utils.GetEnv("PDF_ENDPOINT", "/pdf")
	listenAddress := utils.GetEnv("LISTEN_ADDRESS", "0.0.0.0:3005")
	profilingEnabled := utils.GetEnvBool("PROFILING_ENABLED", false)
	pagePoolSize := utils.GetEnvInt("PAGE_POOL_SIZE", 5)
	logLevel := utils.GetEnv("LOG_LEVEL", "info")
	logFile := utils.GetEnv("LOG_FILE", "")
	browserRestartInterval := utils.GetEnvInt("BROWSER_RESTART_INTERVAL", 0) // 0 = disabled

	// Optionally override the settings with command line arguments:
	flag.IntVar(&pagePoolSize, "pool", pagePoolSize, "Page pool size")
	flag.StringVar(&pdfEndpoint, "path", pdfEndpoint, "PDF endpoint or path e.g. /pdf")
	flag.StringVar(&listenAddress, "host", listenAddress, "Listen address e.g. 0.0.0.0:3005")
	flag.BoolVar(&profilingEnabled, "profiling", profilingEnabled, "Enable profiling")
	flag.StringVar(&logLevel, "log", logLevel, "Log level e.g. 'debug' or 'info'")
	flag.StringVar(&logFile, "log-file", logFile, "Log file e.g. 'log.log'")
	flag.IntVar(&browserRestartInterval, "browser-restart", browserRestartInterval, "Browser restart interval in seconds (0 = disabled)")
	flag.Parse()

	setupLogging(logFile, logLevel)

	// Start the optional profiler
	if profilingEnabled {
		defer profile.Start(profile.MemProfile).Stop()
	}

	var browser *rod.Browser
	var browserLock sync.Mutex

	pool := rod.NewPagePool(pagePoolSize)
	// pagePoolSize == cap(pool)

	// Function to connect/reconnect browser
	connectBrowser := func() error {
		browserLock.Lock()
		defer browserLock.Unlock()

		// Drain the page pool - old pages are connected to the old browser.
		// Cleanup() removes elements from the channel but does not refill it,
		// so we need to refill it with nil entries afterwards to unblock Get().
		pool.Cleanup(func(p *rod.Page) {
			log.Debug("Closing stale page from pool")
			if err := p.Close(); err != nil {
				log.Warnf("Error closing stale page: %s", err)
			}
		})
		// Refill the pool with nil entries so Get() can create fresh pages
		for i := 0; i < cap(pool); i++ {
			select {
			case pool <- nil:
			default:
			}
		}

		if browser != nil {
			if err := browser.Close(); err != nil {
				log.Warnf("Error closing existing browser: %s", err)
			}
		}

		browser = rod.New()
		if err := browser.Connect(); err != nil {
			return fmt.Errorf("failed to connect to browser: %w", err)
		}
		return nil
	}

	// Initial browser connection
	if err := connectBrowser(); err != nil {
		log.Fatalf("Failed to connect to browser: %s", err)
	}
	defer func() {
		pool.Cleanup(func(p *rod.Page) {
			if err := p.Close(); err != nil {
				log.Warnf("Error closing page: %s", err)
			}
		})
		browserLock.Lock()
		defer browserLock.Unlock()
		if browser != nil {
			if err := browser.Close(); err != nil {
				log.Errorf("Error closing browser: %s", err)
			}
		}
	}()

	// Start periodic browser restart if configured
	if browserRestartInterval > 0 {
		fmt.Printf(" - Browser will restart every %d seconds\n", browserRestartInterval)
		go func() {
			ticker := time.NewTicker(time.Duration(browserRestartInterval) * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				log.Info("Performing scheduled browser restart")
				if err := connectBrowser(); err != nil {
					log.Errorf("Failed to restart browser: %s", err)
				} else {
					log.Info("Browser restarted successfully")
				}
			}
		}()
	}

	createPage := func() (*rod.Page, error) {
		browserLock.Lock()
		currentBrowser := browser
		browserLock.Unlock()

		if currentBrowser == nil {
			return nil, fmt.Errorf("browser not connected")
		}

		incognito, err := currentBrowser.Incognito()
		if err != nil {
			return nil, fmt.Errorf("failed to create incognito context: %w", err)
		}
		page, err := incognito.Page(proto.TargetCreateTarget{})
		if err != nil {
			return nil, fmt.Errorf("failed to create page: %w", err)
		}
		return page, nil
	}

	logPoolSize := func(logger *log.Entry) {
		logger.Debugf("Pool size: %d/%d", pagePoolSize-len(pool), pagePoolSize)
	}

	// Keep track of request IDs for logging
	lastRequestId := 0

	// Handle the root URL or any other URL
	http.HandleFunc("/", recoverPanic(func(response http.ResponseWriter, request *http.Request) {
		renderTemplate(response, "templates/index.html", map[string]string{
			"PDF_ENDPOINT":   pdfEndpoint,
			"LISTEN_ADDRESS": listenAddress,
		})
	}))

	// renderPDF handles the core PDF rendering logic. It returns the PDF data or an error.
	// If the error is a closed connection error, it returns true for the retry flag.
	renderPDF := func(page *rod.Page, navigateURL string, htmlBody []byte, request *http.Request, logger *log.Entry) ([]byte, bool, error) {
		var err error

		if htmlBody != nil {
			dataUrl := "data:text/html;charset=utf-8," + url.PathEscape(string(htmlBody))
			logger.Infoln("Navigating to data URL")
			err = page.Navigate(dataUrl)
		} else {
			logger.Infof("Navigating to URL: %s", navigateURL)
			err = page.Navigate(navigateURL)
		}

		if err != nil {
			return nil, isClosedConnectionError(err), fmt.Errorf("error navigating: %w", err)
		}

		if err = page.WaitLoad(); err != nil {
			return nil, isClosedConnectionError(err), fmt.Errorf("error waiting for page to load: %w", err)
		}
		if err = page.WaitStable(time.Second); err != nil {
			logger.Warnf("Error waiting for page to be stable: %s", err)
		}
		if err = page.WaitIdle(time.Second); err != nil {
			logger.Warnf("Error waiting for page to be idle: %s", err)
		}

		pdf := pageToPDF(page, getPDFOptionsFromRequest(request), logger)
		defer func() {
			if err := pdf.Close(); err != nil {
				logger.Warnf("Error closing PDF: %s", err)
			}
		}()

		data, err := io.ReadAll(pdf)
		if err != nil {
			return nil, isClosedConnectionError(err), fmt.Errorf("error reading PDF: %w", err)
		}

		return data, false, nil
	}

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

		// Determine navigation target
		var navigateURL string
		var htmlBody []byte

		if request.Method == "POST" {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				msg := "Error reading request body"
				http.Error(response, msg, http.StatusInternalServerError)
				logger.Warnf("%s: %s", msg, err)
				return
			}
			if err = request.Body.Close(); err != nil {
				logger.Warnf("Error closing request body: %s", err)
			}
			htmlBody = body
			logger.Infoln("Rendering page from POST body")
		}

		if request.Method == "GET" {
			query := request.URL.Query()
			if !query.Has("url") {
				msg := "Missing request parameter 'url'"
				logger.Warnln(msg)
				http.Error(response, msg, http.StatusBadRequest)
				return
			}
			navigateURL = query.Get("url")
			logger.Infoln("Rendering url")
		}

		// Try to render the PDF, with one retry on connection errors
		maxAttempts := 2
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			// Get a page from the pool
			page, err := pool.Get(createPage)
			if err != nil {
				if isClosedConnectionError(err) && attempt < maxAttempts {
					logger.Warnf("Browser connection lost while creating page, reconnecting (attempt %d/%d)", attempt, maxAttempts)
					if reconnectErr := connectBrowser(); reconnectErr != nil {
						logger.Errorf("Failed to reconnect browser: %s", reconnectErr)
					}
					continue
				}
				msg := "Error creating page"
				http.Error(response, msg, http.StatusInternalServerError)
				logger.Errorf("%s: %s", msg, err)
				return
			}
			logger.Debugf("Got page from pool after %s", time.Since(start))
			logPoolSize(logger)

			logger.Debugf("Rendering PDF (attempt %d/%d)", attempt, maxAttempts)
			data, shouldRetry, renderErr := renderPDF(page, navigateURL, htmlBody, request, logger)

			// Put the page back in the pool
			pool.Put(page)
			logPoolSize(logger)

			if renderErr != nil {
				if shouldRetry && attempt < maxAttempts {
					logger.Warnf("Browser connection lost during rendering, reconnecting and retrying (attempt %d/%d): %s", attempt, maxAttempts, renderErr)
					if reconnectErr := connectBrowser(); reconnectErr != nil {
						logger.Errorf("Failed to reconnect browser: %s", reconnectErr)
						http.Error(response, "Browser connection lost and reconnect failed", http.StatusInternalServerError)
						return
					}
					continue
				}
				http.Error(response, renderErr.Error(), http.StatusInternalServerError)
				logger.Errorf("Render failed: %s", renderErr)
				return
			}

			// Success
			response.Header().Set("Content-Type", "application/pdf")
			logger.Debugf("PDF size: %d bytes", len(data))
			if _, err := response.Write(data); err != nil {
				msg := "Error writing PDF"
				logger.Warnf("%s: %s", msg, err)
				return
			}
			logger.Infof("Completed request after %s", time.Since(start))
			return
		}
	}

	http.HandleFunc(pdfEndpoint, recoverPanic(pdfHandler))
	if !strings.HasSuffix(pdfEndpoint, "/") {
		// Also handle requests with a trailing slash:
		http.HandleFunc(pdfEndpoint+"/", recoverPanic(pdfHandler))
	} else {
		// Also handle requests without a trailing slash:
		http.HandleFunc(pdfEndpoint[:len(pdfEndpoint)-1], recoverPanic(pdfHandler))
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
			title, err := titleElement.Text()
			if err == nil {
				logger.Debugf("Printing page '%s'", title)
			} else {
				logger.Debugf("Printing page")
			}
		} else {
			logger.Debugf("Printing page")
		}
	}

	pdf, err := page.PDF(pdfOptions)
	if err != nil {
		// Log the error and panic - will be caught by recoverPanic middleware
		logger.Errorf("Error rendering PDF: %s", err)
		panic(fmt.Sprintf("Error rendering PDF: %s", err))
	}
	logger.Debugf("Rendered PDF in %v", time.Since(now))

	return pdf
}

func renderTemplate(w http.ResponseWriter, templatePath string, values map[string]string) {
	indexContent, err := os.ReadFile(templatePath)
	if err != nil {
		errorMsg := fmt.Sprintf("Error reading template: %v", err)
		http.Error(w, errorMsg, http.StatusInternalServerError)
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
