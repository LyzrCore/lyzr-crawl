package models

import (
	"time"
)

// CrawlEvent represents an event published to RabbitMQ
type CrawlEvent struct {
	Type       string    `json:"type"`           // "progress", "url_discovered", "completed", "error", "sitemap_discovered", "tier_switch"
	JobID      string    `json:"job_id"`
	URL        string    `json:"url,omitempty"`
	Depth      int       `json:"depth,omitempty"`
	Progress   string    `json:"progress,omitempty"`   // Human-readable progress message
	Timestamp  time.Time `json:"timestamp"`
	Total      int       `json:"total,omitempty"`      // Total URLs found so far
	PageCount  int       `json:"page_count,omitempty"` // Total pages crawled
	Error      string    `json:"error,omitempty"`
	Tier       string    `json:"tier,omitempty"`       // "sitemap", "html", "headless"
}

// WebSocketMessage represents a real-time update message sent to WebSocket clients
type WebSocketMessage struct {
	Type      string    `json:"type"`             // "progress", "url_discovered", "completed", "connected", "error", "sitemap_discovered", "tier_switch"
	JobID     string    `json:"job_id"`
	URL       string    `json:"url,omitempty"`
	Depth     int       `json:"depth,omitempty"`
	Progress  string    `json:"progress,omitempty"`   // Human-readable progress message
	Timestamp time.Time `json:"timestamp"`
	Total     int       `json:"total,omitempty"`      // Total URLs found
	PageCount int       `json:"page_count,omitempty"` // Total pages crawled
	Error     string    `json:"error,omitempty"`
	Tier      string    `json:"tier,omitempty"`       // "sitemap", "html", "headless"
}