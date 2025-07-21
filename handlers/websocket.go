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

	log.Printf("[WEBSOCKET] New WebSocket connection request for job: %s", jobID)

	// Upgrade HTTP connection to WebSocket
	conn, err := config.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WEBSOCKET] WebSocket upgrade failed for job %s: %v", jobID, err)
		return
	}
	defer conn.Close()

	log.Printf("[WEBSOCKET] WebSocket connection established for job: %s", jobID)

	// Create RabbitMQ queue for this job
	queueName, err := services.CreateJobQueue(jobID)
	if err != nil {
		log.Printf("[WEBSOCKET] Failed to create job queue for %s: %v", jobID, err)
		conn.WriteJSON(models.WebSocketMessage{
			Type:      "error",
			JobID:     jobID,
			Error:     "Failed to create event queue",
			Timestamp: time.Now(),
		})
		return
	}

	log.Printf("[WEBSOCKET] Created RabbitMQ queue %s for job %s", queueName, jobID)

	// Send initial connection confirmation
	initialMessage := models.WebSocketMessage{
		Type:      "connected",
		JobID:     jobID,
		Progress:  "Connected to live updates",
		Timestamp: time.Now(),
	}
	if err := conn.WriteJSON(initialMessage); err != nil {
		log.Printf("[WEBSOCKET] Failed to send initial message for job %s: %v", jobID, err)
		return
	}

	log.Printf("[WEBSOCKET] Sent initial connection message for job %s", jobID)

	// Create channels for event consumption
	eventChan := make(chan models.CrawlEvent, 100)
	stopChan := make(chan bool, 1)

	// Start consuming events from RabbitMQ
	if err := services.ConsumeJobEvents(queueName, eventChan, stopChan); err != nil {
		log.Printf("[WEBSOCKET] Failed to start consuming events for job %s: %v", jobID, err)
		conn.WriteJSON(models.WebSocketMessage{
			Type:      "error",
			JobID:     jobID,
			Error:     "Failed to start event consumption",
			Timestamp: time.Now(),
		})
		return
	}

	log.Printf("[WEBSOCKET] Started consuming events for job %s", jobID)

	// Handle WebSocket connection lifecycle
	go func() {
		// Read messages from client (mainly for ping/pong)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Printf("[WEBSOCKET] WebSocket read error for job %s: %v", jobID, err)
				stopChan <- true
				break
			}
		}
	}()

	eventCount := 0
	// Stream events from RabbitMQ to WebSocket
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				log.Printf("[WEBSOCKET] Event channel closed for job %s", jobID)
				return
			}

			eventCount++
			
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
				log.Printf("[WEBSOCKET] Failed to send message #%d for job %s: %v", eventCount, jobID, err)
				stopChan <- true
				return
			}

			// Log completion events
			if event.Type == "completed" {
				log.Printf("[WEBSOCKET] Job %s completed, sent %d events total", jobID, eventCount)
			}

		case <-stopChan:
			log.Printf("[WEBSOCKET] Connection closed for job %s after %d events", jobID, eventCount)
			return
		}
	}
}