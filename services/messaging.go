package services

import (
	"crawler/config"
	"crawler/models"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// InitRabbitMQ initializes RabbitMQ connection
func InitRabbitMQ(rabbitURL string) error {
	var err error
	
	// Connect to RabbitMQ
	config.RabbitConnection, err = amqp.Dial(rabbitURL)
	if err != nil {
		return err
	}

	// Create channel
	config.RabbitChannel, err = config.RabbitConnection.Channel()
	if err != nil {
		return err
	}

	// Declare exchange
	err = config.RabbitChannel.ExchangeDeclare(
		config.ExchangeName, // name
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

// CreateJobQueue creates a temporary queue for a specific job ID
func CreateJobQueue(jobID string) (string, error) {
	if config.RabbitChannel == nil {
		return "", fmt.Errorf("RabbitMQ not connected")
	}

	// Check if channel is closed and reconnect if needed
	if config.RabbitChannel.IsClosed() {
		log.Printf("[RABBITMQ] Channel is closed, attempting to reconnect...")
		var err error
		config.RabbitChannel, err = config.RabbitConnection.Channel()
		if err != nil {
			return "", fmt.Errorf("failed to recreate channel: %v", err)
		}
		log.Printf("[RABBITMQ] Successfully recreated channel")
	}

	// Create a unique queue name for this job
	queueName := fmt.Sprintf("crawler_ws_%s_%d", jobID, time.Now().UnixNano())
	
	// Declare temporary queue with TTL
	queue, err := config.RabbitChannel.QueueDeclare(
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
		err = config.RabbitChannel.QueueBind(
			queue.Name,   // queue name
			routingKey,   // routing key
			config.ExchangeName, // exchange
			false,
			nil,
		)
		if err != nil {
			return "", err
		}
	}

	return queue.Name, nil
}

// ConsumeJobEvents consumes events for a specific job and sends them to a channel
func ConsumeJobEvents(queueName string, eventChan chan<- models.CrawlEvent, stopChan <-chan bool) error {
	if config.RabbitChannel == nil {
		return fmt.Errorf("RabbitMQ not connected")
	}

	// Check if channel is closed and reconnect if needed
	if config.RabbitChannel.IsClosed() {
		log.Printf("[RABBITMQ] Channel is closed, attempting to reconnect...")
		var err error
		config.RabbitChannel, err = config.RabbitConnection.Channel()
		if err != nil {
			return fmt.Errorf("failed to recreate channel: %v", err)
		}
		log.Printf("[RABBITMQ] Successfully recreated channel")
	}

	// Start consuming messages
	msgs, err := config.RabbitChannel.Consume(
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
				
				var event models.CrawlEvent
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

// PublishCrawlEvent publishes an event to RabbitMQ (lightweight)
func PublishCrawlEvent(event models.CrawlEvent) {
	log.Printf("[RABBITMQ] Publishing event: JobID=%s, Type=%s", event.JobID, event.Type)
	
	if config.RabbitChannel == nil {
		log.Printf("[RABBITMQ] ERROR: RabbitChannel is nil, cannot publish event")
		return
	}
	
	if config.RabbitChannel.IsClosed() {
		log.Printf("[RABBITMQ] ERROR: RabbitChannel is closed, cannot publish event")
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
		err := config.RabbitChannel.Publish(
			config.ExchangeName, // exchange
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
			log.Printf("[RABBITMQ] ERROR: Failed to publish event: %v", err)
		} else {
			log.Printf("[RABBITMQ] Successfully published event: %s", routingKey)
		}
	}()
}

// CloseRabbitMQ closes RabbitMQ connections
func CloseRabbitMQ() {
	if config.RabbitChannel != nil {
		config.RabbitChannel.Close()
	}
	if config.RabbitConnection != nil {
		config.RabbitConnection.Close()
	}
}