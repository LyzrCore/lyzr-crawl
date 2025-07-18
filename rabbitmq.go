package main

import (
	"encoding/json"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// CrawlEvent represents an event published to RabbitMQ
type CrawlEvent struct {
	Type       string    `json:"type"`           // "progress", "url_discovered", "completed", "error"
	JobID      string    `json:"job_id"`
	URL        string    `json:"url,omitempty"`
	Depth      int       `json:"depth,omitempty"`
	Progress   string    `json:"progress,omitempty"`   // Human-readable progress message
	Timestamp  time.Time `json:"timestamp"`
	Total      int       `json:"total,omitempty"`      // Total URLs found so far
	PageCount  int       `json:"page_count,omitempty"` // Total pages crawled
	Error      string    `json:"error,omitempty"`
}

// RabbitMQ connection and configuration
var (
	rabbitConnection *amqp.Connection
	rabbitChannel    *amqp.Channel
	exchangeName     = "crawler_events"
)

// initRabbitMQ initializes RabbitMQ connection
func initRabbitMQ(rabbitURL string) error {
	var err error
	
	// Connect to RabbitMQ
	rabbitConnection, err = amqp.Dial(rabbitURL)
	if err != nil {
		return err
	}

	// Create channel
	rabbitChannel, err = rabbitConnection.Channel()
	if err != nil {
		return err
	}

	// Declare exchange
	err = rabbitChannel.ExchangeDeclare(
		exchangeName, // name
		"topic",      // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return err
	}


	log.Printf("Connected to RabbitMQ: %s", rabbitURL)
	return nil
}

// publishCrawlEvent publishes an event to RabbitMQ (lightweight)
func publishCrawlEvent(event CrawlEvent) {
	if rabbitChannel == nil {
		// If RabbitMQ is not available, silently continue
		// This keeps the crawler lightweight even without RabbitMQ
		return
	}

	// Convert event to JSON
	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}

	// Determine routing key based on event type
	routingKey := "crawler." + event.Type

	// Publish message (non-blocking, fire-and-forget)
	go func() {
		err := rabbitChannel.Publish(
			exchangeName, // exchange
			routingKey,   // routing key
			false,        // mandatory
			false,        // immediate
			amqp.Publishing{
				ContentType:  "application/json",
				Body:         body,
				Timestamp:    time.Now(),
				DeliveryMode: amqp.Persistent, // Make message persistent
			},
		)
		if err != nil {
			log.Printf("Failed to publish event: %v", err)
		}
	}()
}


// closeRabbitMQ closes RabbitMQ connections
func closeRabbitMQ() {
	if rabbitChannel != nil {
		rabbitChannel.Close()
	}
	if rabbitConnection != nil {
		rabbitConnection.Close()
	}
}