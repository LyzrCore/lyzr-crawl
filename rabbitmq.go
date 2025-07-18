package main

import (
	"encoding/json"
	"fmt"
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

// createJobQueue creates a temporary queue for a specific job ID
func createJobQueue(jobID string) (string, error) {
	if rabbitChannel == nil {
		return "", fmt.Errorf("RabbitMQ not connected")
	}

	// Create a unique queue name for this job
	queueName := fmt.Sprintf("crawler_ws_%s_%d", jobID, time.Now().UnixNano())
	
	// Declare temporary queue with TTL
	queue, err := rabbitChannel.QueueDeclare(
		queueName, // name
		false,     // durable (temporary)
		true,      // delete when unused
		true,      // exclusive
		false,     // no-wait
		amqp.Table{
			"x-message-ttl": int32(3600000), // 1 hour TTL
		},
	)
	if err != nil {
		return "", err
	}

	// Bind queue to exchange with job-specific routing key patterns
	routingKeys := []string{
		fmt.Sprintf("crawler.%s.url_discovered", jobID),
		fmt.Sprintf("crawler.%s.progress", jobID),
		fmt.Sprintf("crawler.%s.completed", jobID),
		fmt.Sprintf("crawler.%s.error", jobID),
	}

	for _, routingKey := range routingKeys {
		err = rabbitChannel.QueueBind(
			queue.Name,   // queue name
			routingKey,   // routing key
			exchangeName, // exchange
			false,
			nil,
		)
		if err != nil {
			return "", err
		}
	}

	return queue.Name, nil
}

// consumeJobEvents consumes events for a specific job and sends them to a channel
func consumeJobEvents(queueName string, eventChan chan<- CrawlEvent, stopChan <-chan bool) error {
	if rabbitChannel == nil {
		return fmt.Errorf("RabbitMQ not connected")
	}

	// Start consuming messages
	msgs, err := rabbitChannel.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		true,      // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		return err
	}

	// Process messages in background
	go func() {
		defer close(eventChan)
		
		for {
			select {
			case <-stopChan:
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				
				var event CrawlEvent
				err := json.Unmarshal(msg.Body, &event)
				if err != nil {
					log.Printf("Failed to unmarshal event: %v", err)
					msg.Nack(false, false)
					continue
				}

				// Send event to channel (non-blocking)
				select {
				case eventChan <- event:
					msg.Ack(false)
				case <-stopChan:
					msg.Nack(false, true) // Requeue message
					return
				}
			}
		}
	}()

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

	// Determine routing key based on job_id and event type
	routingKey := fmt.Sprintf("crawler.%s.%s", event.JobID, event.Type)

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