package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	httpSwagger "github.com/swaggo/http-swagger"
	_ "crawler/docs" // This line is required for Swagger
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// @title Web Crawler API
// @version 1.0
// @description A fast web crawler API that extracts URLs from websites and stores results in MongoDB
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /

// CrawlRequest represents the API request for crawling
type CrawlRequest struct {
	URL     string `json:"url" example:"https://example.com" binding:"required"`
	JobID   string `json:"job_id,omitempty" example:"my-custom-session-123"`
	Depth   int    `json:"depth,omitempty" example:"2"`
	Workers int    `json:"workers,omitempty" example:"10"`
	Delay   string `json:"delay,omitempty" example:"200ms"`
	MaxURLs int    `json:"max_urls,omitempty" example:"1000"`
}

// CrawlResponse represents the immediate API response
type CrawlResponse struct {
	JobID   string `json:"job_id" example:"60f7b3b3b3b3b3b3b3b3b3b3"`
	Status  string `json:"status" example:"accepted"`
	Message string `json:"message" example:"Crawl job started successfully"`
}

// JobStatus represents the status of a crawl job
type JobStatus struct {
	ID        string               `json:"id" bson:"_id" example:"60f7b3b3b3b3b3b3b3b3b3b3"`
	Status    string               `json:"status" bson:"status" example:"completed" enum:"running,completed,failed"`
	Progress  string               `json:"progress,omitempty" bson:"progress,omitempty" example:"Starting crawl..."`
	Result    *CrawlResult         `json:"result,omitempty" bson:"result,omitempty"`
	Error     string               `json:"error,omitempty" bson:"error,omitempty" example:"Error message if failed"`
	CreatedAt time.Time            `json:"created_at" bson:"created_at" example:"2023-07-18T10:30:45Z"`
	UpdatedAt time.Time            `json:"updated_at" bson:"updated_at" example:"2023-07-18T10:32:15Z"`
	Request   *CrawlRequest        `json:"request,omitempty" bson:"request,omitempty"`
}

// WebSocketMessage represents a real-time update message sent to WebSocket clients
type WebSocketMessage struct {
	Type      string    `json:"type"`             // "progress", "url_discovered", "completed", "connected", "error"
	JobID     string    `json:"job_id"`
	URL       string    `json:"url,omitempty"`
	Depth     int       `json:"depth,omitempty"`
	Progress  string    `json:"progress,omitempty"`   // Human-readable progress message
	Timestamp time.Time `json:"timestamp"`
	Total     int       `json:"total,omitempty"`      // Total URLs found
	PageCount int       `json:"page_count,omitempty"` // Total pages crawled
	Error     string    `json:"error,omitempty"`
}

// Global variables for the API server
var (
	mongoClient     *mongo.Client
	crawlCollection *mongo.Collection
	jobsCollection  *mongo.Collection
	activeJobs      = make(map[string]*JobStatus)
	jobsMutex       sync.RWMutex
	
	// WebSocket upgrader
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins in development
		},
	}
)

// URLFallback contains information about URL fallback attempts
type URLFallback struct {
	OriginalURL string
	FallbackURL string
	Success     bool
	Error       string
}

// ScrapeOpsConfig holds configuration for ScrapeOps integration
type ScrapeOpsConfig struct {
	APIKey     string
	UserAgents []string
	Headers    []map[string]string
	LastUpdate time.Time
}

// Global ScrapeOps configuration
var scrapeOpsConfig = &ScrapeOpsConfig{
	APIKey: "8d44ac41-0e85-42ca-8499-cc73eea0b672", // Your provided API key
}

// ScrapeOpsUserAgent represents user agent response from ScrapeOps
type ScrapeOpsUserAgent struct {
	UserAgent string `json:"user-agent"`
}

// ScrapeOpsUserAgentResponse represents the full response
type ScrapeOpsUserAgentResponse struct {
	Result []ScrapeOpsUserAgent `json:"result"`
}

// ScrapeOpsHeader represents browser header response from ScrapeOps
type ScrapeOpsHeader map[string]string

// ScrapeOpsHeaderResponse represents the full response
type ScrapeOpsHeaderResponse struct {
	Result []ScrapeOpsHeader `json:"result"`
}

// fetchScrapeOpsUserAgents fetches fresh user agents from ScrapeOps API
func fetchScrapeOpsUserAgents() error {
	url := fmt.Sprintf("https://headers.scrapeops.io/v1/user-agents?api_key=%s&num_results=50", scrapeOpsConfig.APIKey)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch user agents: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("ScrapeOps API returned status %d", resp.StatusCode)
	}
	
	var response ScrapeOpsUserAgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}
	
	// Extract user agents
	userAgents := make([]string, len(response.Result))
	for i, ua := range response.Result {
		userAgents[i] = ua.UserAgent
	}
	
	scrapeOpsConfig.UserAgents = userAgents
	scrapeOpsConfig.LastUpdate = time.Now()
	
	log.Printf("Fetched %d user agents from ScrapeOps", len(userAgents))
	return nil
}

// fetchScrapeOpsBrowserHeaders fetches fresh browser headers from ScrapeOps API
func fetchScrapeOpsBrowserHeaders() error {
	url := fmt.Sprintf("https://headers.scrapeops.io/v1/browser-headers?api_key=%s&num_results=20", scrapeOpsConfig.APIKey)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch browser headers: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("ScrapeOps API returned status %d", resp.StatusCode)
	}
	
	var response ScrapeOpsHeaderResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}
	
	scrapeOpsConfig.Headers = make([]map[string]string, len(response.Result))
	for i, header := range response.Result {
		scrapeOpsConfig.Headers[i] = map[string]string(header)
	}
	scrapeOpsConfig.LastUpdate = time.Now()
	
	log.Printf("Fetched %d browser header sets from ScrapeOps", len(response.Result))
	return nil
}

// initScrapeOpsHeaders initializes ScrapeOps headers and user agents
func initScrapeOpsHeaders() {
	// Fetch user agents
	if err := fetchScrapeOpsUserAgents(); err != nil {
		log.Printf("Failed to fetch ScrapeOps user agents: %v", err)
		log.Println("Falling back to static user agents")
	}
	
	// Fetch browser headers
	if err := fetchScrapeOpsBrowserHeaders(); err != nil {
		log.Printf("Failed to fetch ScrapeOps browser headers: %v", err)
		log.Println("Falling back to static headers")
	}
}

// getScrapeOpsUserAgent returns a random user agent from ScrapeOps or fallback
func getScrapeOpsUserAgent() string {
	// Refresh if data is older than 1 hour
	if time.Since(scrapeOpsConfig.LastUpdate) > time.Hour {
		go initScrapeOpsHeaders() // Refresh in background
	}
	
	if len(scrapeOpsConfig.UserAgents) > 0 {
		return scrapeOpsConfig.UserAgents[int(time.Now().UnixNano())%len(scrapeOpsConfig.UserAgents)]
	}
	
	// Fallback to static user agents if ScrapeOps is unavailable
	return getRandomUserAgent()
}

// getScrapeOpsBrowserHeaders returns random browser headers from ScrapeOps or fallback
func getScrapeOpsBrowserHeaders() map[string]string {
	if len(scrapeOpsConfig.Headers) > 0 {
		return scrapeOpsConfig.Headers[int(time.Now().UnixNano())%len(scrapeOpsConfig.Headers)]
	}
	
	// Fallback to basic headers
	return map[string]string{
		"User-Agent":                getRandomUserAgent(),
		"Accept":                   "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8",
		"Accept-Language":          "en-US,en;q=0.9",
		"Accept-Encoding":          "gzip, deflate, br",
		"Cache-Control":            "no-cache",
		"Pragma":                   "no-cache",
		"Sec-Fetch-Dest":           "document",
		"Sec-Fetch-Mode":           "navigate",
		"Sec-Fetch-Site":           "none",
		"Sec-Fetch-User":           "?1",
		"Upgrade-Insecure-Requests": "1",
		"Connection":               "keep-alive",
	}
}

// getRandomUserAgent returns a random realistic user agent (fallback)
func getRandomUserAgent() string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/120.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/120.0",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Edge/120.0.0.0",
	}
	// Use a simple index rotation instead of modulo to get different agents
	return userAgents[int(time.Now().UnixNano())%len(userAgents)]
}

// setBrowserHeaders sets realistic browser headers to bypass bot detection
func setBrowserHeaders(req *http.Request) {
	// Use ScrapeOps headers for maximum stealth
	headers := getScrapeOpsBrowserHeaders()
	
	for key, value := range headers {
		req.Header.Set(key, value)
	}
}

// checkURLAccessibility performs a request to check if a URL is accessible
func checkURLAccessibility(urlStr string) error {
	// For very strict sites like OpenAI, skip the accessibility check
	// and try crawling directly with Colly's more sophisticated approach
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	
	// List of sites known to be very strict - skip pre-check for these
	strictSites := []string{
		"openai.com",
		"www.openai.com",
		"claude.ai",
		"www.claude.ai",
		"chatgpt.com",
		"www.chatgpt.com",
	}
	
	for _, site := range strictSites {
		if strings.Contains(parsed.Host, site) || parsed.Host == site {
			// Skip the check for strict sites, let Colly handle it
			return nil
		}
	}
	
	// Add random delay to appear more human-like
	time.Sleep(time.Duration(500+int(time.Now().UnixNano())%1000) * time.Millisecond)
	
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 5 redirects
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Set browser headers for redirects too
			setBrowserHeaders(req)
			return nil
		},
	}
	
	// Try GET request with minimal response reading
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	
	// Set realistic browser headers to bypass bot detection
	setBrowserHeaders(req)
	
	// Add some additional stealth headers
	req.Header.Set("DNT", "1")
	req.Header.Set("Sec-GPC", "1")
	
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	// Consider 2xx and 3xx status codes as accessible
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return nil
	}
	
	// For 403, still return nil for strict sites as Colly might handle it better
	if resp.StatusCode == 403 {
		for _, site := range strictSites {
			if strings.Contains(parsed.Host, site) || parsed.Host == site {
				return nil // Let Colly try anyway
			}
		}
		return fmt.Errorf("HTTP 403: Forbidden (may need different approach)")
	}
	
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
}

// generateFallbackURLs generates alternative URLs to try if the original fails
func generateFallbackURLs(originalURL string) []string {
	parsed, err := url.Parse(originalURL)
	if err != nil {
		return []string{}
	}
	
	var fallbacks []string
	host := parsed.Host
	
	// Try www variant if original doesn't have www
	if !strings.HasPrefix(host, "www.") {
		wwwURL := *parsed
		wwwURL.Host = "www." + host
		fallbacks = append(fallbacks, wwwURL.String())
	}
	
	// Try non-www variant if original has www
	if strings.HasPrefix(host, "www.") {
		nonWwwURL := *parsed
		nonWwwURL.Host = host[4:] // Remove "www."
		fallbacks = append(fallbacks, nonWwwURL.String())
	}
	
	// Try HTTPS if original is HTTP
	if parsed.Scheme == "http" {
		httpsURL := *parsed
		httpsURL.Scheme = "https"
		fallbacks = append(fallbacks, httpsURL.String())
		
		// Also try HTTPS with www/non-www variants
		if !strings.HasPrefix(host, "www.") {
			httpsWwwURL := httpsURL
			httpsWwwURL.Host = "www." + host
			fallbacks = append(fallbacks, httpsWwwURL.String())
		} else {
			httpsNonWwwURL := httpsURL
			httpsNonWwwURL.Host = host[4:]
			fallbacks = append(fallbacks, httpsNonWwwURL.String())
		}
	}
	
	return fallbacks
}

// findAccessibleURL tries the original URL and fallbacks, returns the first accessible one
func findAccessibleURL(originalURL string, jobID string) (string, *URLFallback) {
	fallbackInfo := &URLFallback{
		OriginalURL: originalURL,
		FallbackURL: originalURL,
		Success:     false,
	}
	
	// First try the original URL
	err := checkURLAccessibility(originalURL)
	if err == nil {
		fallbackInfo.Success = true
		return originalURL, fallbackInfo
	}
	
	// Store the original error
	fallbackInfo.Error = err.Error()
	
	// Publish that we're trying fallbacks
	if jobID != "" {
		publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("⚠️ Original URL failed (%v), trying fallback URLs...", err),
			Timestamp: time.Now(),
		})
	}
	
	// Try fallback URLs
	fallbacks := generateFallbackURLs(originalURL)
	for _, fallbackURL := range fallbacks {
		if jobID != "" {
			publishCrawlEvent(CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("🔄 Trying fallback: %s", fallbackURL),
				Timestamp: time.Now(),
			})
		}
		
		err := checkURLAccessibility(fallbackURL)
		if err == nil {
			fallbackInfo.FallbackURL = fallbackURL
			fallbackInfo.Success = true
			
			if jobID != "" {
				publishCrawlEvent(CrawlEvent{
					Type:      "progress",
					JobID:     jobID,
					Progress:  fmt.Sprintf("✅ Fallback successful! Using: %s", fallbackURL),
					Timestamp: time.Now(),
				})
			}
			
			return fallbackURL, fallbackInfo
		}
		
		if jobID != "" {
			publishCrawlEvent(CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ Fallback failed: %s (%v)", fallbackURL, err),
				Timestamp: time.Now(),
			})
		}
	}
	
	// All URLs failed
	return originalURL, fallbackInfo
}

// initMongoDB initializes the MongoDB connection
func initMongoDB(mongoURI, dbName string) error {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	// Test the connection
	err = client.Ping(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to ping MongoDB: %v", err)
	}

	mongoClient = client
	db := client.Database(dbName)
	crawlCollection = db.Collection("crawls")
	jobsCollection = db.Collection("jobs")

	log.Printf("Connected to MongoDB: %s/%s", mongoURI, dbName)
	return nil
}

// crawlWebsiteWithEvents performs async crawling with real-time events
func crawlWebsiteWithEvents(targetURL string, depth, workers int, delayStr string, maxURLs int, jobID string) (*CrawlResult, error) {
	// Parse delay
	delay, err := time.ParseDuration(delayStr)
	if err != nil {
		delay = 200 * time.Millisecond
	}

	// Publish initial setup progress
	stealthStatus := "basic headers"
	if len(scrapeOpsConfig.UserAgents) > 0 {
		stealthStatus = fmt.Sprintf("ScrapeOps stealth (%d user agents, %d header sets)", len(scrapeOpsConfig.UserAgents), len(scrapeOpsConfig.Headers))
	}
	
	publishCrawlEvent(CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  fmt.Sprintf("🚀 Setting up stealth crawler for %s (depth: %d, workers: %d, %s)", targetURL, depth, workers, stealthStatus),
		Timestamp: time.Now(),
	})

	// Try to find an accessible URL (original or fallback)
	publishCrawlEvent(CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  "🔍 Checking URL accessibility...",
		Timestamp: time.Now(),
	})

	actualURL, fallbackInfo := findAccessibleURL(targetURL, jobID)
	if !fallbackInfo.Success {
		return nil, fmt.Errorf("URL and all fallbacks are inaccessible. Original error: %v", fallbackInfo.Error)
	}

	// If we used a fallback, log it
	if actualURL != targetURL {
		publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("📍 Using fallback URL: %s (original: %s)", actualURL, targetURL),
			Timestamp: time.Now(),
		})
	} else {
		publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("✅ Direct access confirmed for %s", actualURL),
			Timestamp: time.Now(),
		})
	}

	// Parse the actual URL to get the base domain
	parsedURL, err := url.Parse(actualURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing actual URL: %v", err)
	}

	// Create async crawler with maximum stealth settings
	c := colly.NewCollector(
		colly.Async(true), // Enable async mode
	)
	
	// BYPASS ROBOTS.TXT - This ignores robots.txt entirely
	c.IgnoreRobotsTxt = true
	
	// Configure limits for async operation with random delays
	baseDelay := delay
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Parallelism: workers,
		Delay:       baseDelay,
		RandomDelay: baseDelay, // Add randomness to delays
	})
	c.SetRequestTimeout(60 * time.Second) // Even longer timeout for very strict sites
	
	// Add additional stealth settings
	c.UserAgent = getScrapeOpsUserAgent() // Set a base user agent

	// Allow both www and non-www versions of the domain
	baseDomain := parsedURL.Host
	allowedDomains := []string{baseDomain}
	if strings.HasPrefix(baseDomain, "www.") {
		allowedDomains = append(allowedDomains, baseDomain[4:])
	} else {
		allowedDomains = append(allowedDomains, "www."+baseDomain)
	}
	c.AllowedDomains = allowedDomains

	// Thread-safe URL tracking
	var (
		mu           sync.RWMutex
		foundURLs    = make(map[string]bool)
		urlList      []string
		pagesCrawled int64
		stopped      = false
	)

	// Set realistic browser headers for stealth crawling using ScrapeOps
	c.OnRequest(func(r *colly.Request) {
		if stopped {
			r.Abort()
			return
		}
		
		// Use ScrapeOps headers for maximum stealth
		headers := getScrapeOpsBrowserHeaders()
		for key, value := range headers {
			r.Headers.Set(key, value)
		}
		
		// Add additional stealth headers
		r.Headers.Set("DNT", "1")
		r.Headers.Set("Sec-GPC", "1")
		r.Headers.Set("X-Requested-With", "")
		
		// Override some headers for better stealth
		r.Headers.Set("Sec-Fetch-Site", "cross-site")
		if r.Depth > 0 {
			r.Headers.Set("Referer", actualURL)
			r.Headers.Set("Sec-Fetch-Site", "same-origin")
		}
		
		// Add random small delay between requests to appear more human
		if r.Depth > 0 {
			time.Sleep(time.Duration(100+int(time.Now().UnixNano())%200) * time.Millisecond)
		}
		
		count := atomic.AddInt64(&pagesCrawled, 1)
		
		// Publish progress event (async, non-blocking)
		go publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("🔍 Crawling page %d at depth %d: %s", count, r.Depth, r.URL.String()),
			Timestamp: time.Now(),
			PageCount: int(count),
		})
	})
	
	startTime := time.Now()

	// Publish crawler ready progress
	publishCrawlEvent(CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  "Crawler initialized, starting to crawl...",
		Timestamp: time.Now(),
	})

	// Async link discovery with real-time events
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		if link == "" || shouldSkipURL(link) {
			return
		}

		// Convert to absolute URL
		absoluteURL := e.Request.AbsoluteURL(link)
		linkURL, err := url.Parse(absoluteURL)
		if err != nil {
			return
		}

		// Check if URL is from allowed domain
		isAllowed := false
		for _, domain := range allowedDomains {
			if linkURL.Host == domain {
				isAllowed = true
				break
			}
		}
		
		if isAllowed {
			// Clean the URL
			cleanURL := linkURL.Scheme + "://" + linkURL.Host + linkURL.Path
			if linkURL.RawQuery != "" {
				cleanURL += "?" + linkURL.RawQuery
			}
			
			// Thread-safe URL processing
			mu.Lock()
			if !foundURLs[cleanURL] && len(urlList) < maxURLs && !stopped {
				foundURLs[cleanURL] = true
				urlList = append(urlList, cleanURL)
				currentTotal := len(urlList)
				
				// Check if limit reached
				if currentTotal >= maxURLs {
					stopped = true
					publishCrawlEvent(CrawlEvent{
						Type:      "progress",
						JobID:     jobID,
						Progress:  fmt.Sprintf("⚠️ URL limit reached! Stopping at %d URLs", maxURLs),
						Timestamp: time.Now(),
					})
				} else {
					// Publish URL discovery event (async, non-blocking)
					go publishCrawlEvent(CrawlEvent{
						Type:      "url_discovered",
						JobID:     jobID,
						URL:       cleanURL,
						Depth:     e.Request.Depth,
						Timestamp: time.Now(),
						Total:     currentTotal,
					})
				}
				
				// Queue next visit if within depth and not stopped
				if e.Request.Depth < depth && !stopped {
					go func() {
						if !stopped {
							e.Request.Visit(cleanURL)
						}
					}()
				}
			}
			mu.Unlock()
		}
	})


	// Response tracking
	c.OnResponse(func(r *colly.Response) {
		mu.RLock()
		currentTotal := len(urlList)
		mu.RUnlock()
		
		// Publish progress event (async, non-blocking)
		go publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("Processed %s - Found %d URLs so far", r.Request.URL.String(), currentTotal),
			Timestamp: time.Now(),
			Total:     currentTotal,
		})
	})

	// Error handling
	c.OnError(func(r *colly.Response, err error) {
		// Publish error event (async, non-blocking)
		go publishCrawlEvent(CrawlEvent{
			Type:      "error",
			JobID:     jobID,
			Progress:  fmt.Sprintf("❌ Error crawling %s: %v", r.Request.URL.String(), err),
			Timestamp: time.Now(),
			Error:     err.Error(),
		})
	})

	// Start crawling
	publishCrawlEvent(CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  fmt.Sprintf("Starting to crawl %s...", actualURL),
		Timestamp: time.Now(),
	})

	// Visit initial URL (using the accessible URL)
	if err := c.Visit(actualURL); err != nil {
		return nil, fmt.Errorf("error visiting URL: %v", err)
	}

	// Wait for async crawler to complete
	c.Wait()

	// Calculate final stats
	mu.RLock()
	finalURLList := make([]string, len(urlList))
	copy(finalURLList, urlList)
	finalCount := len(urlList)
	mu.RUnlock()
	
	duration := time.Since(startTime)
	urlsPerSecond := float64(finalCount) / duration.Seconds()

	// Create structured result
	result := &CrawlResult{
		TargetURL:     actualURL, // Use the actually crawled URL
		CrawledAt:     time.Now(),
		Duration:      duration.String(),
		TotalURLs:     finalCount,
		URLsPerSecond: fmt.Sprintf("%.2f", urlsPerSecond),
		Settings: CrawlSettings{
			Workers: workers,
			Delay:   delayStr,
			Depth:   depth,
		},
		URLs: finalURLList,
	}

	// Send final completion message
	completionMessage := fmt.Sprintf("✅ Crawling completed! Found %d URLs in %s (%.2f URLs/sec)", finalCount, duration.String(), urlsPerSecond)
	if finalCount >= maxURLs {
		completionMessage = fmt.Sprintf("✅ Crawling completed! Found %d URLs (limit reached) in %s (%.2f URLs/sec)", finalCount, duration.String(), urlsPerSecond)
	}
	
	publishCrawlEvent(CrawlEvent{
		Type:      "completed",
		JobID:     jobID,
		Progress:  completionMessage,
		Timestamp: time.Now(),
		Total:     finalCount,
	})

	return result, nil
}

// crawlWebsite performs the actual crawling (moved from main function)
func crawlWebsite(targetURL string, depth, workers int, delayStr string) (*CrawlResult, error) {
	// Parse delay
	delay, err := time.ParseDuration(delayStr)
	if err != nil {
		delay = 200 * time.Millisecond
	}

	// Parse the target URL to get the base domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	// Create a new collector with optimized settings
	c := colly.NewCollector()
	c.Limit(&colly.LimitRule{
		Parallelism: workers,
		Delay:       delay,
	})
	c.SetRequestTimeout(30 * time.Second)

	// Allow both www and non-www versions of the domain
	baseDomain := parsedURL.Host
	allowedDomains := []string{baseDomain}
	if strings.HasPrefix(baseDomain, "www.") {
		allowedDomains = append(allowedDomains, baseDomain[4:])
	} else {
		allowedDomains = append(allowedDomains, "www."+baseDomain)
	}
	c.AllowedDomains = allowedDomains

	// Set user agent to be respectful
	c.UserAgent = "Go-Colly-Crawler/1.0"

	// Store found URLs to avoid duplicates (thread-safe)
	var mu sync.Mutex
	foundURLs := make(map[string]bool)
	var urlList []string
	startTime := time.Now()

	// Find all links
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")

		// Skip empty links
		if link == "" {
			return
		}

		// Skip non-content URLs (performance optimization)
		if shouldSkipURL(link) {
			return
		}

		// Convert relative URLs to absolute
		absoluteURL := e.Request.AbsoluteURL(link)

		// Parse the absolute URL
		linkURL, err := url.Parse(absoluteURL)
		if err != nil {
			return
		}

		// Only include URLs from the same domain (check against allowed domains)
		isAllowed := false
		for _, domain := range allowedDomains {
			if linkURL.Host == domain {
				isAllowed = true
				break
			}
		}

		if isAllowed {
			// Clean the URL (remove fragments)
			cleanURL := linkURL.Scheme + "://" + linkURL.Host + linkURL.Path
			if linkURL.RawQuery != "" {
				cleanURL += "?" + linkURL.RawQuery
			}

			// Thread-safe check and add
			mu.Lock()
			alreadyFound := foundURLs[cleanURL]
			if !alreadyFound {
				foundURLs[cleanURL] = true
				urlList = append(urlList, cleanURL)
				
				// Note: RabbitMQ events will be published from the caller
				// This keeps the crawler function lightweight
			}
			mu.Unlock()

			// Visit this URL if we haven't reached max depth and it's new
			if !alreadyFound && e.Request.Depth < depth {
				e.Request.Visit(cleanURL)
			}
		}
	})

	// Set up error handling
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Error occurred: %v", err)
	})

	// Start crawling
	err = c.Visit(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error visiting URL: %v", err)
	}

	// Calculate performance stats
	duration := time.Since(startTime)
	urlsPerSecond := float64(len(urlList)) / duration.Seconds()

	// Create structured result
	crawlTime := time.Now()
	result := &CrawlResult{
		TargetURL:     targetURL,
		CrawledAt:     crawlTime,
		Duration:      duration.String(),
		TotalURLs:     len(urlList),
		URLsPerSecond: fmt.Sprintf("%.2f", urlsPerSecond),
		Settings: CrawlSettings{
			Workers: workers,
			Delay:   delayStr,
			Depth:   depth,
		},
		URLs: urlList,
	}

	return result, nil
}

// handleCrawl handles the POST /crawl endpoint
// @Summary Start a new web crawl
// @Description Initiates a web crawling job for the specified URL with configurable parameters
// @Tags crawl
// @Accept json
// @Produce json
// @Param request body CrawlRequest true "Crawl parameters"
// @Success 200 {object} CrawlResponse
// @Failure 400 {object} map[string]string
// @Router /crawl [post]
func handleCrawl(w http.ResponseWriter, r *http.Request) {
	var req CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate URL
	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Depth == 0 {
		req.Depth = 1
	}
	if req.Workers == 0 {
		req.Workers = 10
	}
	if req.Delay == "" {
		req.Delay = "200ms"
	}
	if req.MaxURLs == 0 {
		req.MaxURLs = 1000 // Default limit
	}
	// Enforce maximum limit for safety
	if req.MaxURLs > 5000 {
		req.MaxURLs = 5000
	}

	// Use provided job ID or generate one
	var jobID string
	if req.JobID != "" {
		// Validate custom job ID (alphanumeric, hyphens, underscores only)
		if !isValidJobID(req.JobID) {
			http.Error(w, "Invalid job_id format. Use alphanumeric characters, hyphens, and underscores only", http.StatusBadRequest)
			return
		}
		// Check if job ID already exists
		jobsMutex.RLock()
		_, exists := activeJobs[req.JobID]
		jobsMutex.RUnlock()
		if exists {
			http.Error(w, "Job ID already exists. Choose a different job_id or omit it for auto-generation", http.StatusConflict)
			return
		}
		jobID = req.JobID
	} else {
		// Auto-generate job ID
		jobID = primitive.NewObjectID().Hex()
	}

	// Create job status
	job := &JobStatus{
		ID:        jobID,
		Status:    "running",
		Progress:  "Starting crawl...",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Request:   &req,
	}

	// Store job in MongoDB
	if err := saveJobToMongoDB(job); err != nil {
		log.Printf("Failed to save job to MongoDB: %v", err)
		// Continue anyway - store in memory as fallback
	}

	// Store job status in memory for fast access
	jobsMutex.Lock()
	activeJobs[jobID] = job
	jobsMutex.Unlock()

	// Start crawling in background
	go func() {
		// Send initial progress event
		publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  "Starting crawl...",
			Timestamp: time.Now(),
		})
		
		result, err := crawlWebsiteWithEvents(req.URL, req.Depth, req.Workers, req.Delay, req.MaxURLs, jobID)
		
		jobsMutex.Lock()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "completed"
			
			// Save crawl result to MongoDB if connected and get the inserted ID
			if crawlCollection != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				insertResult, err := crawlCollection.InsertOne(ctx, result)
				if err == nil {
					// Update the result with the actual MongoDB ID
					if oid, ok := insertResult.InsertedID.(primitive.ObjectID); ok {
						result.ID = oid
					}
				}
			}
			
			job.Result = result
			
			// Publish completion event to RabbitMQ
			publishCrawlEvent(CrawlEvent{
				Type:      "completed",
				JobID:     jobID,
				Progress:  fmt.Sprintf("Crawl completed! Found %d URLs", len(result.URLs)),
				Timestamp: time.Now(),
				Total:     len(result.URLs),
			})
		}
		job.UpdatedAt = time.Now()
		
		// Update job in MongoDB
		if err := updateJobInMongoDB(job); err != nil {
			log.Printf("Failed to update job in MongoDB: %v", err)
		}
		
		jobsMutex.Unlock()
	}()

	// Return immediate response
	response := CrawlResponse{
		JobID:   jobID,
		Status:  "accepted",
		Message: "Crawl job started successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleJobStatus handles the GET /jobs/{id} endpoint
// @Summary Get crawl job status
// @Description Retrieves the current status and progress of a crawl job
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} JobStatus
// @Failure 404 {object} map[string]string
// @Router /jobs/{id} [get]
func handleJobStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	// First check memory for active jobs (fastest)
	jobsMutex.RLock()
	job, exists := activeJobs[jobID]
	jobsMutex.RUnlock()

	if exists {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
		return
	}

	// If not in memory, check MongoDB
	job, err := getJobFromMongoDB(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// handleGetJobs handles the GET /jobs endpoint to list all jobs
// @Summary List recent jobs
// @Description Retrieves a list of recent jobs from the database
// @Tags jobs
// @Accept json
// @Produce json
// @Param limit query int false "Maximum number of results to return" default(10)
// @Param status query string false "Filter by job status (running, completed, failed)"
// @Success 200 {array} JobStatus
// @Failure 503 {object} map[string]string
// @Router /jobs [get]
func handleGetJobs(w http.ResponseWriter, r *http.Request) {
	if jobsCollection == nil {
		http.Error(w, "Jobs collection not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	statusFilter := r.URL.Query().Get("status")
	
	limit := int64(10) // default
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			limit = l
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build filter
	filter := bson.M{}
	if statusFilter != "" {
		filter["status"] = statusFilter
	}

	// Find jobs sorted by created_at descending
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := jobsCollection.Find(ctx, filter, opts)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var jobs []JobStatus
	if err := cursor.All(ctx, &jobs); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// handleGetCrawls handles the GET /crawls endpoint
// @Summary List recent crawl results
// @Description Retrieves a list of recent crawl results from the database
// @Tags crawls
// @Accept json
// @Produce json
// @Param limit query int false "Maximum number of results to return" default(10)
// @Success 200 {array} CrawlResult
// @Failure 503 {object} map[string]string
// @Router /crawls [get]
func handleGetCrawls(w http.ResponseWriter, r *http.Request) {
	if crawlCollection == nil {
		http.Error(w, "MongoDB not connected", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := int64(10) // default
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			limit = l
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find crawls sorted by crawled_at descending
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "crawled_at", Value: -1}})
	cursor, err := crawlCollection.Find(ctx, bson.D{}, opts)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var crawls []CrawlResult
	if err := cursor.All(ctx, &crawls); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(crawls)
}

// handleGetCrawlByID handles the GET /crawls/{id} endpoint
// @Summary Get specific crawl result
// @Description Retrieves a specific crawl result by its ID
// @Tags crawls
// @Accept json
// @Produce json
// @Param id path string true "Crawl ID"
// @Success 200 {object} CrawlResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /crawls/{id} [get]
func handleGetCrawlByID(w http.ResponseWriter, r *http.Request) {
	if crawlCollection == nil {
		http.Error(w, "MongoDB not connected", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]

	objectID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var crawl CrawlResult
	err = crawlCollection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&crawl)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			http.Error(w, "Crawl not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(crawl)
}

// handleWebSocket handles WebSocket connections for live job updates
// @Summary Connect to live crawl updates
// @Description Establishes a WebSocket connection to receive real-time updates for a specific crawl job
// @Tags websocket
// @Param id path string true "Job ID"
// @Router /ws/{id} [get]
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Create RabbitMQ queue for this job
	queueName, err := createJobQueue(jobID)
	if err != nil {
		log.Printf("Failed to create job queue: %v", err)
		conn.WriteJSON(WebSocketMessage{
			Type:      "error",
			JobID:     jobID,
			Error:     "Failed to create event queue",
			Timestamp: time.Now(),
		})
		return
	}

	// Send initial connection confirmation
	initialMessage := WebSocketMessage{
		Type:      "connected",
		JobID:     jobID,
		Progress:  "Connected to live updates",
		Timestamp: time.Now(),
	}
	if err := conn.WriteJSON(initialMessage); err != nil {
		log.Printf("Failed to send initial message: %v", err)
		return
	}

	// Create channels for event consumption
	eventChan := make(chan CrawlEvent, 100)
	stopChan := make(chan bool, 1)

	// Start consuming events from RabbitMQ
	if err := consumeJobEvents(queueName, eventChan, stopChan); err != nil {
		log.Printf("Failed to start consuming events: %v", err)
		conn.WriteJSON(WebSocketMessage{
			Type:      "error",
			JobID:     jobID,
			Error:     "Failed to start event consumption",
			Timestamp: time.Now(),
		})
		return
	}

	// Handle WebSocket connection lifecycle
	go func() {
		// Read messages from client (mainly for ping/pong)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				stopChan <- true
				break
			}
		}
	}()

	// Stream events from RabbitMQ to WebSocket
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				// Event channel closed
				return
			}

			// Convert CrawlEvent to WebSocketMessage
			wsMessage := WebSocketMessage{
				Type:      event.Type,
				JobID:     event.JobID,
				URL:       event.URL,
				Depth:     event.Depth,
				Progress:  event.Progress,
				Timestamp: event.Timestamp,
				Total:     event.Total,
				PageCount: event.PageCount,
				Error:     event.Error,
			}

			// Send to WebSocket client
			if err := conn.WriteJSON(wsMessage); err != nil {
				log.Printf("Failed to send WebSocket message: %v", err)
				stopChan <- true
				return
			}

		case <-stopChan:
			return
		}
	}
}

// startAPIServer starts the REST API server
func startAPIServer(port string, mongoURI, dbName, rabbitMQURL string) {
	// Initialize ScrapeOps for stealth headers
	log.Println("🔧 Initializing ScrapeOps stealth headers...")
	initScrapeOpsHeaders()
	
	// Initialize MongoDB
	if err := initMongoDB(mongoURI, dbName); err != nil {
		log.Printf("MongoDB initialization failed: %v", err)
		log.Println("API will run without MongoDB storage")
	} else {
		// Load any active jobs from previous sessions
		loadActiveJobsFromMongoDB()
	}
	
	// Initialize RabbitMQ
	if err := initRabbitMQ(rabbitMQURL); err != nil {
		log.Printf("RabbitMQ initialization failed: %v", err)
		log.Println("API will run without RabbitMQ messaging")
	}

	// Create router
	r := mux.NewRouter()

	// Add CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set CORS headers for all requests
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			
			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	})
	
	// Add global OPTIONS handler for all routes
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}).Methods("OPTIONS")

	// Define routes
	r.HandleFunc("/crawl", handleCrawl).Methods("POST", "OPTIONS")
	r.HandleFunc("/jobs", handleGetJobs).Methods("GET", "OPTIONS")
	r.HandleFunc("/jobs/{id}", handleJobStatus).Methods("GET", "OPTIONS")
	r.HandleFunc("/crawls", handleGetCrawls).Methods("GET", "OPTIONS")
	r.HandleFunc("/crawls/{id}", handleGetCrawlByID).Methods("GET", "OPTIONS")
	r.HandleFunc("/ws/{id}", handleWebSocket).Methods("GET", "OPTIONS")

	// Add health check endpoint
	// @Summary Health check
	// @Description Check if the API server is running
	// @Tags health
	// @Produce json
	// @Success 200 {object} map[string]string
	// @Router /health [get]
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET")

	// Swagger UI endpoint
	r.PathPrefix("/swagger/").Handler(httpSwagger.WrapHandler)

	log.Printf("Starting API server on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  POST /crawl - Start a new crawl")
	log.Printf("  GET  /jobs - List recent jobs")
	log.Printf("  GET  /jobs/{id} - Get job status")
	log.Printf("  GET  /crawls - List recent crawls")
	log.Printf("  GET  /crawls/{id} - Get specific crawl")
	log.Printf("  GET  /ws/{id} - WebSocket live updates")
	log.Printf("  GET  /health - Health check")
	log.Printf("  GET  /swagger/ - API documentation")

	log.Fatal(http.ListenAndServe(":"+port, r))
}


// isValidJobID validates custom job ID format
func isValidJobID(jobID string) bool {
	// Allow alphanumeric characters, hyphens, and underscores
	// Length between 3 and 50 characters
	if len(jobID) < 3 || len(jobID) > 50 {
		return false
	}
	
	for _, char := range jobID {
		if !((char >= 'a' && char <= 'z') || 
			(char >= 'A' && char <= 'Z') || 
			(char >= '0' && char <= '9') || 
			char == '-' || char == '_') {
			return false
		}
	}
	return true
}

// saveJobToMongoDB saves a job to the jobs collection
func saveJobToMongoDB(job *JobStatus) error {
	if jobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := jobsCollection.InsertOne(ctx, job)
	return err
}

// updateJobInMongoDB updates a job in the jobs collection
func updateJobInMongoDB(job *JobStatus) error {
	if jobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": job.ID}
	update := bson.M{"$set": job}

	_, err := jobsCollection.UpdateOne(ctx, filter, update)
	return err
}

// getJobFromMongoDB retrieves a job from the jobs collection
func getJobFromMongoDB(jobID string) (*JobStatus, error) {
	if jobsCollection == nil {
		return nil, fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var job JobStatus
	err := jobsCollection.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

// loadActiveJobsFromMongoDB loads running jobs from MongoDB on startup
func loadActiveJobsFromMongoDB() {
	if jobsCollection == nil {
		log.Println("Jobs collection not initialized, skipping job recovery")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find all running jobs
	cursor, err := jobsCollection.Find(ctx, bson.M{"status": "running"})
	if err != nil {
		log.Printf("Failed to load active jobs from MongoDB: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var recoveredJobs []JobStatus
	if err := cursor.All(ctx, &recoveredJobs); err != nil {
		log.Printf("Failed to decode active jobs: %v", err)
		return
	}

	// Load recovered jobs into memory
	jobsMutex.Lock()
	for _, job := range recoveredJobs {
		// Mark recovered jobs as failed since the process was interrupted
		job.Status = "failed"
		job.Error = "Job interrupted by server restart"
		job.UpdatedAt = time.Now()
		
		activeJobs[job.ID] = &job
		
		// Update status in MongoDB
		go updateJobInMongoDB(&job)
	}
	jobsMutex.Unlock()

	log.Printf("Recovered %d interrupted jobs from MongoDB", len(recoveredJobs))
}