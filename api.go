package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gorilla/mux"
	httpSwagger "github.com/swaggo/http-swagger"
	_ "crawler/docs" // This line is required for Swagger
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// @title Web Crawler API
// @version 1.0
// @description A fast web crawler API that extracts URLs from websites and stores results in MongoDB
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /

// CrawlRequest represents the API request for crawling
type CrawlRequest struct {
	URL     string `json:"url" example:"https://example.com" binding:"required"`
	JobID   string `json:"job_id,omitempty" example:"my-custom-session-123"`
	Depth   int    `json:"depth,omitempty" example:"2"`
	Workers int    `json:"workers,omitempty" example:"10"`
	Delay   string `json:"delay,omitempty" example:"200ms"`
	MaxURLs int    `json:"max_urls,omitempty" example:"1000"`
}

// CrawlResponse represents the immediate API response
type CrawlResponse struct {
	JobID   string `json:"job_id" example:"60f7b3b3b3b3b3b3b3b3b3b3"`
	Status  string `json:"status" example:"accepted"`
	Message string `json:"message" example:"Crawl job started successfully"`
}

// JobStatus represents the status of a crawl job
type JobStatus struct {
	ID        string               `json:"id" bson:"_id" example:"60f7b3b3b3b3b3b3b3b3b3b3"`
	Status    string               `json:"status" bson:"status" example:"completed" enum:"running,completed,failed"`
	Progress  string               `json:"progress,omitempty" bson:"progress,omitempty" example:"Starting crawl..."`
	Result    *CrawlResult         `json:"result,omitempty" bson:"result,omitempty"`
	Error     string               `json:"error,omitempty" bson:"error,omitempty" example:"Error message if failed"`
	CreatedAt time.Time            `json:"created_at" bson:"created_at" example:"2023-07-18T10:30:45Z"`
	UpdatedAt time.Time            `json:"updated_at" bson:"updated_at" example:"2023-07-18T10:32:15Z"`
	Request   *CrawlRequest        `json:"request,omitempty" bson:"request,omitempty"`
}


// Global variables for the API server
var (
	mongoClient     *mongo.Client
	crawlCollection *mongo.Collection
	jobsCollection  *mongo.Collection
	activeJobs      = make(map[string]*JobStatus)
	jobsMutex       sync.RWMutex
)

// initMongoDB initializes the MongoDB connection
func initMongoDB(mongoURI, dbName string) error {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	// Test the connection
	err = client.Ping(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("failed to ping MongoDB: %v", err)
	}

	mongoClient = client
	db := client.Database(dbName)
	crawlCollection = db.Collection("crawls")
	jobsCollection = db.Collection("jobs")

	log.Printf("Connected to MongoDB: %s/%s", mongoURI, dbName)
	return nil
}

// crawlWebsiteWithEvents performs async crawling with events
func crawlWebsiteWithEvents(targetURL string, depth, workers int, delayStr string, maxURLs int, jobID string) (*CrawlResult, error) {
	// Just use the regular crawler function - no need for complex async handling
	return crawlWebsite(targetURL, depth, workers, delayStr)
}

// crawlWebsite performs the actual crawling (moved from main function)
func crawlWebsite(targetURL string, depth, workers int, delayStr string) (*CrawlResult, error) {
	// Parse delay
	delay, err := time.ParseDuration(delayStr)
	if err != nil {
		delay = 200 * time.Millisecond
	}

	// Parse the target URL to get the base domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	// Create a new collector with optimized settings
	c := colly.NewCollector()
	c.Limit(&colly.LimitRule{
		Parallelism: workers,
		Delay:       delay,
	})
	c.SetRequestTimeout(30 * time.Second)

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
		link := e.Attr("href")

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
				
				// Note: RabbitMQ events will be published from the caller
				// This keeps the crawler function lightweight
			}
			mu.Unlock()

			// Visit this URL if we haven't reached max depth and it's new
			if !alreadyFound && e.Request.Depth < depth {
				e.Request.Visit(cleanURL)
			}
		}
	})

	// Set up error handling
	c.OnError(func(r *colly.Response, err error) {
		log.Printf("Error occurred: %v", err)
	})

	// Start crawling
	err = c.Visit(targetURL)
	if err != nil {
		return nil, fmt.Errorf("error visiting URL: %v", err)
	}

	// Calculate performance stats
	duration := time.Since(startTime)
	urlsPerSecond := float64(len(urlList)) / duration.Seconds()

	// Create structured result
	crawlTime := time.Now()
	result := &CrawlResult{
		TargetURL:     targetURL,
		CrawledAt:     crawlTime,
		Duration:      duration.String(),
		TotalURLs:     len(urlList),
		URLsPerSecond: fmt.Sprintf("%.2f", urlsPerSecond),
		Settings: CrawlSettings{
			Workers: workers,
			Delay:   delayStr,
			Depth:   depth,
		},
		URLs: urlList,
	}

	return result, nil
}

// handleCrawl handles the POST /crawl endpoint
// @Summary Start a new web crawl
// @Description Initiates a web crawling job for the specified URL with configurable parameters
// @Tags crawl
// @Accept json
// @Produce json
// @Param request body CrawlRequest true "Crawl parameters"
// @Success 200 {object} CrawlResponse
// @Failure 400 {object} map[string]string
// @Router /crawl [post]
func handleCrawl(w http.ResponseWriter, r *http.Request) {
	var req CrawlRequest
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

	// Use provided job ID or generate one
	var jobID string
	if req.JobID != "" {
		// Validate custom job ID (alphanumeric, hyphens, underscores only)
		if !isValidJobID(req.JobID) {
			http.Error(w, "Invalid job_id format. Use alphanumeric characters, hyphens, and underscores only", http.StatusBadRequest)
			return
		}
		// Check if job ID already exists
		jobsMutex.RLock()
		_, exists := activeJobs[req.JobID]
		jobsMutex.RUnlock()
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
	job := &JobStatus{
		ID:        jobID,
		Status:    "running",
		Progress:  "Starting crawl...",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Request:   &req,
	}

	// Store job in MongoDB
	if err := saveJobToMongoDB(job); err != nil {
		log.Printf("Failed to save job to MongoDB: %v", err)
		// Continue anyway - store in memory as fallback
	}

	// Store job status in memory for fast access
	jobsMutex.Lock()
	activeJobs[jobID] = job
	jobsMutex.Unlock()

	// Start crawling in background
	go func() {
		// Send initial progress event
		publishCrawlEvent(CrawlEvent{
			Type:      "progress",
			JobID:     jobID,
			Progress:  "Starting crawl...",
			Timestamp: time.Now(),
		})
		
		result, err := crawlWebsiteWithEvents(req.URL, req.Depth, req.Workers, req.Delay, req.MaxURLs, jobID)
		
		jobsMutex.Lock()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "completed"
			
			// Save crawl result to MongoDB if connected and get the inserted ID
			if crawlCollection != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				insertResult, err := crawlCollection.InsertOne(ctx, result)
				if err == nil {
					// Update the result with the actual MongoDB ID
					if oid, ok := insertResult.InsertedID.(primitive.ObjectID); ok {
						result.ID = oid
					}
				}
			}
			
			job.Result = result
			
			// Publish completion event to RabbitMQ
			publishCrawlEvent(CrawlEvent{
				Type:      "completed",
				JobID:     jobID,
				Progress:  fmt.Sprintf("Crawl completed! Found %d URLs", len(result.URLs)),
				Timestamp: time.Now(),
				Total:     len(result.URLs),
			})
		}
		job.UpdatedAt = time.Now()
		
		// Update job in MongoDB
		if err := updateJobInMongoDB(job); err != nil {
			log.Printf("Failed to update job in MongoDB: %v", err)
		}
		
		jobsMutex.Unlock()
	}()

	// Return immediate response
	response := CrawlResponse{
		JobID:   jobID,
		Status:  "accepted",
		Message: "Crawl job started successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleJobStatus handles the GET /jobs/{id} endpoint
// @Summary Get crawl job status
// @Description Retrieves the current status and progress of a crawl job
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Job ID"
// @Success 200 {object} JobStatus
// @Failure 404 {object} map[string]string
// @Router /jobs/{id} [get]
func handleJobStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	// First check memory for active jobs (fastest)
	jobsMutex.RLock()
	job, exists := activeJobs[jobID]
	jobsMutex.RUnlock()

	if exists {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
		return
	}

	// If not in memory, check MongoDB
	job, err := getJobFromMongoDB(jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// handleGetJobs handles the GET /jobs endpoint to list all jobs
// @Summary List recent jobs
// @Description Retrieves a list of recent jobs from the database
// @Tags jobs
// @Accept json
// @Produce json
// @Param limit query int false "Maximum number of results to return" default(10)
// @Param status query string false "Filter by job status (running, completed, failed)"
// @Success 200 {array} JobStatus
// @Failure 503 {object} map[string]string
// @Router /jobs [get]
func handleGetJobs(w http.ResponseWriter, r *http.Request) {
	if jobsCollection == nil {
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build filter
	filter := bson.M{}
	if statusFilter != "" {
		filter["status"] = statusFilter
	}

	// Find jobs sorted by created_at descending
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := jobsCollection.Find(ctx, filter, opts)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var jobs []JobStatus
	if err := cursor.All(ctx, &jobs); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// handleGetCrawls handles the GET /crawls endpoint
// @Summary List recent crawl results
// @Description Retrieves a list of recent crawl results from the database
// @Tags crawls
// @Accept json
// @Produce json
// @Param limit query int false "Maximum number of results to return" default(10)
// @Success 200 {array} CrawlResult
// @Failure 503 {object} map[string]string
// @Router /crawls [get]
func handleGetCrawls(w http.ResponseWriter, r *http.Request) {
	if crawlCollection == nil {
		http.Error(w, "MongoDB not connected", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	limitStr := r.URL.Query().Get("limit")
	limit := int64(10) // default
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil {
			limit = l
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find crawls sorted by crawled_at descending
	opts := options.Find().SetLimit(limit).SetSort(bson.D{{Key: "crawled_at", Value: -1}})
	cursor, err := crawlCollection.Find(ctx, bson.D{}, opts)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var crawls []CrawlResult
	if err := cursor.All(ctx, &crawls); err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(crawls)
}

// handleGetCrawlByID handles the GET /crawls/{id} endpoint
// @Summary Get specific crawl result
// @Description Retrieves a specific crawl result by its ID
// @Tags crawls
// @Accept json
// @Produce json
// @Param id path string true "Crawl ID"
// @Success 200 {object} CrawlResult
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /crawls/{id} [get]
func handleGetCrawlByID(w http.ResponseWriter, r *http.Request) {
	if crawlCollection == nil {
		http.Error(w, "MongoDB not connected", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]

	objectID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var crawl CrawlResult
	err = crawlCollection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&crawl)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			http.Error(w, "Crawl not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(crawl)
}


// startAPIServer starts the REST API server
func startAPIServer(port string, mongoURI, dbName, rabbitMQURL string) {
	// Initialize MongoDB
	if err := initMongoDB(mongoURI, dbName); err != nil {
		log.Printf("MongoDB initialization failed: %v", err)
		log.Println("API will run without MongoDB storage")
	} else {
		// Load any active jobs from previous sessions
		loadActiveJobsFromMongoDB()
	}
	
	// Initialize RabbitMQ
	if err := initRabbitMQ(rabbitMQURL); err != nil {
		log.Printf("RabbitMQ initialization failed: %v", err)
		log.Println("API will run without RabbitMQ messaging")
	}

	// Create router
	r := mux.NewRouter()

	// Define routes
	r.HandleFunc("/crawl", handleCrawl).Methods("POST")
	r.HandleFunc("/jobs", handleGetJobs).Methods("GET")
	r.HandleFunc("/jobs/{id}", handleJobStatus).Methods("GET")
	r.HandleFunc("/crawls", handleGetCrawls).Methods("GET")
	r.HandleFunc("/crawls/{id}", handleGetCrawlByID).Methods("GET")

	// Add health check endpoint
	// @Summary Health check
	// @Description Check if the API server is running
	// @Tags health
	// @Produce json
	// @Success 200 {object} map[string]string
	// @Router /health [get]
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}).Methods("GET")

	// Swagger UI endpoint
	r.PathPrefix("/swagger/").Handler(httpSwagger.WrapHandler)

	log.Printf("Starting API server on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  POST /crawl - Start a new crawl")
	log.Printf("  GET  /jobs - List recent jobs")
	log.Printf("  GET  /jobs/{id} - Get job status")
	log.Printf("  GET  /crawls - List recent crawls")
	log.Printf("  GET  /crawls/{id} - Get specific crawl")
	log.Printf("  GET  /health - Health check")
	log.Printf("  GET  /swagger/ - API documentation")

	log.Fatal(http.ListenAndServe(":"+port, r))
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

// saveJobToMongoDB saves a job to the jobs collection
func saveJobToMongoDB(job *JobStatus) error {
	if jobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := jobsCollection.InsertOne(ctx, job)
	return err
}

// updateJobInMongoDB updates a job in the jobs collection
func updateJobInMongoDB(job *JobStatus) error {
	if jobsCollection == nil {
		return fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": job.ID}
	update := bson.M{"$set": job}

	_, err := jobsCollection.UpdateOne(ctx, filter, update)
	return err
}

// getJobFromMongoDB retrieves a job from the jobs collection
func getJobFromMongoDB(jobID string) (*JobStatus, error) {
	if jobsCollection == nil {
		return nil, fmt.Errorf("jobs collection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var job JobStatus
	err := jobsCollection.FindOne(ctx, bson.M{"_id": jobID}).Decode(&job)
	if err != nil {
		return nil, err
	}

	return &job, nil
}

// loadActiveJobsFromMongoDB loads running jobs from MongoDB on startup
func loadActiveJobsFromMongoDB() {
	if jobsCollection == nil {
		log.Println("Jobs collection not initialized, skipping job recovery")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find all running jobs
	cursor, err := jobsCollection.Find(ctx, bson.M{"status": "running"})
	if err != nil {
		log.Printf("Failed to load active jobs from MongoDB: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var recoveredJobs []JobStatus
	if err := cursor.All(ctx, &recoveredJobs); err != nil {
		log.Printf("Failed to decode active jobs: %v", err)
		return
	}

	// Load recovered jobs into memory
	jobsMutex.Lock()
	for _, job := range recoveredJobs {
		// Mark recovered jobs as failed since the process was interrupted
		job.Status = "failed"
		job.Error = "Job interrupted by server restart"
		job.UpdatedAt = time.Now()
		
		activeJobs[job.ID] = &job
		
		// Update status in MongoDB
		go updateJobInMongoDB(&job)
	}
	jobsMutex.Unlock()

	log.Printf("Recovered %d interrupted jobs from MongoDB", len(recoveredJobs))
}