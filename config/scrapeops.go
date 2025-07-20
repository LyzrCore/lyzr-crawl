package config

import (
	"time"
)

// ScrapeOpsConfig holds configuration for ScrapeOps integration
type ScrapeOpsConfig struct {
	APIKey     string
	UserAgents []string
	Headers    []map[string]string
	LastUpdate time.Time
}

// ScrapeOpsUserAgentResponse represents the full response
type ScrapeOpsUserAgentResponse struct {
	Result []string `json:"result"`
}

// ScrapeOpsHeader represents browser header response from ScrapeOps
type ScrapeOpsHeader map[string]string

// ScrapeOpsHeaderResponse represents the full response
type ScrapeOpsHeaderResponse struct {
	Result []ScrapeOpsHeader `json:"result"`
}

// Global ScrapeOps configuration
var GlobalScrapeOpsConfig = &ScrapeOpsConfig{
	APIKey: "8d44ac41-0e85-42ca-8499-cc73eea0b672", // Your provided API key
}