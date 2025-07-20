package utils

import (
	"crawler/models"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SetBrowserHeaders sets realistic browser headers to bypass bot detection
func SetBrowserHeaders(req *http.Request) {
	// Use basic fallback headers since we can't import services (circular dependency)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Connection", "keep-alive")
}

// CheckURLAccessibility performs a request to check if a URL is accessible
func CheckURLAccessibility(urlStr string) error {
	_, err := url.Parse(urlStr)
	if err != nil {
		return err
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
			SetBrowserHeaders(req)
			return nil
		},
	}
	
	// Try GET request with minimal response reading
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	
	// Set realistic browser headers to bypass bot detection
	SetBrowserHeaders(req)
	
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
	
	// For 403, return error with helpful message
	if resp.StatusCode == 403 {
		return fmt.Errorf("HTTP 403: Forbidden (may need different approach)")
	}
	
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
}

// GenerateFallbackURLs generates alternative URLs to try if the original fails
func GenerateFallbackURLs(originalURL string) []string {
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

// FindAccessibleURL tries the original URL and fallbacks, returns the first accessible one
func FindAccessibleURL(originalURL string, jobID string) (string, *models.URLFallback) {
	fallbackInfo := &models.URLFallback{
		OriginalURL: originalURL,
		FallbackURL: originalURL,
		Success:     false,
	}
	
	// First try the original URL
	err := CheckURLAccessibility(originalURL)
	if err == nil {
		fallbackInfo.Success = true
		return originalURL, fallbackInfo
	}
	
	// Store the original error
	fallbackInfo.Error = err.Error()
	
	// Try fallback URLs
	fallbacks := GenerateFallbackURLs(originalURL)
	for _, fallbackURL := range fallbacks {
		err := CheckURLAccessibility(fallbackURL)
		if err == nil {
			fallbackInfo.FallbackURL = fallbackURL
			fallbackInfo.Success = true
			return fallbackURL, fallbackInfo
		}
	}
	
	// All URLs failed
	return originalURL, fallbackInfo
}