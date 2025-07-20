package config

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// RabbitMQ connection and configuration
var (
	RabbitConnection *amqp.Connection
	RabbitChannel    *amqp.Channel
	ExchangeName     = "crawler_events"
)