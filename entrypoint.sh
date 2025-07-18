#!/bin/sh
set -e

# Make sure the binary is executable
chmod +x /root/crawler

# Start the crawler API
exec /root/crawler -api -port=8080 -mongo="$MONGO_URI" -rabbitmq="$RABBITMQ_URL"