package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"

	"go-pdf/utils"
)

func init() {
	file, err := os.OpenFile("log.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(io.MultiWriter(os.Stdout, file))
}

func main() {
	fmt.Println("Starting")
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	browser := rod.New().MustConnect()
	defer browser.MustClose()

	pool := rod.NewPagePool(5)

	createPage := func() (*rod.Page, error) {
		return browser.MustIncognito().MustPage(), nil
	}

	lastRequestId := 0

	http.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
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

		page, err := pool.Get(createPage)
		if err != nil {
			msg := "Error creating page"
			http.Error(response, msg, http.StatusInternalServerError)
			logger.Fatalf("%s: %s", msg, err)
		}
		logger.Infof("Got page after %s", time.Since(start))
		defer pool.Put(page)

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
			//fmt.Printf("Rendering page from data URL: %s\n", dataUrl)
			logger.Infoln("Rendering page from data URL")

			err = request.Body.Close()
			if err != nil {
				logger.Warnf("Error closing request body: %s", err)
			}
			//page = browser.MustPage(dataUrl)
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
		logger.Infof("Page loaded in %v", time.Since(start))

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
		if _, err := response.Write(data); err != nil {
			msg := "Error writing PDF"
			logger.Warnf("%s: %s", msg, err)
			http.Error(response, msg, http.StatusInternalServerError)
			return
		}
		logger.Infof("Completed request after %s", time.Since(start))
	})

	addr := os.Getenv("LISTEN_ADDRESS")
	fmt.Printf("Listening at: %s\n", addr)
	fmt.Println(" - GET /?url=https://example.com to render a page")
	fmt.Println(" - POST / with HTML to render a page")
	fmt.Println("Press Ctrl+C to quit")
	err = http.ListenAndServe(addr, nil)
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
	titleElement, err := page.Element("head title")
	if err == nil {
		title := titleElement.MustText()
		logger.Infof("Printing page '%s'", title)
	}

	pdf, err := page.PDF(pdfOptions)
	if err != nil {
		logger.Panicf("Error rendering PDF: %s", err)
	}
	logger.Infof("Rendered PDF in %v", time.Since(now))

	return pdf
}
