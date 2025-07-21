package models

// ContentRequest represents the API request for fetching webpage content
type ContentRequest struct {
	URL         string   `json:"url,omitempty" example:"https://example.com"`
	URLs        []string `json:"urls,omitempty" example:"[\"https://example.com\", \"https://another.com\"]"`
	Concurrency int      `json:"concurrency,omitempty" example:"50"`
}

// ContentResponse represents the webpage content response with all formats
type ContentResponse struct {
	URL         string            `json:"url" example:"https://example.com"`
	StatusCode  int              `json:"status_code" example:"200"`
	ContentType string           `json:"content_type" example:"text/html; charset=UTF-8"`
	Markdown    string           `json:"markdown" example:"# Example\n\nClean markdown content"`
	Sizes       ContentSizes     `json:"sizes"`
	Headers     map[string]string `json:"headers,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// ContentSizes represents the sizes of different content formats
type ContentSizes struct {
	Markdown int `json:"markdown" example:"1680"`
}

// ContentBatchResponse represents the response for multiple URLs
type ContentBatchResponse struct {
	Results []ContentResponse `json:"results"`
	Total   int              `json:"total"`
	Success int              `json:"success"`
	Failed  int              `json:"failed"`
}