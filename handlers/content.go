package handlers

import (
	"crawler/models"
	"crawler/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// cleanContentForLLM extracts and cleans text content from HTML for LLM consumption
func cleanContentForLLM(htmlContent string) (string, error) {
	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}

	// Remove script and style elements
	doc.Find("script, style, noscript, iframe, object, embed").Remove()
	
	// Remove navigation, header, footer, sidebar elements
	doc.Find("nav, header, footer, aside, .nav, .navbar, .sidebar, .menu").Remove()
	
	// Remove ads and tracking elements
	doc.Find(".ad, .ads, .advertisement, .google-ad, .banner, .popup, .modal").Remove()
	doc.Find("[id*='ad'], [class*='ad'], [id*='google'], [class*='google']").Remove()
	
	// Remove social media widgets
	doc.Find(".social, .share, .facebook, .twitter, .instagram, .linkedin").Remove()
	
	// Remove comments sections
	doc.Find(".comments, .comment, #comments, #comment").Remove()
	
	// Remove images with data URLs and UI elements
	doc.Find("img[src^='data:']").Remove()
	doc.Find("img[data-src]").Remove()
	doc.Find(".button, .btn, button").Remove()
	doc.Find(".scroll, .skip, .toggle").Remove()
	doc.Find("[class*='cookie'], [class*='gdpr']").Remove()
	doc.Find(".elementor-action, .popup").Remove()

	// Extract title
	title := strings.TrimSpace(doc.Find("title").Text())
	
	// Extract meta description
	metaDesc, _ := doc.Find("meta[name='description']").Attr("content")
	metaDesc = strings.TrimSpace(metaDesc)
	
	// Extract main content - prioritize article, main, or content areas
	var mainContent string
	
	// Try to find main content containers
	contentSelectors := []string{
		"article",
		"main", 
		"[role='main']",
		".content",
		".main-content", 
		".post-content",
		".entry-content",
		".article-content",
		"#content",
		"#main",
		".container .row .col", // Bootstrap common pattern
	}
	
	for _, selector := range contentSelectors {
		if content := doc.Find(selector).First(); content.Length() > 0 {
			mainContent = content.Text()
			break
		}
	}
	
	// If no main content found, extract from body but filter out common noise
	if mainContent == "" {
		doc.Find(".header, .footer, .sidebar, .nav, .menu, .breadcrumb, .pagination").Remove()
		mainContent = doc.Find("body").Text()
	}

	// Clean up the text
	mainContent = cleanText(mainContent)
	
	// Build final clean content
	var result strings.Builder
	
	if title != "" {
		result.WriteString("TITLE: ")
		result.WriteString(title)
		result.WriteString("\n\n")
	}
	
	if metaDesc != "" {
		result.WriteString("DESCRIPTION: ")
		result.WriteString(metaDesc)
		result.WriteString("\n\n")
	}
	
	if mainContent != "" {
		result.WriteString("CONTENT:\n")
		result.WriteString(mainContent)
	}
	
	return result.String(), nil
}

// cleanText removes extra whitespace and normalizes text
func cleanText(text string) string {
	// Replace multiple spaces/tabs with single space
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")
	
	// Replace multiple newlines with double newline (paragraph breaks)
	newlineRegex := regexp.MustCompile(`\n\s*\n\s*`)
	text = newlineRegex.ReplaceAllString(text, "\n\n")
	
	// Remove leading/trailing whitespace
	text = strings.TrimSpace(text)
	
	// Remove common boilerplate text patterns
	boilerplatePatterns := []string{
		`(?i)cookie policy`,
		`(?i)privacy policy`,
		`(?i)terms of service`,
		`(?i)accept cookies`,
		`(?i)this website uses cookies`,
		`(?i)subscribe to our newsletter`,
		`(?i)follow us on`,
		`(?i)share this article`,
		`(?i)print this page`,
		`(?i)scroll to top`,
		`(?i)skip to content`,
		`(?i)book a demo`,
		`(?i)know more`,
		`(?i)data:image/gif;base64,[A-Za-z0-9+/=]+`,
		`(?i)elementor-action`,
		`\[.*?\]\(#[^)]*\)`, // Remove internal anchor links
	}
	
	for _, pattern := range boilerplatePatterns {
		regex := regexp.MustCompile(pattern)
		text = regex.ReplaceAllString(text, "")
	}
	
	return strings.TrimSpace(text)
}

// convertToMarkdown converts HTML to markdown format
func convertToMarkdown(htmlContent string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}

	// Remove unwanted elements
	doc.Find("script, style, noscript, iframe, object, embed").Remove()
	doc.Find("nav, header, footer, aside, .nav, .navbar, .sidebar, .menu").Remove()
	doc.Find(".ad, .ads, .advertisement, .google-ad, .banner, .popup, .modal").Remove()
	doc.Find(".social, .share, .facebook, .twitter, .instagram, .linkedin").Remove()
	doc.Find(".comments, .comment, #comments, #comment").Remove()
	
	// Remove images with data URLs (base64/placeholder images)
	doc.Find("img[src^='data:']").Remove()
	doc.Find("img[data-src]").Remove() // Lazy loaded images
	
	// Remove common UI elements
	doc.Find(".button, .btn, button").Remove()
	doc.Find(".scroll, .skip, .toggle").Remove()
	doc.Find("[class*='cookie'], [class*='gdpr']").Remove()
	doc.Find(".elementor-action, .popup").Remove()

	var result strings.Builder

	// Extract title
	if title := strings.TrimSpace(doc.Find("title").Text()); title != "" {
		result.WriteString("# " + title + "\n\n")
	}

	// Extract meta description
	if metaDesc, exists := doc.Find("meta[name='description']").Attr("content"); exists {
		metaDesc = strings.TrimSpace(metaDesc)
		if metaDesc != "" {
			result.WriteString("*" + metaDesc + "*\n\n")
		}
	}

	// Process main content
	processElementToMarkdown(doc.Find("body"), &result, 0)

	return cleanMarkdownText(result.String()), nil
}

// processElementToMarkdown recursively processes HTML elements to markdown
func processElementToMarkdown(selection *goquery.Selection, result *strings.Builder, depth int) {
	selection.Contents().Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				result.WriteString(text)
			}
		} else {
			tag := goquery.NodeName(s)
			switch tag {
			case "h1":
				result.WriteString("\n\n# " + strings.TrimSpace(s.Text()) + "\n\n")
			case "h2":
				result.WriteString("\n\n## " + strings.TrimSpace(s.Text()) + "\n\n")
			case "h3":
				result.WriteString("\n\n### " + strings.TrimSpace(s.Text()) + "\n\n")
			case "h4":
				result.WriteString("\n\n#### " + strings.TrimSpace(s.Text()) + "\n\n")
			case "h5":
				result.WriteString("\n\n##### " + strings.TrimSpace(s.Text()) + "\n\n")
			case "h6":
				result.WriteString("\n\n###### " + strings.TrimSpace(s.Text()) + "\n\n")
			case "p":
				text := strings.TrimSpace(s.Text())
				if text != "" {
					result.WriteString("\n\n" + text + "\n\n")
				}
			case "br":
				result.WriteString("\n")
			case "strong", "b":
				result.WriteString("**" + strings.TrimSpace(s.Text()) + "**")
			case "em", "i":
				result.WriteString("*" + strings.TrimSpace(s.Text()) + "*")
			case "code":
				result.WriteString("`" + strings.TrimSpace(s.Text()) + "`")
			case "pre":
				result.WriteString("\n\n```\n" + s.Text() + "\n```\n\n")
			case "blockquote":
				lines := strings.Split(strings.TrimSpace(s.Text()), "\n")
				result.WriteString("\n\n")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						result.WriteString("> " + strings.TrimSpace(line) + "\n")
					}
				}
				result.WriteString("\n")
			case "ul":
				result.WriteString("\n\n")
				s.Find("li").Each(func(j int, li *goquery.Selection) {
					result.WriteString("- " + strings.TrimSpace(li.Text()) + "\n")
				})
				result.WriteString("\n")
			case "ol":
				result.WriteString("\n\n")
				s.Find("li").Each(func(j int, li *goquery.Selection) {
					result.WriteString(fmt.Sprintf("%d. %s\n", j+1, strings.TrimSpace(li.Text())))
				})
				result.WriteString("\n")
			case "a":
				href, exists := s.Attr("href")
				text := strings.TrimSpace(s.Text())
				if exists && text != "" && href != "" {
					result.WriteString("[" + text + "](" + href + ")")
				} else if text != "" {
					result.WriteString(text)
				}
			case "img":
				alt, _ := s.Attr("alt")
				src, exists := s.Attr("src")
				if exists && !strings.HasPrefix(src, "data:") && alt != "" {
					// Only include images with real URLs and alt text
					result.WriteString("![" + alt + "](" + src + ")")
				}
			case "table":
				result.WriteString("\n\n")
				s.Find("tr").Each(func(j int, tr *goquery.Selection) {
					result.WriteString("|")
					tr.Find("td, th").Each(func(k int, cell *goquery.Selection) {
						result.WriteString(" " + strings.TrimSpace(cell.Text()) + " |")
					})
					result.WriteString("\n")
					if j == 0 { // Add header separator
						tr.Find("td, th").Each(func(k int, cell *goquery.Selection) {
							result.WriteString("|---")
						})
						result.WriteString("|\n")
					}
				})
				result.WriteString("\n")
			default:
				// For other elements, just process their content
				processElementToMarkdown(s, result, depth+1)
			}
		}
	})
}

// cleanMarkdownText normalizes markdown text
func cleanMarkdownText(text string) string {
	// Remove excessive newlines
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	
	// Clean up spacing around headers
	text = regexp.MustCompile(`\n\s*\n\s*(#{1,6})`).ReplaceAllString(text, "\n\n$1")
	text = regexp.MustCompile(`(#{1,6}[^\n]*)\n\s*\n\s*`).ReplaceAllString(text, "$1\n\n")
	
	// Remove extra spaces
	text = regexp.MustCompile(` +`).ReplaceAllString(text, " ")
	
	return strings.TrimSpace(text)
}

// HandleGetContent handles the POST /content endpoint
// @Summary Get webpage content in all formats (HTML, text, markdown)
// @Description Fetches webpage content and returns it in HTML, clean text, and markdown formats
// @Tags content
// @Accept json
// @Produce json
// @Param request body models.ContentRequest true "Content request with URL or URLs"
// @Success 200 {object} models.ContentBatchResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Router /content [post]
func HandleGetContent(w http.ResponseWriter, r *http.Request) {
	var req models.ContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Determine which URLs to process
	var urls []string
	if len(req.URLs) > 0 {
		urls = req.URLs
	} else if req.URL != "" {
		urls = []string{req.URL}
	} else {
		http.Error(w, "Either 'url' or 'urls' is required", http.StatusBadRequest)
		return
	}

	// Configure concurrency (default 50, max 100)
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 50 // Default
	}
	if concurrency > 100 {
		concurrency = 100 // Max limit
	}

	// Process URLs with worker pool
	results := make([]models.ContentResponse, len(urls))
	var success, failed int
	var successMutex, failedMutex sync.Mutex

	// Create job channel and worker pool
	jobs := make(chan struct {
		index int
		url   string
	}, len(urls))
	
	var wg sync.WaitGroup
	
	// Start worker goroutines
	workerCount := concurrency
	if len(urls) < concurrency {
		workerCount = len(urls)
	}
	
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result := processURL(job.url)
				results[job.index] = result
				
				if result.Error == "" {
					successMutex.Lock()
					success++
					successMutex.Unlock()
				} else {
					failedMutex.Lock()
					failed++
					failedMutex.Unlock()
				}
			}
		}()
	}
	
	// Send jobs to workers
	for i, url := range urls {
		jobs <- struct {
			index int
			url   string
		}{index: i, url: url}
	}
	close(jobs)
	
	// Wait for all workers to complete
	wg.Wait()

	// Build batch response
	batchResponse := models.ContentBatchResponse{
		Results: results,
		Total:   len(urls),
		Success: success,
		Failed:  failed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(batchResponse)
}

// processURL processes a single URL and returns all formats using tiered scraping
func processURL(targetURL string) models.ContentResponse {
	fmt.Printf("[CONTENT SCRAPER] Starting tiered scraping for URL: %s\n", targetURL)
	
	// Tier 1: Try HTML-based scraping first
	fmt.Printf("[CONTENT SCRAPER] Tier 1: Attempting HTML scraping for %s\n", targetURL)
	htmlResponse := tryHTMLScraping(targetURL)
	
	// If HTML scraping succeeds and returns meaningful content, use it
	if htmlResponse.Error == "" && len(strings.TrimSpace(htmlResponse.Markdown)) > 100 {
		fmt.Printf("[CONTENT SCRAPER] Tier 1: SUCCESS - HTML scraping returned %d chars for %s\n", len(htmlResponse.Markdown), targetURL)
		return htmlResponse
	}
	
	fmt.Printf("[CONTENT SCRAPER] Tier 1: FAILED - HTML scraping failed for %s (error: %s, content length: %d)\n", targetURL, htmlResponse.Error, len(strings.TrimSpace(htmlResponse.Markdown)))
	
	// Tier 2: Fallback to browser-based scraping if HTML scraping fails or returns minimal content
	fmt.Printf("[CONTENT SCRAPER] Tier 2: Attempting browser scraping for %s\n", targetURL)
	browserResponse := tryBrowserScraping(targetURL)
	if browserResponse.Error == "" {
		fmt.Printf("[CONTENT SCRAPER] Tier 2: SUCCESS - Browser scraping returned %d chars for %s\n", len(browserResponse.Markdown), targetURL)
		return browserResponse
	}
	
	fmt.Printf("[CONTENT SCRAPER] Tier 2: FAILED - Browser scraping failed for %s (error: %s)\n", targetURL, browserResponse.Error)
	
	// If both methods fail, return the HTML response with additional context
	if htmlResponse.Error != "" {
		// Add browser fallback info to the original error
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

// tryJSDOMRendering performs lightweight browser automation with basic JavaScript rendering and scrolling
func tryJSDOMRendering(targetURL string) (string, error) {
	fmt.Printf("[TIER 1 - JSDOM] Starting lightweight browser rendering for %s\n", targetURL)

	// Add recovery for browser panics with proper error return
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[TIER 1 - JSDOM] Browser panic recovered for %s: %v\n", targetURL, r)
		}
	}()

	// Create a lightweight browser instance with error handling
	var browser *rod.Browser
	var l *launcher.Launcher
	
	// Always use system Chromium instead of downloading
	l = launcher.New().
		Bin("/usr/bin/chromium"). // Force use of system browser (Alpine path)
		Headless(true).
		NoSandbox(true).
		Set("disable-dev-shm-usage").
		Set("disable-extensions").
		Set("disable-gpu").
		Set("disable-web-security").
		Set("disable-background-timer-throttling").
		Set("disable-backgrounding-occluded-windows").
		Set("disable-renderer-backgrounding")
	
	fmt.Printf("[TIER 1 - JSDOM] Using system Chromium (no download) for %s\n", targetURL)

	controlURL, err := l.Launch()
	if err != nil {
		return "", fmt.Errorf("failed to launch browser: %v", err)
	}
	
	browser = rod.New().ControlURL(controlURL)
	err = browser.Connect()
	if err != nil {
		l.Cleanup()
		return "", fmt.Errorf("failed to connect to browser: %v", err)
	}

	// Ensure cleanup happens properly
	defer func() {
		if browser != nil {
			browser.Close()
		}
		if l != nil {
			l.Cleanup()
		}
	}()

	// Create page with shorter timeout for Tier 1 (fast processing)
	page, err := browser.Timeout(10 * time.Second).Page(proto.TargetCreateTarget{URL: targetURL})
	if err != nil {
		return "", fmt.Errorf("failed to create page: %v", err)
	}
	defer page.MustClose()

	// Navigate with timeout
	err = page.Navigate(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to navigate: %v", err)
	}

	// Wait for page to load with timeout handling
	err = page.WaitLoad()
	if err != nil {
		fmt.Printf("[TIER 1 - JSDOM] Warning: WaitLoad failed for %s: %v - continuing anyway\n", targetURL, err)
	}

	// Perform simple scrolling using Rod's built-in methods instead of eval
	fmt.Printf("[TIER 1 - JSDOM] Performing scrolling to trigger lazy-loaded content for %s\n", targetURL)
	
	// Use Rod's built-in scroll methods which are more reliable
	maxScrollSteps := 5
	for i := 0; i < maxScrollSteps; i++ {
		// Try to scroll using different methods
		err := func() error {
			// Method 1: Try keyboard PageDown
			if err := page.KeyActions().Press(input.PageDown).Do(); err == nil {
				return nil
			}
			
			// Method 2: Try mouse wheel scroll
			if err := page.Mouse.Scroll(0, 800, 3); err == nil {
				return nil
			}
			
			// Method 3: Simple eval fallback
			_, err := page.Eval("window.scrollBy(0, 800)")
			return err
		}()
		
		if err != nil {
			fmt.Printf("[TIER 1 - JSDOM] Warning: Scroll step %d failed for %s: %v\n", i+1, targetURL, err)
		} else {
			fmt.Printf("[TIER 1 - JSDOM] Scroll step %d/%d completed for %s\n", i+1, maxScrollSteps, targetURL)
		}
		
		// Wait for content to load
		time.Sleep(800 * time.Millisecond)
	}
	
	// Final scroll to bottom using reliable mouse scroll (no eval)
	fmt.Printf("[TIER 1 - JSDOM] Performing final scroll to bottom for %s\n", targetURL)
	page.Mouse.Scroll(0, 5000, 10) // Large scroll to reach absolute bottom
	fmt.Printf("[TIER 1 - JSDOM] Final scroll to bottom completed for %s\n", targetURL)
	
	time.Sleep(1 * time.Second)
	fmt.Printf("[TIER 1 - JSDOM] Completed scrolling sequence for %s\n", targetURL)
	
	// Wait briefly for final content to settle
	time.Sleep(800 * time.Millisecond)

	// Scroll back to top for consistent content extraction
	page.MustEval(`window.scrollTo(0, 0)`)
	time.Sleep(200 * time.Millisecond)

	// Get the final HTML content after JavaScript execution and scrolling
	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get HTML content: %v", err)
	}

	fmt.Printf("[TIER 1 - JSDOM] Successfully extracted content after scrolling for %s (size: %d bytes)\n", targetURL, len(html))
	
	return html, nil
}

// tryHTMLScraping attempts to scrape content using HTTP client and HTML parsing
func tryHTMLScraping(targetURL string) models.ContentResponse {
	response := models.ContentResponse{
		URL: targetURL,
	}

	fmt.Printf("[TIER 1 - HTML+JSDOM] Starting enhanced HTML scraping with JSDOM rendering for URL: %s\n", targetURL)

	// Try URL fallback to find an accessible URL
	actualURL, fallbackInfo := utils.FindAccessibleURL(targetURL, "")
	response.URL = actualURL

	if !fallbackInfo.Success {
		response.Error = fallbackInfo.Error
		return response
	}

	// Try browser-based JSDOM rendering first for better JS support
	if htmlContent, err := tryJSDOMRendering(actualURL); err == nil && htmlContent != "" {
		fmt.Printf("[TIER 1 - HTML+JSDOM] Successfully rendered with JSDOM for %s\n", actualURL)
		
		response.StatusCode = 200
		response.ContentType = "text/html; charset=UTF-8"
		response.Headers = map[string]string{
			"Content-Type": "text/html; charset=UTF-8",
			"X-Rendered-By": "JSDOM-Tier1",
		}

		// Process JSDOM-rendered HTML content
		if markdown, err := convertToMarkdown(htmlContent); err == nil {
			response.Markdown = markdown
		} else {
			response.Markdown = "Error converting JSDOM content to markdown: " + err.Error()
		}

		// Calculate sizes
		response.Sizes = models.ContentSizes{
			Markdown: len(response.Markdown),
		}

		return response
	} else {
		fmt.Printf("[TIER 1 - HTML+JSDOM] JSDOM rendering failed for %s: %v - falling back to HTTP scraping\n", actualURL, err)
	}

	// Fallback to original HTTP scraping method
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	httpReq, err := http.NewRequest("GET", actualURL, nil)
	if err != nil {
		response.Error = "Failed to create request: " + err.Error()
		return response
	}

	// Set browser headers using utils function
	utils.SetBrowserHeaders(httpReq)
	// Disable compression to get raw content
	httpReq.Header.Set("Accept-Encoding", "identity")

	// Execute request
	resp, err := client.Do(httpReq)
	if err != nil {
		response.Error = "Failed to fetch URL: " + err.Error()
		return response
	}
	defer resp.Body.Close()

	// Check for 403/blocked responses
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		response.Error = fmt.Sprintf("HTTP %d: %s (blocked by server, trying browser fallback)", resp.StatusCode, resp.Status)
		return response
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		response.Error = "Failed to read response: " + err.Error()
		return response
	}

	// Prepare headers map
	headers := make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}

	// Process content into all formats
	rawContent := string(body)
	contentType := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(strings.ToLower(contentType), "text/html")

	// Set basic response fields
	response.StatusCode = resp.StatusCode
	response.ContentType = contentType
	response.Headers = headers

	if isHTML {
		// Markdown format
		if markdown, err := convertToMarkdown(rawContent); err == nil {
			response.Markdown = markdown
		} else {
			response.Markdown = "Error converting to markdown: " + err.Error()
		}
	} else {
		// For non-HTML content, return as-is for all formats
		response.Markdown = rawContent
	}

	// Calculate sizes
	response.Sizes = models.ContentSizes{
		Markdown: len(response.Markdown),
	}

	return response
}

// tryBrowserScraping attempts to scrape content using a headless browser (Rod)
func tryBrowserScraping(targetURL string) models.ContentResponse {
	response := models.ContentResponse{
		URL: targetURL,
	}

	// Try to get content using headless browser
	htmlContent, err := scrapeWithBrowser(targetURL)
	if err != nil {
		// If browser fails for any reason, try aggressive HTTP as fallback
		aggressiveResponse := tryAggressiveHTTP(targetURL)
		if aggressiveResponse.Error == "" {
			return aggressiveResponse
		}
		
		// If both browser and aggressive HTTP fail, return browser error with context
		response.Error = fmt.Sprintf("Browser scraping failed: %s (Aggressive HTTP also failed: %s)", err.Error(), aggressiveResponse.Error)
		return response
	}

	// Process the HTML content
	if markdown, err := convertToMarkdown(htmlContent); err == nil {
		response.Markdown = markdown
	} else {
		response.Markdown = "Error converting to markdown: " + err.Error()
	}

	// Set response fields
	response.StatusCode = 200
	response.ContentType = "text/html"
	response.Headers = map[string]string{
		"Content-Type": "text/html",
		"X-Scraped-With": "Browser",
	}

	// Calculate sizes
	response.Sizes = models.ContentSizes{
		Markdown: len(response.Markdown),
	}

	return response
}

// tryAggressiveHTTP attempts aggressive HTTP scraping with different user agents and headers
func tryAggressiveHTTP(targetURL string) models.ContentResponse {
	fmt.Printf("[AGGRESSIVE HTTP] Starting aggressive HTTP scraping for %s\n", targetURL)
	
	response := models.ContentResponse{
		URL: targetURL,
	}

	// Different user agents to try
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/121.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
	}

	for i, ua := range userAgents {
		fmt.Printf("[AGGRESSIVE HTTP] Attempt %d/%d for %s using UA: %s\n", i+1, len(userAgents), targetURL, ua[:50]+"...")
		
		// Add delay between attempts
		if i > 0 {
			fmt.Printf("[AGGRESSIVE HTTP] Waiting %d seconds before attempt %d\n", i, i+1)
			time.Sleep(time.Duration(i) * time.Second)
		}

		client := &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Allow up to 10 redirects
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				// Set headers for redirect requests too
				req.Header.Set("User-Agent", ua)
				return nil
			},
		}

		httpReq, err := http.NewRequest("GET", targetURL, nil)
		if err != nil {
			continue
		}

		// Set aggressive headers
		httpReq.Header.Set("User-Agent", ua)
		httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		httpReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
		httpReq.Header.Set("Accept-Encoding", "identity") // Disable compression for better compatibility
		httpReq.Header.Set("DNT", "1")
		httpReq.Header.Set("Connection", "keep-alive")
		httpReq.Header.Set("Upgrade-Insecure-Requests", "1")
		
		// Add browser-specific headers
		if strings.Contains(ua, "Chrome") {
			httpReq.Header.Set("Sec-Fetch-Dest", "document")
			httpReq.Header.Set("Sec-Fetch-Mode", "navigate")
			httpReq.Header.Set("Sec-Fetch-Site", "none")
			httpReq.Header.Set("Sec-Fetch-User", "?1")
		}
		
		httpReq.Header.Set("Cache-Control", "max-age=0")

		// Add referer for some attempts
		if i > 0 {
			httpReq.Header.Set("Referer", "https://www.google.com/")
		}
		
		// Add random delay
		if i > 2 {
			httpReq.Header.Set("Referer", "https://www.bing.com/")
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			fmt.Printf("[AGGRESSIVE HTTP] Attempt %d FAILED for %s: %v\n", i+1, targetURL, err)
			continue
		}
		defer resp.Body.Close()

		fmt.Printf("[AGGRESSIVE HTTP] Attempt %d got status %d for %s\n", i+1, resp.StatusCode, targetURL)

		// If successful (200, 201, etc.), process the response
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}

			rawContent := string(body)
			contentType := resp.Header.Get("Content-Type")
			isHTML := strings.Contains(strings.ToLower(contentType), "text/html")

			response.StatusCode = resp.StatusCode
			response.ContentType = contentType
			response.Headers = map[string]string{
				"Content-Type": contentType,
				"X-Scraped-With": fmt.Sprintf("Aggressive-HTTP-UA%d", i+1),
			}

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

			fmt.Printf("[AGGRESSIVE HTTP] SUCCESS on attempt %d for %s - got %d chars\n", i+1, targetURL, len(response.Markdown))
			return response
		}
	}

	fmt.Printf("[AGGRESSIVE HTTP] ALL ATTEMPTS FAILED for %s\n", targetURL)
	response.Error = "All aggressive HTTP attempts failed (browser not available in this environment)"
	return response
}

// scrapeWithBrowser uses Rod (headless Firefox) to scrape JavaScript-heavy pages
func scrapeWithBrowser(targetURL string) (string, error) {
	// Try to launch Firefox with minimal settings for better Docker compatibility
	launch := launcher.New().
		Bin("/usr/bin/firefox").
		Headless(true).
		Set("no-sandbox").
		Set("disable-gpu").
		Set("disable-dev-shm-usage")

	// Try to launch browser with error handling
	browserURL, err := launch.Launch()
	if err != nil {
		return "", fmt.Errorf("failed to launch browser: %v", err)
	}

	browser := rod.New().ControlURL(browserURL)
	defer func() {
		// Safe cleanup
		if browser != nil {
			browser.Close()
		}
	}()

	// Create page with timeout and error handling
	page := browser.Timeout(30 * time.Second).MustPage()
	defer func() {
		if page != nil {
			page.Close()
		}
	}()

	// Set basic stealth properties for Firefox
	_, err = page.Evaluate(rod.Eval(`
		// Override webdriver property
		Object.defineProperty(navigator, 'webdriver', {
			get: () => undefined,
		});
		
		// Set languages
		Object.defineProperty(navigator, 'languages', {
			get: () => ['en-US', 'en'],
		});
	`))
	if err != nil {
		// Continue even if stealth setup fails
		fmt.Printf("Warning: Failed to set stealth properties: %v\n", err)
	}

	// Navigate to the URL
	err = page.Navigate(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to navigate to URL: %v", err)
	}

	// Wait for page to load
	err = page.WaitLoad()
	if err != nil {
		return "", fmt.Errorf("failed to wait for page load: %v", err)
	}

	// Wait a bit more for JavaScript to execute
	time.Sleep(2 * time.Second)

	// Try to wait for common content containers to load (optional)
	page.Timeout(5 * time.Second).Element("body")

	// Get the page HTML content
	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get page HTML: %v", err)
	}

	// Check if we got meaningful content
	if len(strings.TrimSpace(html)) < 100 {
		return "", fmt.Errorf("page returned minimal content")
	}

	return html, nil
}