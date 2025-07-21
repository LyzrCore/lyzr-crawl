package services

import (
	"crawler/models"
	"crawler/utils"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ParseRobotsTxt fetches and parses robots.txt to find sitemaps
func ParseRobotsTxt(baseURL, jobID string) []string {
	var sitemaps []string

	// Parse the base URL
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		if jobID != "" {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ Failed to parse URL for robots.txt: %v", err),
				Timestamp: time.Now(),
				Tier:      "sitemap",
				Error:     err.Error(),
			})
		}
		return sitemaps
	}

	// Construct robots.txt URL
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", parsedURL.Scheme, parsedURL.Host)

	if jobID != "" {
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("🤖 Checking robots.txt: %s", robotsURL),
			Timestamp: time.Now(),
			Tier:      "sitemap",
		})
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequest("GET", robotsURL, nil)
	if err != nil {
		if jobID != "" {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ Failed to create robots.txt request: %v", err),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
		return sitemaps
	}

	// Set browser headers
	utils.SetBrowserHeaders(req)
	req.Header.Set("Accept-Encoding", "identity")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		if jobID != "" {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("⚠️ Could not fetch robots.txt: %v", err),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
		return sitemaps
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != 200 {
		if jobID != "" {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("⚠️ robots.txt returned status %d", resp.StatusCode),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
		return sitemaps
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if jobID != "" {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ Failed to read robots.txt: %v", err),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
		return sitemaps
	}

	// Parse robots.txt content
	content := string(body)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for sitemap entries
		if strings.HasPrefix(strings.ToLower(line), "sitemap:") {
			sitemapURL := strings.TrimSpace(line[8:]) // Remove "sitemap:" prefix
			if sitemapURL != "" {
				// Make sure it's an absolute URL
				if strings.HasPrefix(sitemapURL, "http://") || strings.HasPrefix(sitemapURL, "https://") {
					sitemaps = append(sitemaps, sitemapURL)
				} else {
					// Convert relative URL to absolute
					absoluteURL := fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, sitemapURL)
					sitemaps = append(sitemaps, absoluteURL)
				}
			}
		}
	}

	if jobID != "" {
		if len(sitemaps) > 0 {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("🤖 Found %d sitemap(s) in robots.txt", len(sitemaps)),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		} else {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  "🤖 No sitemaps found in robots.txt",
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
	}

	return sitemaps
}