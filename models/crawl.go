package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

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

// CrawlRequest represents the API request for crawling
type CrawlRequest struct {
	URL             string `json:"url" example:"https://example.com" binding:"required"`
	JobID           string `json:"job_id,omitempty" example:"my-custom-session-123"`
	Depth           int    `json:"depth,omitempty" example:"2"`
	Workers         int    `json:"workers,omitempty" example:"10"`
	Delay           string `json:"delay,omitempty" example:"200ms"`
	MaxURLs         int    `json:"max_urls,omitempty" example:"1000"`
	EnableSitemap   bool   `json:"enable_sitemap,omitempty" example:"true"`
	EnableHTML      bool   `json:"enable_html,omitempty" example:"true"`
	EnableHeadless  bool   `json:"enable_headless,omitempty" example:"false"`
	HeadlessTimeout int    `json:"headless_timeout,omitempty" example:"30"`
	WaitForJS       bool   `json:"wait_for_js,omitempty" example:"true"`
}

// CrawlResponse represents the immediate API response
type CrawlResponse struct {
	JobID   string `json:"job_id" example:"60f7b3b3b3b3b3b3b3b3b3b3"`
	Status  string `json:"status" example:"accepted"`
	Message string `json:"message" example:"Crawl job started successfully"`
}

// CrawlTierStats represents statistics for each crawling tier
type CrawlTierStats struct {
	SitemapURLs  int `json:"sitemap_urls"`
	HTMLURLs     int `json:"html_urls"`
	HeadlessURLs int `json:"headless_urls"`
	TotalTiers   int `json:"total_tiers_used"`
}