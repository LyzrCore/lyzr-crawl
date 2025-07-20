package handlers

import (
	"context"
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
// @Router /crawl [post]
func HandleCrawl(w http.ResponseWriter, r *http.Request) {
	var req models.CrawlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate URL
	if req.URL == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Depth == 0 {
		req.Depth = 1
	}
	if req.Workers == 0 {
		req.Workers = 10
	}
	if req.Delay == "" {
		req.Delay = "200ms"
	}
	if req.MaxURLs == 0 {
		req.MaxURLs = 1000 // Default limit
	}
	// Enforce maximum limit for safety
	if req.MaxURLs > 5000 {
		req.MaxURLs = 5000
	}
	
	// Set defaults for tier configuration
	if req.HeadlessTimeout == 0 {
		req.HeadlessTimeout = 30 // Default 30 seconds
	}

	// Use provided job ID or generate one
	var jobID string
	if req.JobID != "" {
		// Validate custom job ID (alphanumeric, hyphens, underscores only)
		if !isValidJobID(req.JobID) {
			http.Error(w, "Invalid job_id format. Use alphanumeric characters, hyphens, and underscores only", http.StatusBadRequest)
			return
		}
		// Check if job ID already exists
		config.JobsMutex.RLock()
		_, exists := config.ActiveJobs[req.JobID]
		config.JobsMutex.RUnlock()
		if exists {
			http.Error(w, "Job ID already exists. Choose a different job_id or omit it for auto-generation", http.StatusConflict)
			return
		}
		jobID = req.JobID
	} else {
		// Auto-generate job ID
		jobID = primitive.NewObjectID().Hex()
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

	// Store job in MongoDB
	if err := services.SaveJobToMongoDB(job); err != nil {
		log.Printf("Failed to save job to MongoDB: %v", err)
		// Continue anyway - store in memory as fallback
	}

	// Store job status in memory for fast access
	config.JobsMutex.Lock()
	config.ActiveJobs[jobID] = job
	config.JobsMutex.Unlock()

	// Start crawling in background
	go func() {
		// Send initial progress event
		services.PublishCrawlEvent(models.CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  "Starting crawl...",
			Timestamp: time.Now(),
		})
		
		result, err := services.CrawlWebsiteWithTiers(req.URL, req, jobID)
		
		config.JobsMutex.Lock()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "completed"
			
			// Save crawl result to MongoDB if connected and get the inserted ID
			if config.CrawlCollection != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				insertResult, err := config.CrawlCollection.InsertOne(ctx, result)
				if err == nil {
					// Update the result with the actual MongoDB ID
					if oid, ok := insertResult.InsertedID.(primitive.ObjectID); ok {
						result.ID = oid
					}
				}
			}
			
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
		
		// Update job in MongoDB
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