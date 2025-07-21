package config

import (
	"crawler/models"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/mongo"
)

// Global variables for the API server
var (
	MongoClient    *mongo.Client
	JobsCollection *mongo.Collection
	ActiveJobs     = make(map[string]*models.JobStatus)
	JobsMutex      sync.RWMutex
	
	// WebSocket upgrader
	Upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins in development
		},
	}
)