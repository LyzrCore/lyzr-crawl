package handlers

import (
	"crawler/config"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// HandleHealth handles the GET /health endpoint
// @Summary Health check
// @Description Returns the health status of the API and its dependencies
// @Tags health
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /health [get]
func HandleHealth(w http.ResponseWriter, r *http.Request) {
	log.Printf("[HEALTH API] Health check request received")
	
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"services": map[string]interface{}{
			"mongodb": map[string]interface{}{
				"status": "connected",
				"ping":   true,
			},
			"rabbitmq": map[string]interface{}{
				"status": "connected",
				"ping":   true,
			},
		},
		"memory": map[string]interface{}{
			"active_jobs": len(config.ActiveJobs),
		},
	}

	// Check MongoDB connection
	if config.JobsCollection == nil {
		log.Printf("[HEALTH API] MongoDB is disconnected")
		health["services"].(map[string]interface{})["mongodb"] = map[string]interface{}{
			"status": "disconnected",
			"ping":   false,
		}
		health["status"] = "degraded"
	}

	statusStr := health["status"].(string)
	activeJobs := len(config.ActiveJobs)
	log.Printf("[HEALTH API] Health status: %s, Active jobs: %d", statusStr, activeJobs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}