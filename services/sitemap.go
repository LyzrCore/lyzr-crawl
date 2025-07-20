package services

import (
	"compress/gzip"
	"crawler/models"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DiscoverSitemaps discovers sitemap URLs from common locations
func DiscoverSitemaps(baseURL, jobID string) []string {
	var sitemapURLs []string
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return sitemapURLs
	}
	
	baseScheme := parsed.Scheme
	baseHost := parsed.Host
	
	// Common sitemap locations to check
	candidates := []string{
		fmt.Sprintf("%s://%s/sitemap.xml", baseScheme, baseHost),
		fmt.Sprintf("%s://%s/sitemap_index.xml", baseScheme, baseHost),
		fmt.Sprintf("%s://%s/sitemaps.xml", baseScheme, baseHost),
		fmt.Sprintf("%s://%s/sitemap/sitemap.xml", baseScheme, baseHost),
	}
	
	PublishCrawlEvent(models.CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  "🗺️ Discovering sitemaps...",
		Timestamp: time.Now(),
		Tier:      "sitemap",
	})
	
	// Check each candidate URL
	for _, candidate := range candidates {
		if CheckSitemapExists(candidate) {
			sitemapURLs = append(sitemapURLs, candidate)
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "sitemap_discovered",
				JobID:     jobID,
				URL:       candidate,
				Progress:  fmt.Sprintf("📍 Found sitemap: %s", candidate),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
	}
	
	// Also check robots.txt for sitemap references
	robotsURL := fmt.Sprintf("%s://%s/robots.txt", baseScheme, baseHost)
	robotsSitemaps := ExtractSitemapsFromRobots(robotsURL)
	for _, sitemapURL := range robotsSitemaps {
		if CheckSitemapExists(sitemapURL) {
			sitemapURLs = append(sitemapURLs, sitemapURL)
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "sitemap_discovered",
				JobID:     jobID,
				URL:       sitemapURL,
				Progress:  fmt.Sprintf("📍 Found sitemap in robots.txt: %s", sitemapURL),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		}
	}
	
	return sitemapURLs
}

// CheckSitemapExists checks if a sitemap URL exists and is valid
func CheckSitemapExists(sitemapURL string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("HEAD", sitemapURL, nil)
	if err != nil {
		return false
	}
	
	SetBrowserHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == 200
}

// ExtractSitemapsFromRobots extracts sitemap URLs from robots.txt
func ExtractSitemapsFromRobots(robotsURL string) []string {
	var sitemaps []string
	
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", robotsURL, nil)
	if err != nil {
		return sitemaps
	}
	
	SetBrowserHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return sitemaps
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return sitemaps
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return sitemaps
	}
	
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "sitemap:") {
			sitemapURL := strings.TrimSpace(line[8:]) // Remove "sitemap:" prefix
			if sitemapURL != "" {
				sitemaps = append(sitemaps, sitemapURL)
			}
		}
	}
	
	return sitemaps
}

// ParseSitemap parses a sitemap XML and returns URLs
func ParseSitemap(sitemapURL, jobID string) ([]string, error) {
	var urls []string
	
	PublishCrawlEvent(models.CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  fmt.Sprintf("📄 Parsing sitemap: %s", sitemapURL),
		Timestamp: time.Now(),
		Tier:      "sitemap",
	})
	
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", sitemapURL, nil)
	if err != nil {
		return urls, err
	}
	
	SetBrowserHeaders(req)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	resp, err := client.Do(req)
	if err != nil {
		return urls, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return urls, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	
	// Handle gzip compression
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return urls, fmt.Errorf("failed to create gzip reader: %v", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}
	
	body, err := io.ReadAll(reader)
	if err != nil {
		return urls, err
	}
	
	// Try parsing as sitemap index first
	var sitemapIndex models.SitemapIndex
	if err := xml.Unmarshal(body, &sitemapIndex); err == nil && len(sitemapIndex.Sitemaps) > 0 {
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("📂 Found sitemap index with %d sitemaps", len(sitemapIndex.Sitemaps)),
			Timestamp: time.Now(),
			Tier:      "sitemap",
		})
		
		// Recursively parse each referenced sitemap
		for _, ref := range sitemapIndex.Sitemaps {
			subURLs, err := ParseSitemap(ref.Loc, jobID)
			if err != nil {
				log.Printf("Failed to parse sub-sitemap %s: %v", ref.Loc, err)
				continue
			}
			urls = append(urls, subURLs...)
		}
		return urls, nil
	}
	
	// Try parsing as regular sitemap
	var sitemapSet models.SitemapSet
	if err := xml.Unmarshal(body, &sitemapSet); err != nil {
		// Log the first few bytes of the response for debugging
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("Failed to parse sitemap %s, response preview: %q", sitemapURL, preview)
		return urls, fmt.Errorf("failed to parse sitemap XML: %v", err)
	}
	
	for _, urlEntry := range sitemapSet.URLs {
		if urlEntry.Loc != "" {
			urls = append(urls, urlEntry.Loc)
		}
	}
	
	PublishCrawlEvent(models.CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  fmt.Sprintf("✅ Extracted %d URLs from sitemap", len(urls)),
		Timestamp: time.Now(),
		Tier:      "sitemap",
		Total:     len(urls),
	})
	
	return urls, nil
}