package main

import (
	"crawler/handlers"
	"crawler/middleware"
	"crawler/services"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	httpSwagger "github.com/swaggo/http-swagger"
	_ "crawler/docs" // This line is required for Swagger
)

// @title Lyzr Crawl API
// @version 1.0
// @description A high-performance web crawler API that extracts content and URLs from websites
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key
// @description API key authentication. Use the header 'X-API-Key: your-api-key-here'

// StartAPIServer starts the REST API server
func StartAPIServer(port string, mongoURI, dbName, rabbitMQURL string) {
	// Initialize ScrapeOps for stealth headers
	log.Println("🔧 Initializing ScrapeOps stealth headers...")
	services.InitScrapeOpsHeaders()
	
	// Initialize MongoDB
	if err := services.InitMongoDB(mongoURI, dbName); err != nil {
		log.Printf("MongoDB initialization failed: %v", err)
		log.Println("API will run without MongoDB storage")
	} else {
		// Load any active jobs from previous sessions
		services.LoadActiveJobsFromMongoDB()
	}
	
	// Initialize RabbitMQ
	if err := services.InitRabbitMQ(rabbitMQURL); err != nil {
		log.Printf("RabbitMQ initialization failed: %v", err)
		log.Println("API will run without RabbitMQ messaging")
	}

	// Create router
	r := mux.NewRouter()

	// Add logging middleware first
	r.Use(middleware.LoggingMiddleware)

	// Add API key authentication middleware
	r.Use(middleware.APIKeyMiddleware)

	// Add CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set CORS headers for all requests
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "86400")
			
			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			
			next.ServeHTTP(w, r)
		})
	})

	
	// Add global OPTIONS handler for all routes
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}).Methods("OPTIONS")

	// Define routes
	r.HandleFunc("/", handlers.HandleHealth).Methods("GET")
	r.HandleFunc("/crawl", handlers.HandleCrawl).Methods("POST", "OPTIONS")
	r.HandleFunc("/content", handlers.HandleGetContent).Methods("POST", "OPTIONS")
	r.HandleFunc("/jobs", handlers.HandleGetJobs).Methods("GET", "OPTIONS")
	r.HandleFunc("/jobs/{id}", handlers.HandleJobStatus).Methods("GET", "OPTIONS")
	r.HandleFunc("/ws/{id}", handlers.HandleWebSocket).Methods("GET", "OPTIONS")
	r.HandleFunc("/health", handlers.HandleHealth).Methods("GET")

	// Swagger UI endpoint
	r.PathPrefix("/notforhumans/").Handler(httpSwagger.WrapHandler)

	log.Printf("Starting API server on port %s", port)
	log.Printf("Endpoints:")
	log.Printf("  GET  / - Health check")
	log.Printf("  POST /crawl - Start a new crawl")
	log.Printf("  POST /content - Get webpage content")
	log.Printf("  GET  /jobs - List recent jobs")
	log.Printf("  GET  /jobs/{id} - Get job status")
	log.Printf("  GET  /ws/{id} - WebSocket live updates")
	log.Printf("  GET  /health - Health check")
	log.Printf("  GET  /notforhumans/ - API documentation")

	log.Fatal(http.ListenAndServe(":"+port, r))
}