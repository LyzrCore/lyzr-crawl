package handlers

import (
	"context"
	"crawler/config"
	"crawler/models"
	"crawler/services"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// HandleJobStatus handles the GET /jobs/{id} endpoint
// @Summary Get crawl job status
// @Description Retrieves the current status and progress of a crawl job
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} models.JobStatus
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security ApiKeyAuth
// @Router /jobs/{id} [get]
func HandleJobStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]
	
	log.Printf("[JOBS API] Status request for job ID: %s", jobID)

	// First check memory for active jobs (fastest)
	config.JobsMutex.RLock()
	job, exists := config.ActiveJobs[jobID]
	config.JobsMutex.RUnlock()

	if exists {
		log.Printf("[JOBS API] Found job %s in memory - Status: %s", jobID, job.Status)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
		return
	}

	log.Printf("[JOBS API] Job %s not in memory, checking MongoDB", jobID)
	
	// If not in memory, check MongoDB
	job, err := services.GetJobFromMongoDB(jobID)
	if err != nil {
		log.Printf("[JOBS API] Job %s not found in MongoDB: %v", jobID, err)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	log.Printf("[JOBS API] Found job %s in MongoDB - Status: %s", jobID, job.Status)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// HandleGetJobs handles the GET /jobs endpoint to list all jobs
// @Summary List recent jobs
// @Description Retrieves a list of recent jobs from the database
// @Tags jobs
// @Accept json
// @Produce json
// @Param limit query int false "Maximum number of results to return" default(10)
// @Param status query string false "Filter by job status (running, completed, failed)"
// @Success 200 {array} models.JobStatus
// @Failure 401 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security ApiKeyAuth
// @Router /jobs [get]
func HandleGetJobs(w http.ResponseWriter, r *http.Request) {
	log.Printf("[JOBS API] List jobs request received")
	
	if config.JobsCollection == nil {
		log.Printf("[JOBS API] ERROR: Jobs collection not available")
		http.Error(w, "Jobs collection not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	statusFilter := r.URL.Query().Get("status")
	
	limit := int64(10) // default
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			limit = l
		}
	}
	
	log.Printf("[JOBS API] Query parameters - Limit: %d, Status filter: %s", limit, statusFilter)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build filter
	filter := bson.M{}
	if statusFilter != "" {
		filter["status"] = statusFilter
	}

	// Find jobs sorted by created_at descending
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := config.JobsCollection.Find(ctx, filter, opts)
	if err != nil {
		log.Printf("[JOBS API] ERROR: Database find failed: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var jobs []models.JobStatus
	if err := cursor.All(ctx, &jobs); err != nil {
		log.Printf("[JOBS API] ERROR: Failed to decode jobs: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	log.Printf("[JOBS API] Returning %d jobs (filter: %s, limit: %d)", len(jobs), statusFilter, limit)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}