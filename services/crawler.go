package services

import (
	"crawler/config"
	"crawler/models"
	"crawler/utils"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/gocolly/colly/v2"
)

// cleanURL removes whitespace, normalizes the URL, and handles common issues
func cleanURL(rawURL string) string {
	// Trim all whitespace characters including newlines, tabs, etc.
	cleaned := strings.TrimSpace(rawURL)
	
	// Remove any control characters and extra whitespace
	cleaned = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1 // Remove control characters
		}
		return r
	}, cleaned)
	
	// Replace any remaining newlines or tabs with empty string
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	
	// Trim again after cleanup
	cleaned = strings.TrimSpace(cleaned)
	
	return cleaned
}

// CrawlWebsiteWithTiers performs three-tier crawling with smart fallbacks
func CrawlWebsiteWithTiers(targetURL string, crawlConfig models.CrawlRequest, jobID string) (*models.CrawlResult, error) {
	var allURLs []string
	var tierStats models.CrawlTierStats
	startTime := time.Now()
	
	// Try URL fallback to find an accessible URL
	actualURL, fallbackInfo := utils.FindAccessibleURL(targetURL, jobID)
	if !fallbackInfo.Success {
		// If URL fallback fails, try to find sitemaps via robots.txt
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("⚠️ URL and all fallbacks failed: %v - trying robots.txt fallback", fallbackInfo.Error),
			Timestamp: time.Now(),
			Tier:      "sitemap",
		})
		
		robotsSitemaps := ParseRobotsTxt(targetURL, jobID)
		if len(robotsSitemaps) > 0 {
			// Use the original URL but indicate we're using robots.txt approach
			actualURL = targetURL
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("✅ Found %d sitemap(s) via robots.txt fallback", len(robotsSitemaps)),
				Timestamp: time.Now(),
				Tier:      "sitemap",
			})
		} else {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ URL and all fallbacks failed: %v", fallbackInfo.Error),
				Timestamp: time.Now(),
				Error:     fallbackInfo.Error,
			})
			return nil, fmt.Errorf("URL and all fallbacks are inaccessible: %v", fallbackInfo.Error)
		}
	}
	
	// If a fallback was used, publish success event
	if fallbackInfo.FallbackURL != fallbackInfo.OriginalURL {
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("✅ Fallback successful! Using: %s instead of %s", fallbackInfo.FallbackURL, fallbackInfo.OriginalURL),
			Timestamp: time.Now(),
		})
	}
	
	// Publish initial setup progress
	stealthStatus := "basic headers"
	if len(config.GlobalScrapeOpsConfig.UserAgents) > 0 {
		stealthStatus = fmt.Sprintf("ScrapeOps stealth (%d user agents, %d header sets)", len(config.GlobalScrapeOpsConfig.UserAgents), len(config.GlobalScrapeOpsConfig.Headers))
	}
	
	PublishCrawlEvent(models.CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  fmt.Sprintf("🚀 Starting three-tier crawling for %s (%s)", actualURL, stealthStatus),
		Timestamp: time.Now(),
	})
	
	// Set defaults for tier enablement if not specified
	enableSitemap := crawlConfig.EnableSitemap || (!crawlConfig.EnableSitemap && !crawlConfig.EnableHTML && !crawlConfig.EnableHeadless) // Default true if nothing specified
	enableHTML := crawlConfig.EnableHTML || (!crawlConfig.EnableSitemap && !crawlConfig.EnableHTML && !crawlConfig.EnableHeadless)    // Default true if nothing specified
	_ = crawlConfig.EnableHeadless // Note: Headless tier not implemented in this version
	
	// Tier 1: Sitemap Crawling (Primary)
	if enableSitemap {
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "tier_switch",
			JobID:     jobID,
			Progress:  "🗺️ Starting Tier 1: Sitemap Discovery",
			Timestamp: time.Now(),
			Tier:      "sitemap",
		})
		
		// If we used robots.txt fallback, pass those sitemaps to CrawlWithSitemaps
		var robotsSitemaps []string
		if !fallbackInfo.Success {
			robotsSitemaps = ParseRobotsTxt(targetURL, jobID)
		}
		
		sitemapURLs, err := CrawlWithSitemapsAndFallback(actualURL, jobID, robotsSitemaps)
		if err != nil {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ Sitemap crawling failed: %v", err),
				Timestamp: time.Now(),
				Tier:      "sitemap",
				Error:     err.Error(),
			})
		} else {
			allURLs = append(allURLs, sitemapURLs...)
			tierStats.SitemapURLs = len(sitemapURLs)
			if len(sitemapURLs) > 0 {
				tierStats.TotalTiers++
			}
		}
	}
	
	// Tier 2: HTML Link Crawling (Fallback)
	shouldUseHTML := enableHTML && shouldFallbackToHTML(allURLs, crawlConfig)
	if shouldUseHTML {
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "tier_switch",
			JobID:     jobID,
			Progress:  "🔗 Starting Tier 2: HTML Link Discovery",
			Timestamp: time.Now(),
			Tier:      "html",
		})
		
		htmlURLs, err := CrawlWithHTML(actualURL, crawlConfig, jobID)
		if err != nil {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ HTML crawling failed: %v", err),
				Timestamp: time.Now(),
				Tier:      "html",
				Error:     err.Error(),
			})
		} else {
			// Merge and deduplicate
			beforeCount := len(allURLs)
			allURLs = removeDuplicateURLs(append(allURLs, htmlURLs...))
			newURLs := len(allURLs) - beforeCount
			tierStats.HTMLURLs = newURLs
			if newURLs > 0 {
				tierStats.TotalTiers++
			}
			
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("🔗 HTML crawling added %d new URLs (total: %d)", newURLs, len(allURLs)),
				Timestamp: time.Now(),
				Tier:      "html",
				Total:     len(allURLs),
			})
		}
	}
	
	// Apply max URL limit
	if crawlConfig.MaxURLs > 0 && len(allURLs) > crawlConfig.MaxURLs {
		allURLs = allURLs[:crawlConfig.MaxURLs]
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("⚠️ Applied URL limit: truncated to %d URLs", crawlConfig.MaxURLs),
			Timestamp: time.Now(),
		})
	}
	
	// Calculate final stats
	duration := time.Since(startTime)
	urlsPerSecond := float64(len(allURLs)) / duration.Seconds()
	
	// Create structured result
	result := &models.CrawlResult{
		TargetURL:     actualURL, // Use the actualURL that was successfully accessed
		CrawledAt:     time.Now(),
		Duration:      duration.String(),
		TotalURLs:     len(allURLs),
		URLsPerSecond: fmt.Sprintf("%.2f", urlsPerSecond),
		Settings: models.CrawlSettings{
			Workers: crawlConfig.Workers,
			Delay:   crawlConfig.Delay,
			Depth:   crawlConfig.Depth,
		},
		URLs: allURLs,
	}
	
	// Send final completion message
	completionMessage := fmt.Sprintf("✅ Three-tier crawling completed! Found %d URLs in %s (%.2f URLs/sec) using %d tiers", 
		len(allURLs), duration.String(), urlsPerSecond, tierStats.TotalTiers)
	
	PublishCrawlEvent(models.CrawlEvent{
		Type:      "completed",
		JobID:     jobID,
		Progress:  completionMessage,
		Timestamp: time.Now(),
		Total:     len(allURLs),
	})
	
	return result, nil
}

// CrawlWithSitemaps performs sitemap-based crawling
func CrawlWithSitemaps(targetURL, jobID string) ([]string, error) {
	return CrawlWithSitemapsAndFallback(targetURL, jobID, nil)
}

// CrawlWithSitemapsAndFallback performs sitemap-based crawling with optional robots.txt fallback
func CrawlWithSitemapsAndFallback(targetURL, jobID string, robotsSitemaps []string) ([]string, error) {
	var allURLs []string
	
	// Discover sitemaps with optional robots.txt fallback
	sitemapURLs := DiscoverSitemapsWithFallback(targetURL, jobID, robotsSitemaps)
	if len(sitemapURLs) == 0 {
		PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  "⚠️ No sitemaps found",
			Timestamp: time.Now(),
			Tier:      "sitemap",
		})
		return allURLs, nil
	}
	
	// Parse each discovered sitemap
	for _, sitemapURL := range sitemapURLs {
		urls, err := ParseSitemap(sitemapURL, jobID)
		if err != nil {
			PublishCrawlEvent(models.CrawlEvent{
				Type:      "error",
				JobID:     jobID,
				Progress:  fmt.Sprintf("❌ Failed to parse sitemap %s: %v", sitemapURL, err),
				Timestamp: time.Now(),
				Tier:      "sitemap",
				Error:     err.Error(),
			})
			continue
		}
		allURLs = append(allURLs, urls...)
	}
	
	// Remove duplicates
	uniqueURLs := removeDuplicateURLs(allURLs)
	
	PublishCrawlEvent(models.CrawlEvent{
		Type:      "progress",
		JobID:     jobID,
		Progress:  fmt.Sprintf("🎯 Sitemap crawling complete: %d unique URLs discovered", len(uniqueURLs)),
		Timestamp: time.Now(),
		Tier:      "sitemap",
		Total:     len(uniqueURLs),
	})
	
	return uniqueURLs, nil
}

// CrawlWithHTML performs HTML link crawling
func CrawlWithHTML(targetURL string, config models.CrawlRequest, jobID string) ([]string, error) {
	result, err := crawlWebsiteWithEvents(targetURL, config.Depth, config.Workers, config.Delay, config.MaxURLs, jobID)
	if err != nil {
		return nil, err
	}
	return result.URLs, nil
}

// shouldFallbackToHTML determines if HTML crawling should be used
func shouldFallbackToHTML(currentURLs []string, config models.CrawlRequest) bool {
	return len(currentURLs) < 10
}

// shouldFallbackToHeadless determines if headless browser crawling should be used
func shouldFallbackToHeadless(currentURLs []string, config models.CrawlRequest) bool {
	return len(currentURLs) < 5
}

// removeDuplicateURLs removes duplicate URLs from a slice
func removeDuplicateURLs(urls []string) []string {
	seen := make(map[string]bool)
	var unique []string
	
	for _, url := range urls {
		if !seen[url] {
			seen[url] = true
			unique = append(unique, url)
		}
	}
	
	return unique
}

// crawlWebsiteWithEvents performs async crawling with real-time events (legacy function for HTML crawling)
func crawlWebsiteWithEvents(targetURL string, depth, workers int, delayStr string, maxURLs int, jobID string) (*models.CrawlResult, error) {
	// Parse delay
	delay, err := time.ParseDuration(delayStr)
	if err != nil {
		delay = 200 * time.Millisecond
	}

	// Try URL fallback to find an accessible URL
	actualURL, fallbackInfo := utils.FindAccessibleURL(targetURL, jobID)
	if !fallbackInfo.Success {
		return nil, fmt.Errorf("URL and all fallbacks are inaccessible: %v", fallbackInfo.Error)
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
	c.UserAgent = GetScrapeOpsUserAgent() // Set a base user agent

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
		headers := GetScrapeOpsBrowserHeaders()
		for key, value := range headers {
			r.Headers.Set(key, value)
		}
		
		// Disable compression to get raw HTML for debugging
		r.Headers.Set("Accept-Encoding", "identity")
		
		count := atomic.AddInt64(&pagesCrawled, 1)
		
		// Publish progress event (async, non-blocking)
		go PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("🔍 Crawling page %d at depth %d: %s", count, r.Depth, r.URL.String()),
			Timestamp: time.Now(),
			PageCount: int(count),
		})
	})

	// Add response debugging
	c.OnResponse(func(r *colly.Response) {
		bodyPreview := string(r.Body)
		if len(bodyPreview) > 200 {
			bodyPreview = bodyPreview[:200] + "..."
		}
		go PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("📄 Response: %d bytes, status %d, content-type: %s", len(r.Body), r.StatusCode, r.Headers.Get("Content-Type")),
			Timestamp: time.Now(),
		})
		go PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  fmt.Sprintf("📄 Content preview: %s", bodyPreview),
			Timestamp: time.Now(),
		})
	})

	// Add HTML debugging  
	c.OnHTML("*", func(e *colly.HTMLElement) {
		if e.Request.Depth == 0 { // Only log for the main page
			linkCount := len(e.ChildAttrs("a", "href"))
			go PublishCrawlEvent(models.CrawlEvent{
				Type:      "progress",
				JobID:     jobID,
				Progress:  fmt.Sprintf("🔍 Found %d total <a> tags on main page", linkCount),
				Timestamp: time.Now(),
			})
		}
	})

	// Add error debugging
	c.OnError(func(r *colly.Response, err error) {
		go PublishCrawlEvent(models.CrawlEvent{
			Type:      "error",
			JobID:     jobID,
			Progress:  fmt.Sprintf("❌ Colly error: %v for URL: %s", err, r.Request.URL.String()),
			Timestamp: time.Now(),
			Error:     err.Error(),
		})
	})
	
	startTime := time.Now()

	// Async link discovery with real-time events
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := cleanURL(e.Attr("href"))
		if link == "" || shouldSkipURL(link) {
			// Debug: log why links are being skipped
			if link == "" {
				go PublishCrawlEvent(models.CrawlEvent{
					Type:      "progress",
					JobID:     jobID,
					Progress:  fmt.Sprintf("🔍 Debug: Empty link found on %s", e.Request.URL.String()),
					Timestamp: time.Now(),
				})
			}
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
				} else {
					// Publish URL discovery event (async, non-blocking)
					go PublishCrawlEvent(models.CrawlEvent{
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

	// Start crawling using the accessible URL
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
	result := &models.CrawlResult{
		TargetURL:     actualURL, // Use the actualURL that was successfully accessed
		CrawledAt:     time.Now(),
		Duration:      duration.String(),
		TotalURLs:     finalCount,
		URLsPerSecond: fmt.Sprintf("%.2f", urlsPerSecond),
		Settings: models.CrawlSettings{
			Workers: workers,
			Delay:   delayStr,
			Depth:   depth,
		},
		URLs: finalURLList,
	}

	return result, nil
}

// shouldSkipURL filters out common non-content file types and patterns
func shouldSkipURL(link string) bool {
	// Convert to lowercase for case-insensitive matching
	link = strings.ToLower(link)
	
	// Skip common file extensions that don't contain links
	skipExtensions := []string{".css", ".js", ".jpg", ".jpeg", ".png", ".gif", ".svg", ".ico", ".pdf", ".zip", ".tar", ".gz", ".mp4", ".mp3", ".avi", ".mov", ".wmv", ".flv", ".swf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"}
	
	for _, ext := range skipExtensions {
		if strings.HasSuffix(link, ext) {
			return true
		}
	}
	
	// Skip common non-content paths
	skipPatterns := []string{"/admin", "/login", "/logout", "/register", "/signin", "/signup", "/auth", "/api/", "/assets/", "/static/", "/images/", "/img/", "/css/", "/js/", "/fonts/", "mailto:", "tel:", "javascript:", "#"}
	
	for _, pattern := range skipPatterns {
		if strings.Contains(link, pattern) {
			return true
		}
	}
	
	return false
}