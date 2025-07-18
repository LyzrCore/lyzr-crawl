# Multi-stage build for Go Web Crawler API
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Generate Swagger docs
RUN go install github.com/swaggo/swag/cmd/swag@latest
RUN swag init -g api.go

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o crawler .

# Final stage - minimal image
FROM alpine:latest

# Install curl for health checks and ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates curl tzdata

# Create app directory
WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/crawler .

# Copy swagger docs
COPY --from=builder /app/docs ./docs

# Copy entrypoint script
COPY entrypoint.sh .

# Make binary and entrypoint executable
RUN chmod +x ./crawler ./entrypoint.sh

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Use entrypoint script
ENTRYPOINT ["./entrypoint.sh"]