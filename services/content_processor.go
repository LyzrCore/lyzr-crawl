package services

import (
	"crawler/models"
	"crawler/utils"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

func processURL(targetURL string) models.ContentResponse {
	fmt.Printf("[CONTENT SCRAPER] Starting tiered scraping for URL: %s\n", targetURL)

	htmlResponse := tryHTMLScraping(targetURL)

	if htmlResponse.Error == "" && len(strings.TrimSpace(htmlResponse.Markdown)) > 100 {
		fmt.Printf("[CONTENT SCRAPER] Tier 1: SUCCESS - HTML scraping returned %d chars for %s\n", len(htmlResponse.Markdown), targetURL)
		return htmlResponse
	}

	fmt.Printf("[CONTENT SCRAPER] Tier 1: FAILED - HTML scraping failed for %s (error: %s, content length: %d)\n", targetURL, htmlResponse.Error, len(strings.TrimSpace(htmlResponse.Markdown)))

	browserResponse := tryBrowserScraping(targetURL)
	if browserResponse.Error == "" {
		fmt.Printf("[CONTENT SCRAPER] Tier 2: SUCCESS - Browser scraping returned %d chars for %s\n", len(browserResponse.Markdown), targetURL)
		return browserResponse
	}

	fmt.Printf("[CONTENT SCRAPER] Tier 2: FAILED - Browser scraping failed for %s (error: %s)\n", targetURL, browserResponse.Error)

	if htmlResponse.Error != "" {
		if strings.Contains(browserResponse.Error, "browser not available") {
			htmlResponse.Error = htmlResponse.Error + " (Browser fallback unavailable: headless browser not available in this environment)"
		} else {
			htmlResponse.Error = htmlResponse.Error + fmt.Sprintf(" (Browser fallback also failed: %s)", browserResponse.Error)
		}
		fmt.Printf("[CONTENT SCRAPER] ALL TIERS FAILED for %s - Final error: %s\n", targetURL, htmlResponse.Error)
		return htmlResponse
	}

	fmt.Printf("[CONTENT SCRAPER] Returning browser response for %s\n", targetURL)
	return browserResponse
}

func tryHTMLScraping(targetURL string) models.ContentResponse {
	response := models.ContentResponse{
		URL: targetURL,
	}

	fmt.Printf("[TIER 1 - HTML+JSDOM] Starting enhanced HTML scraping with JSDOM rendering for URL: %s\n", targetURL)

	actualURL, fallbackInfo := utils.FindAccessibleURL(targetURL, "")
	response.URL = actualURL

	if !fallbackInfo.Success {
		response.Error = fallbackInfo.Error
		return response
	}

	if htmlContent, err := tryJSDOMRendering(actualURL); err == nil && htmlContent != "" {
		fmt.Printf("[TIER 1 - HTML+JSDOM] Successfully rendered with JSDOM for %s\n", actualURL)

		response.StatusCode = 200
		response.ContentType = "text/html; charset=UTF-8"
		response.Headers = map[string]string{
			"Content-Type":  "text/html; charset=UTF-8",
			"X-Rendered-By": "JSDOM-Tier1",
		}

		if markdown, err := convertToMarkdown(htmlContent); err == nil {
			response.Markdown = markdown
		} else {
			response.Markdown = "Error converting JSDOM content to markdown: " + err.Error()
		}

		response.Sizes = models.ContentSizes{
			Markdown: len(response.Markdown),
		}

		return response
	} else {
		fmt.Printf("[TIER 1 - HTML+JSDOM] JSDOM rendering failed for %s: %v - falling back to HTTP scraping\n", actualURL, err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	httpReq, err := http.NewRequest("GET", actualURL, nil)
	if err != nil {
		response.Error = "Failed to create request: " + err.Error()
		return response
	}

	utils.SetBrowserHeaders(httpReq)
	httpReq.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(httpReq)
	if err != nil {
		response.Error = "Failed to fetch URL: " + err.Error()
		return response
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		response.Error = fmt.Sprintf("HTTP %d: %s (blocked by server, trying browser fallback)", resp.StatusCode, resp.Status)
		return response
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		response.Error = "Failed to read response: " + err.Error()
		return response
	}

	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	rawContent := string(body)
	contentType := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(strings.ToLower(contentType), "text/html")

	response.StatusCode = resp.StatusCode
	response.ContentType = contentType
	response.Headers = headers

	if isHTML {
		if markdown, err := convertToMarkdown(rawContent); err == nil {
			response.Markdown = markdown
		} else {
			response.Markdown = "Error converting to markdown: " + err.Error()
		}
	} else {
		response.Markdown = rawContent
	}

	response.Sizes = models.ContentSizes{
		Markdown: len(response.Markdown),
	}

	return response
}

func tryBrowserScraping(targetURL string) models.ContentResponse {
	response := models.ContentResponse{
		URL: targetURL,
	}

	if GlobalPool == nil || GlobalPool.browserPool == nil {
		response.Error = "Browser pool not initialized"
		return response
	}

	browser, err := GlobalPool.browserPool.Get()
	if err != nil {
		response.Error = fmt.Sprintf("Failed to get browser from pool: %v", err)
		return response
	}
	defer GlobalPool.browserPool.Release(browser)

	htmlContent, err := scrapeWithPooledBrowser(browser, targetURL)
	if err != nil {
		aggressiveResponse := tryAggressiveHTTP(targetURL)
		if aggressiveResponse.Error == "" {
			return aggressiveResponse
		}

		response.Error = fmt.Sprintf("Browser scraping failed: %s (Aggressive HTTP also failed: %s)", err.Error(), aggressiveResponse.Error)
		return response
	}

	if markdown, err := convertToMarkdown(htmlContent); err == nil {
		response.Markdown = markdown
	} else {
		response.Markdown = "Error converting to markdown: " + err.Error()
	}

	response.StatusCode = 200
	response.ContentType = "text/html"
	response.Headers = map[string]string{
		"Content-Type":   "text/html",
		"X-Scraped-With": "Browser",
	}

	response.Sizes = models.ContentSizes{
		Markdown: len(response.Markdown),
	}

	return response
}

func scrapeWithPooledBrowser(browser *rod.Browser, targetURL string) (string, error) {
	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return "", fmt.Errorf("failed to create page: %v", err)
	}
	defer page.Close()

	err = page.Navigate(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to navigate to URL: %v", err)
	}

	err = page.WaitLoad()
	if err != nil {
		return "", fmt.Errorf("failed to wait for page load: %v", err)
	}

	time.Sleep(2 * time.Second)

	page.Timeout(5 * time.Second).Element("body")

	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get page HTML: %v", err)
	}

	if len(strings.TrimSpace(html)) < 100 {
		return "", fmt.Errorf("page returned minimal content")
	}

	return html, nil
}
