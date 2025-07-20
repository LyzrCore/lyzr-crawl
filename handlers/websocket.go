package handlers

import (
	"crawler/config"
	"crawler/models"
	"crawler/services"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// HandleWebSocket handles WebSocket connections for live job updates
// @Summary Connect to live crawl updates
// @Description Establishes a WebSocket connection to receive real-time updates for a specific crawl job
// @Tags websocket
// @Param id path string true "Job ID"
// @Router /ws/{id} [get]
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Upgrade HTTP connection to WebSocket
	conn, err := config.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Create RabbitMQ queue for this job
	queueName, err := services.CreateJobQueue(jobID)
	if err != nil {
		log.Printf("Failed to create job queue: %v", err)
		conn.WriteJSON(models.WebSocketMessage{
			Type:      "error",
			JobID:     jobID,
			Error:     "Failed to create event queue",
			Timestamp: time.Now(),
		})
		return
	}

	// Send initial connection confirmation
	initialMessage := models.WebSocketMessage{
		Type:      "connected",
		JobID:     jobID,
		Progress:  "Connected to live updates",
		Timestamp: time.Now(),
	}
	if err := conn.WriteJSON(initialMessage); err != nil {
		log.Printf("Failed to send initial message: %v", err)
		return
	}

	// Create channels for event consumption
	eventChan := make(chan models.CrawlEvent, 100)
	stopChan := make(chan bool, 1)

	// Start consuming events from RabbitMQ
	if err := services.ConsumeJobEvents(queueName, eventChan, stopChan); err != nil {
		log.Printf("Failed to start consuming events: %v", err)
		conn.WriteJSON(models.WebSocketMessage{
			Type:      "error",
			JobID:     jobID,
			Error:     "Failed to start event consumption",
			Timestamp: time.Now(),
		})
		return
	}

	// Handle WebSocket connection lifecycle
	go func() {
		// Read messages from client (mainly for ping/pong)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				stopChan <- true
				break
			}
		}
	}()

	// Stream events from RabbitMQ to WebSocket
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				// Event channel closed
				return
			}

			// Convert CrawlEvent to WebSocketMessage
			wsMessage := models.WebSocketMessage{
				Type:      event.Type,
				JobID:     event.JobID,
				URL:       event.URL,
				Depth:     event.Depth,
				Progress:  event.Progress,
				Timestamp: event.Timestamp,
				Total:     event.Total,
				PageCount: event.PageCount,
				Error:     event.Error,
			}

			// Send to WebSocket client
			if err := conn.WriteJSON(wsMessage); err != nil {
				log.Printf("Failed to send WebSocket message: %v", err)
				stopChan <- true
				return
			}

		case <-stopChan:
			return
		}
	}
}