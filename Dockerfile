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
RUN swag init -g server.go

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o crawler .

# Final stage - minimal image
FROM alpine:latest

# Install dependencies for headless browsing and browser automation
RUN apk --no-cache add ca-certificates curl tzdata firefox chromium \
    # Dependencies for browser automation
    nss freetype freetype-dev harfbuzz \
    # Additional dependencies for Chromium
    ttf-freefont

# Set environment variables for browser automation
ENV CHROME_BIN=/usr/bin/chromium
ENV CHROMIUM_FLAGS="--no-sandbox --disable-dev-shm-usage --disable-gpu --disable-web-security"
# Disable Rod auto-download by pointing to system binary
ENV ROD_LAUNCHER_BIN=/usr/bin/chromium

# Create app directory and cache directory
WORKDIR /root/
# Create cache directory for browser automation (optional, since we're using system browser)
RUN mkdir -p /root/.cache/rod

# Copy the binary from builder stage
COPY --from=builder /app/crawler .

# Copy swagger docs
COPY --from=builder /app/docs ./docs

# Copy entrypoint script
COPY entrypoint.sh .

RUN chmod +x ./crawler ./entrypoint.sh && \
    sed -i 's/\r$//' ./entrypoint.sh

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Use entrypoint script
ENTRYPOINT ["./entrypoint.sh"]