package services

import (
	"crawler/models"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func cleanText(text string) string {
	spaceRegex := regexp.MustCompile(`\s+`)
	text = spaceRegex.ReplaceAllString(text, " ")

	newlineRegex := regexp.MustCompile(`\n\s*\n\s*`)
	text = newlineRegex.ReplaceAllString(text, "\n\n")

	text = strings.TrimSpace(text)

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
		`\[.*?\]\(#[^)]*\)`,
	}

	for _, pattern := range boilerplatePatterns {
		regex := regexp.MustCompile(pattern)
		text = regex.ReplaceAllString(text, "")
	}

	return strings.TrimSpace(text)
}

func convertToMarkdown(htmlContent string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", err
	}

	doc.Find("script, style, noscript, iframe, object, embed").Remove()
	doc.Find("nav, header, footer, aside, .nav, .navbar, .sidebar, .menu").Remove()
	doc.Find(".ad, .ads, .advertisement, .google-ad, .banner, .popup, .modal").Remove()
	doc.Find(".social, .share, .facebook, .twitter, .instagram, .linkedin").Remove()
	doc.Find(".comments, .comment, #comments, #comment").Remove()
	doc.Find("img[src^='data:']").Remove()
	doc.Find("img[data-src]").Remove()
	doc.Find(".button, .btn, button").Remove()
	doc.Find(".scroll, .skip, .toggle").Remove()
	doc.Find("[class*='cookie'], [class*='gdpr']").Remove()
	doc.Find(".elementor-action, .popup").Remove()

	var result strings.Builder

	if title := strings.TrimSpace(doc.Find("title").Text()); title != "" {
		result.WriteString("# " + title + "\n\n")
	}

	if metaDesc, exists := doc.Find("meta[name='description']").Attr("content"); exists {
		metaDesc = strings.TrimSpace(metaDesc)
		if metaDesc != "" {
			result.WriteString("*" + metaDesc + "*\n\n")
		}
	}

	processElementToMarkdown(doc.Find("body"), &result, 0)

	return cleanMarkdownText(result.String()), nil
}

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
					if j == 0 {
						tr.Find("td, th").Each(func(k int, cell *goquery.Selection) {
							result.WriteString("|---")
						})
						result.WriteString("|\n")
					}
				})
				result.WriteString("\n")
			default:
				processElementToMarkdown(s, result, depth+1)
			}
		}
	})
}

func cleanMarkdownText(text string) string {
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	text = regexp.MustCompile(`\n\s*\n\s*(#{1,6})`).ReplaceAllString(text, "\n\n$1")
	text = regexp.MustCompile(`(#{1,6}[^\n]*)\n\s*\n\s*`).ReplaceAllString(text, "$1\n\n")
	text = regexp.MustCompile(` +`).ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

func tryJSDOMRendering(targetURL string) (string, error) {
	fmt.Printf("[TIER 1 - JSDOM] Starting lightweight browser rendering for %s\n", targetURL)

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[TIER 1 - JSDOM] Browser panic recovered for %s: %v\n", targetURL, r)
		}
	}()

	var browser *rod.Browser
	var l *launcher.Launcher

	l = launcher.New().
		Bin("/usr/bin/chromium").
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

	defer func() {
		if browser != nil {
			browser.Close()
		}
		if l != nil {
			l.Cleanup()
		}
	}()

	page, err := browser.Timeout(10 * time.Second).Page(proto.TargetCreateTarget{URL: targetURL})
	if err != nil {
		return "", fmt.Errorf("failed to create page: %v", err)
	}
	defer page.MustClose()

	err = page.Navigate(targetURL)
	if err != nil {
		return "", fmt.Errorf("failed to navigate: %v", err)
	}

	err = page.WaitLoad()
	if err != nil {
		fmt.Printf("[TIER 1 - JSDOM] Warning: WaitLoad failed for %s: %v - continuing anyway\n", targetURL, err)
	}

	fmt.Printf("[TIER 1 - JSDOM] Performing scrolling to trigger lazy-loaded content for %s\n", targetURL)

	maxScrollSteps := 5
	for i := 0; i < maxScrollSteps; i++ {
		err := func() error {
			if err := page.KeyActions().Press(input.PageDown).Do(); err == nil {
				return nil
			}

			if err := page.Mouse.Scroll(0, 800, 3); err == nil {
				return nil
			}

			_, err := page.Eval("window.scrollBy(0, 800)")
			return err
		}()

		if err != nil {
			fmt.Printf("[TIER 1 - JSDOM] Warning: Scroll step %d failed for %s: %v\n", i+1, targetURL, err)
		} else {
			fmt.Printf("[TIER 1 - JSDOM] Scroll step %d/%d completed for %s\n", i+1, maxScrollSteps, targetURL)
		}

		time.Sleep(800 * time.Millisecond)
	}

	fmt.Printf("[TIER 1 - JSDOM] Performing final scroll to bottom for %s\n", targetURL)
	page.Mouse.Scroll(0, 5000, 10)
	fmt.Printf("[TIER 1 - JSDOM] Final scroll to bottom completed for %s\n", targetURL)

	time.Sleep(1 * time.Second)
	fmt.Printf("[TIER 1 - JSDOM] Completed scrolling sequence for %s\n", targetURL)

	time.Sleep(800 * time.Millisecond)

	page.MustEval(`window.scrollTo(0, 0)`)
	time.Sleep(200 * time.Millisecond)

	html, err := page.HTML()
	if err != nil {
		return "", fmt.Errorf("failed to get HTML content: %v", err)
	}

	fmt.Printf("[TIER 1 - JSDOM] Successfully extracted content after scrolling for %s (size: %d bytes)\n", targetURL, len(html))

	return html, nil
}

func tryAggressiveHTTP(targetURL string) models.ContentResponse {
	fmt.Printf("[AGGRESSIVE HTTP] Starting aggressive HTTP scraping for %s\n", targetURL)

	response := models.ContentResponse{
		URL: targetURL,
	}

	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:109.0) Gecko/20100101 Firefox/121.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
	}

	for i, ua := range userAgents {
		fmt.Printf("[AGGRESSIVE HTTP] Attempt %d/%d for %s using UA: %s\n", i+1, len(userAgents), targetURL, ua[:50]+"...")

		if i > 0 {
			fmt.Printf("[AGGRESSIVE HTTP] Waiting %d seconds before attempt %d\n", i, i+1)
			time.Sleep(time.Duration(i) * time.Second)
		}

		client := &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				req.Header.Set("User-Agent", ua)
				return nil
			},
		}

		httpReq, err := http.NewRequest("GET", targetURL, nil)
		if err != nil {
			continue
		}

		httpReq.Header.Set("User-Agent", ua)
		httpReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		httpReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
		httpReq.Header.Set("Accept-Encoding", "identity")
		httpReq.Header.Set("DNT", "1")
		httpReq.Header.Set("Connection", "keep-alive")
		httpReq.Header.Set("Upgrade-Insecure-Requests", "1")

		if strings.Contains(ua, "Chrome") {
			httpReq.Header.Set("Sec-Fetch-Dest", "document")
			httpReq.Header.Set("Sec-Fetch-Mode", "navigate")
			httpReq.Header.Set("Sec-Fetch-Site", "none")
			httpReq.Header.Set("Sec-Fetch-User", "?1")
		}

		httpReq.Header.Set("Cache-Control", "max-age=0")

		if i > 0 {
			httpReq.Header.Set("Referer", "https://www.google.com/")
		}

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
				"Content-Type":   contentType,
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
