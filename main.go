package main

import (
	"context"
	"crawler/services"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gocolly/colly/v2"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

// CrawlResult represents a crawl session stored in MongoDB
type CrawlResult struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TargetURL     string             `bson:"target_url" json:"target_url"`
	CrawledAt     time.Time          `bson:"crawled_at" json:"crawled_at"`
	Duration      string             `bson:"duration" json:"duration"`
	TotalURLs     int                `bson:"total_urls" json:"total_urls"`
	URLsPerSecond string             `bson:"urls_per_second" json:"urls_per_second"`
	Settings      CrawlSettings      `bson:"settings" json:"settings"`
	URLs          []string           `bson:"urls" json:"urls"`
}

// CrawlSettings represents the crawler configuration
type CrawlSettings struct {
	Workers int    `bson:"workers" json:"workers"`
	Delay   string `bson:"delay" json:"delay"`
	Depth   int    `bson:"depth" json:"depth"`
}

func main() {
	// Define command line flags
	var (
		// API mode flags
		apiMode = flag.Bool("api", false, "Run as REST API server instead of CLI")
		port    = flag.String("port", "8080", "API server port")

		// CLI mode flags
		workers     = flag.Int("workers", 10, "Number of concurrent workers")
		delay       = flag.Duration("delay", 200*time.Millisecond, "Delay between requests")
		timeout     = flag.Duration("timeout", 30*time.Second, "Request timeout")
		depth       = flag.Int("depth", 1, "Crawling depth")
		mongoURI    = flag.String("mongo", "mongodb://localhost:27017", "MongoDB connection URI")
		mongoDB     = flag.String("db", "crawler", "MongoDB database name")
		mongoCol    = flag.String("collection", "crawls", "MongoDB collection name")
		saveOnly    = flag.Bool("save-only", false, "Only save to MongoDB, don't print JSON")
		rabbitMQURL = flag.String("rabbitmq", "amqp://localhost:5672", "RabbitMQ connection URL")
	)
	flag.Parse()

	// Initialize global worker pool before starting API or CLI mode
	services.InitializeGlobalPool()
	defer services.GlobalPool.Shutdown()

	// Check if running in API mode
	if *apiMode {
		StartAPIServer(*port, *mongoURI, *mongoDB, *rabbitMQURL)
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: go run main.go [flags] <URL>")
		fmt.Println("Modes:")
		fmt.Println("  -api               Run as REST API server")
		fmt.Println("  -port string       API server port (default 8080)")
		fmt.Println("CLI Flags:")
		fmt.Println("  -workers int       Number of concurrent workers (default 10)")
		fmt.Println("  -delay duration    Delay between requests (default 200ms)")
		fmt.Println("  -timeout duration  Request timeout (default 30s)")
		fmt.Println("  -depth int         Crawling depth (default 1)")
		fmt.Println("  -mongo string      MongoDB URI (default mongodb://localhost:27017)")
		fmt.Println("  -db string         MongoDB database (default crawler)")
		fmt.Println("  -collection string MongoDB collection (default crawls)")
		fmt.Println("  -rabbitmq string   RabbitMQ URL (default amqp://localhost:5672)")
		fmt.Println("  -save-only         Only save to MongoDB, don't print JSON")
		fmt.Println("Examples:")
		fmt.Println("  CLI: go run main.go -workers=20 -depth=2 https://example.com")
		fmt.Println("  API: go run main.go -api -port=8080")
		os.Exit(1)
	}

	targetURL := args[0]

	// Connect to MongoDB
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(*mongoURI))
	if err != nil {
		log.Printf("Failed to connect to MongoDB: %v\n", err)
		log.Println("Continuing without MongoDB storage...")
		client = nil
	} else {
		// Test the connection
		err = client.Ping(context.Background(), nil)
		if err != nil {
			log.Printf("Failed to ping MongoDB: %v\n", err)
			log.Println("Continuing without MongoDB storage...")
			client = nil
		} else {
			fmt.Printf("Connected to MongoDB: %s/%s.%s\n", *mongoURI, *mongoDB, *mongoCol)
		}
	}

	var collection *mongo.Collection
	if client != nil {
		collection = client.Database(*mongoDB).Collection(*mongoCol)
		defer client.Disconnect(context.Background())
	}

	// Parse the target URL to get the base domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		fmt.Printf("Error parsing URL: %v\n", err)
		os.Exit(1)
	}

	// Create a new collector with optimized settings
	c := colly.NewCollector()
	c.Limit(&colly.LimitRule{
		Parallelism: *workers,
		Delay:       *delay,
	})
	c.SetRequestTimeout(*timeout)

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
		link := cleanURL(e.Attr("href"))

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
			}
			mu.Unlock()

			// Visit this URL if we haven't reached max depth and it's new
			if !alreadyFound && e.Request.Depth < *depth {
				e.Request.Visit(cleanURL)
			}
		}
	})

	// Set up error handling
	c.OnError(func(r *colly.Response, err error) {
		fmt.Printf("Error occurred: %v\n", err)
	})

	// Set up request callback
	c.OnRequest(func(r *colly.Request) {
		fmt.Printf("Crawling (depth %d): %s\n", r.Depth, r.URL.String())
	})

	fmt.Printf("Starting crawler for: %s\n", targetURL)
	fmt.Println(strings.Repeat("-", 50))

	// Start crawling
	err = c.Visit(targetURL)
	if err != nil {
		fmt.Printf("Error visiting URL: %v\n", err)
		os.Exit(1)
	}

	// Calculate performance stats
	duration := time.Since(startTime)
	urlsPerSecond := float64(len(urlList)) / duration.Seconds()

	// Create structured result
	crawlTime := time.Now()
	result := CrawlResult{
		TargetURL:     targetURL,
		CrawledAt:     crawlTime,
		Duration:      duration.String(),
		TotalURLs:     len(urlList),
		URLsPerSecond: fmt.Sprintf("%.2f", urlsPerSecond),
		Settings: CrawlSettings{
			Workers: *workers,
			Delay:   delay.String(),
			Depth:   *depth,
		},
		URLs: urlList,
	}

	// Save to MongoDB if connected
	if collection != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		insertResult, err := collection.InsertOne(ctx, result)
		if err != nil {
			log.Printf("Failed to save to MongoDB: %v\n", err)
		} else {
			fmt.Printf("Saved to MongoDB with ID: %v\n", insertResult.InsertedID)
		}
	}

	// Output as JSON (unless save-only mode)
	if !*saveOnly {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Printf("Error creating JSON: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonOutput))
	}
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
