package services

import (
	"crawler/config"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// fetchScrapeOpsUserAgents fetches fresh user agents from ScrapeOps API
func FetchScrapeOpsUserAgents() error {
	url := fmt.Sprintf("https://headers.scrapeops.io/v1/user-agents?api_key=%s&num_results=50", config.GlobalScrapeOpsConfig.APIKey)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch user agents: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("ScrapeOps API returned status %d", resp.StatusCode)
	}
	
	var response config.ScrapeOpsUserAgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}
	
	// Extract user agents - response.Result is now a string array
	userAgents := response.Result
	
	config.GlobalScrapeOpsConfig.UserAgents = userAgents
	config.GlobalScrapeOpsConfig.LastUpdate = time.Now()
	
	log.Printf("Fetched %d user agents from ScrapeOps", len(userAgents))
	return nil
}

// fetchScrapeOpsBrowserHeaders fetches fresh browser headers from ScrapeOps API
func FetchScrapeOpsBrowserHeaders() error {
	url := fmt.Sprintf("https://headers.scrapeops.io/v1/browser-headers?api_key=%s&num_results=20", config.GlobalScrapeOpsConfig.APIKey)
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch browser headers: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return fmt.Errorf("ScrapeOps API returned status %d", resp.StatusCode)
	}
	
	var response config.ScrapeOpsHeaderResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}
	
	config.GlobalScrapeOpsConfig.Headers = make([]map[string]string, len(response.Result))
	for i, header := range response.Result {
		config.GlobalScrapeOpsConfig.Headers[i] = map[string]string(header)
	}
	config.GlobalScrapeOpsConfig.LastUpdate = time.Now()
	
	log.Printf("Fetched %d browser header sets from ScrapeOps", len(response.Result))
	return nil
}

// InitScrapeOpsHeaders initializes ScrapeOps headers and user agents
func InitScrapeOpsHeaders() {
	// Fetch user agents
	if err := FetchScrapeOpsUserAgents(); err != nil {
		log.Printf("Failed to fetch ScrapeOps user agents: %v", err)
		log.Println("Falling back to static user agents")
	}
	
	// Fetch browser headers
	if err := FetchScrapeOpsBrowserHeaders(); err != nil {
		log.Printf("Failed to fetch ScrapeOps browser headers: %v", err)
		log.Println("Falling back to static headers")
	}
}

// GetScrapeOpsUserAgent returns a random user agent from ScrapeOps or fallback
func GetScrapeOpsUserAgent() string {
	// Refresh if data is older than 1 hour
	if time.Since(config.GlobalScrapeOpsConfig.LastUpdate) > time.Hour {
		go InitScrapeOpsHeaders() // Refresh in background
	}
	
	if len(config.GlobalScrapeOpsConfig.UserAgents) > 0 {
		return config.GlobalScrapeOpsConfig.UserAgents[int(time.Now().UnixNano())%len(config.GlobalScrapeOpsConfig.UserAgents)]
	}
	
	// Fallback to static user agents if ScrapeOps is unavailable
	return GetRandomUserAgent()
}

// GetScrapeOpsBrowserHeaders returns random browser headers from ScrapeOps or fallback
func GetScrapeOpsBrowserHeaders() map[string]string {
	if len(config.GlobalScrapeOpsConfig.Headers) > 0 {
		return config.GlobalScrapeOpsConfig.Headers[int(time.Now().UnixNano())%len(config.GlobalScrapeOpsConfig.Headers)]
	}
	
	// Fallback to basic headers
	return map[string]string{
		"User-Agent":                GetRandomUserAgent(),
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

// GetRandomUserAgent returns a random realistic user agent (fallback)
func GetRandomUserAgent() string {
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

// SetBrowserHeaders sets realistic browser headers to bypass bot detection
func SetBrowserHeaders(req *http.Request) {
	// Use ScrapeOps headers for maximum stealth
	headers := GetScrapeOpsBrowserHeaders()
	
	for key, value := range headers {
		req.Header.Set(key, value)
	}
}