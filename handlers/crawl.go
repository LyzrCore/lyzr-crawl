package handlers

import (
	"crawler/config"
	"crawler/models"
	"crawler/services"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// HandleCrawl handles the POST /crawl endpoint
// @Summary Start a new web crawl
// @Description Initiates a web crawling job for the specified URL with configurable parameters
// @Tags crawl
// @Accept json
// @Produce json
// @Param request body models.CrawlRequest true "Crawl parameters"
// @Success 200 {object} models.CrawlResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Security ApiKeyAuth
// @Router /crawl [post]
func HandleCrawl(w http.ResponseWriter, r *http.Request) {
	log.Printf("[CRAWL API] New crawl request received")

	var req models.CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[CRAWL API] ERROR: Invalid JSON in request: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	log.Printf("[CRAWL API] Parsed request - URL: %s, Depth: %d, Workers: %d, MaxURLs: %d",
		req.URL, req.Depth, req.Workers, req.MaxURLs)

	// Validate URL
	if req.URL == "" {
		log.Printf("[CRAWL API] ERROR: Missing URL in request")
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Depth == 0 {
		req.Depth = 1
		log.Printf("[CRAWL API] Using default depth: %d", req.Depth)
	}
	if req.Workers == 0 {
		req.Workers = 10
		log.Printf("[CRAWL API] Using default workers: %d", req.Workers)
	}
	if req.Delay == "" {
		req.Delay = "200ms"
		log.Printf("[CRAWL API] Using default delay: %s", req.Delay)
	}
	if req.MaxURLs == 0 {
		req.MaxURLs = 1000 // Default limit
		log.Printf("[CRAWL API] Using default max URLs: %d", req.MaxURLs)
	}
	// Enforce maximum limit for safety
	if req.MaxURLs > 5000 {
		log.Printf("[CRAWL API] Limiting max URLs from %d to 5000", req.MaxURLs)
		req.MaxURLs = 5000
	}

	// Set defaults for tier configuration
	if req.HeadlessTimeout == 0 {
		req.HeadlessTimeout = 30 // Default 30 seconds
		log.Printf("[CRAWL API] Using default headless timeout: %d seconds", req.HeadlessTimeout)
	}

	log.Printf("[CRAWL API] Final configuration - Depth: %d, Workers: %d, Delay: %s, MaxURLs: %d, Tiers: [Sitemap:%t, HTML:%t, Headless:%t]",
		req.Depth, req.Workers, req.Delay, req.MaxURLs, req.EnableSitemap, req.EnableHTML, req.EnableHeadless)

	// Use provided job ID or generate one
	var jobID string
	if req.JobID != "" {
		// Validate custom job ID (alphanumeric, hyphens, underscores only)
		if !isValidJobID(req.JobID) {
			log.Printf("[CRAWL API] ERROR: Invalid job ID format: %s", req.JobID)
			http.Error(w, "Invalid job_id format. Use alphanumeric characters, hyphens, and underscores only", http.StatusBadRequest)
			return
		}
		// Check if job ID already exists
		config.JobsMutex.RLock()
		_, exists := config.ActiveJobs[req.JobID]
		config.JobsMutex.RUnlock()
		if exists {
			log.Printf("[CRAWL API] ERROR: Job ID already exists: %s", req.JobID)
			http.Error(w, "Job ID already exists. Choose a different job_id or omit it for auto-generation", http.StatusConflict)
			return
		}
		jobID = req.JobID
		log.Printf("[CRAWL API] Using custom job ID: %s", jobID)
	} else {
		// Auto-generate job ID
		jobID = primitive.NewObjectID().Hex()
		log.Printf("[CRAWL API] Generated job ID: %s", jobID)
	}

	// Create job status
	job := &models.JobStatus{
		ID:        jobID,
		Status:    "running",
		Progress:  "Starting crawl...",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Request:   &req,
	}

	log.Printf("[CRAWL API] Created job status for ID: %s", jobID)

	// Store job in MongoDB
	if err := services.SaveJobToMongoDB(job); err != nil {
		log.Printf("[CRAWL API] WARNING: Failed to save job to MongoDB: %v", err)
		// Continue anyway - store in memory as fallback
	} else {
		log.Printf("[CRAWL API] Saved job %s to MongoDB", jobID)
	}

	// Store job status in memory for fast access
	config.JobsMutex.Lock()
	config.ActiveJobs[jobID] = job
	config.JobsMutex.Unlock()
	log.Printf("[CRAWL API] Stored job %s in memory, total active jobs: %d", jobID, len(config.ActiveJobs))

	// Start crawling in background
	go func() {
		log.Printf("[CRAWL API] Starting background crawling for job %s", jobID)

		// Send initial progress event
		services.PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  "Starting crawl...",
			Timestamp: time.Now(),
		})

		startTime := time.Now()
		result, err := services.CrawlWebsiteWithTiers(req.URL, req, jobID)
		duration := time.Since(startTime)

		config.JobsMutex.Lock()
		if err != nil {
			log.Printf("[CRAWL API] Job %s FAILED after %v: %v", jobID, duration, err)
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			log.Printf("[CRAWL API] Job %s COMPLETED after %v - Found %d URLs", jobID, duration, result.TotalURLs)
			job.Status = "completed"

			// Generate ID for the result (no separate crawls collection needed)
			result.ID = primitive.NewObjectID()

			job.Result = result

			// Publish completion event to RabbitMQ
			services.PublishCrawlEvent(models.CrawlEvent{
				Type:      "completed",
				JobID:     jobID,
				Progress:  fmt.Sprintf("Crawl completed! Found %d URLs", len(result.URLs)),
				Timestamp: time.Now(),
				Total:     len(result.URLs),
			})
		}
		job.UpdatedAt = time.Now()

		if err := services.UpdateJobInMongoDB(job); err != nil {
			log.Printf("Failed to update job in MongoDB: %v", err)
		}

		config.JobsMutex.Unlock()
	}()

	// Return immediate response
	response := models.CrawlResponse{
		JobID:   jobID,
		Status:  "accepted",
		Message: "Crawl job started successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// isValidJobID validates custom job ID format
func isValidJobID(jobID string) bool {
	// Allow alphanumeric characters, hyphens, and underscores
	// Length between 3 and 50 characters
	if len(jobID) < 3 || len(jobID) > 50 {
		return false
	}

	for _, char := range jobID {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_') {
			return false
		}
	}
	return true
}
