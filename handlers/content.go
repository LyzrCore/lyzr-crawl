package handlers

import (
	"crawler/models"
	"crawler/services"
	"encoding/json"
	"net/http"
)

// HandleGetContent handles the POST /content endpoint
// @Summary Get webpage content in all formats (HTML, text, markdown)
// @Description Fetches webpage content and returns it in HTML, clean text, and markdown formats
// @Tags content
// @Accept json
// @Produce json
// @Param request body models.ContentRequest true "Content request with URL or URLs"
// @Success 200 {object} models.ContentBatchResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security ApiKeyAuth
// @Router /content [post]
func HandleGetContent(w http.ResponseWriter, r *http.Request) {
	var req models.ContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var urls []string
	if len(req.URLs) > 0 {
		urls = req.URLs
	} else if req.URL != "" {
		urls = []string{req.URL}
	} else {
		http.Error(w, "Either 'url' or 'urls' is required", http.StatusBadRequest)
		return
	}

	// Ignore user-provided concurrency, use server-determined limits
	_ = req.Concurrency

	results, err := services.GlobalPool.ProcessContentURLs(urls)
	if err != nil {
		if err.Error() == "system overloaded" {
			http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	success, failed := 0, 0
	for _, result := range results {
		if result.Error == "" {
			success++
		} else {
			failed++
		}
	}

	batchResponse := models.ContentBatchResponse{
		Results: results,
		Total:   len(urls),
		Success: success,
		Failed:  failed,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(batchResponse)
}
