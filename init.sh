#!/bin/bash

# Web Crawler API Initialization Script
# This script prepares the application and generates documentation

docker compose down  # Stop any running containers first
docker system prune -f  # Clean up any stopped containers

set -e  # Exit on any error

echo "🚀 Initializing Web Crawler API..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Go is installed
check_go() {
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed. Please install Go first."
        exit 1
    fi
    print_success "Go is installed ($(go version))"
}

# Install Go dependencies
install_dependencies() {
    print_status "Installing Go dependencies..."
    go mod tidy
    print_success "Dependencies installed"
}

# Generate Swagger documentation
generate_swagger() {
    print_status "Generating Swagger documentation..."
    
    # Install swag if not present and find the correct path
    SWAG_PATH=""
    if command -v swag &> /dev/null; then
        SWAG_PATH="swag"
    elif [ -f "$(go env GOPATH)/bin/swag" ]; then
        SWAG_PATH="$(go env GOPATH)/bin/swag"
    else
        print_status "Installing swag CLI tool..."
        go install github.com/swaggo/swag/cmd/swag@latest
        SWAG_PATH="$(go env GOPATH)/bin/swag"
    fi
    
    # Generate docs using the correct path
    if [ -x "$SWAG_PATH" ]; then
        $SWAG_PATH init -g server.go
        print_success "Swagger documentation generated"
    else
        print_error "Failed to install or find swag CLI tool"
        exit 1
    fi
}

# Start all services with Docker Compose
start_all_services() {
    print_status "Starting all services with Docker Compose..."
    print_status "This will start: MongoDB, RabbitMQ, and the Crawler API"
    print_status ""
    
    # Build and start all services
    docker compose up --build -d
    
    print_success "All services started!"
    print_status ""
    print_status "Services available at:"
    print_status "  🌐 API Server: http://localhost:8080"
    print_status "  📚 Swagger UI: http://localhost:8080/swagger/index.html"
    print_status "  🐰 RabbitMQ Management: http://localhost:15672 (crawler/crawler123)"
    print_status "  🍃 MongoDB: localhost:27017 (crawler/crawler123)"
    print_status ""
    print_status "API Endpoints:"
    print_status "  POST /crawl - Start a new crawl"
    print_status "  GET  /jobs/{id} - Get job status"
    print_status "  GET  /crawls - List recent crawls"
    print_status "  GET  /crawls/{id} - Get specific crawl"
    print_status "  GET  /health - Health check"
    print_status ""
    print_status "Useful commands:"
    print_status "  docker compose logs -f           # View logs"
    print_status "  docker compose logs -f crawler-api  # API logs only"
    print_status "  docker compose down              # Stop all services"
    print_status "  docker compose restart crawler-api  # Restart API"
}

# Start only dependencies (MongoDB + RabbitMQ)
start_dependencies() {
    print_status "Starting dependencies (MongoDB + RabbitMQ) only..."
    docker compose up -d mongodb rabbitmq
    
    print_success "Dependencies started!"
    print_status ""
    print_status "Now you can run the API locally:"
    print_status "  go run . -api -port=8080"
    print_status "  Or use: ./init.sh --local"
}

# Start API locally (assuming dependencies are running)
start_local_api() {
    print_status "Starting API server locally..."
    print_status "API will be available at: http://localhost:8080"
    print_status "Swagger UI will be available at: http://localhost:8080/swagger/index.html"
    print_status ""
    print_warning "Note: Make sure dependencies are running: docker compose up -d mongodb rabbitmq"
    print_status ""
    print_success "Starting server..."
    
    go run . -api -port=8080 -rabbitmq=amqp://crawler:crawler123@localhost:5672 -mongo=mongodb://crawler:crawler123@localhost:27017/crawler?authSource=admin
}

# Main execution
main() {
    echo "========================================"
    echo "🕷️  Web Crawler API Initialization"
    echo "========================================"
    echo ""
    
    # Checks
    check_go
    echo ""
    
    # Prepare application
    install_dependencies
    generate_swagger
    echo ""
    
    # Start services based on options
    if [[ "$START_ALL" == "true" ]]; then
        start_all_services
    elif [[ "$START_DEPS" == "true" ]]; then
        start_dependencies
    elif [[ "$START_LOCAL" == "true" ]]; then
        start_local_api
    else
        print_success "Setup complete! Ready for deployment."
        print_status ""
        print_status "Choose your deployment option:"
        print_status ""
        print_status "🐳 Full Docker deployment:"
        print_status "  ./init.sh --docker    # All services in Docker"
        print_status ""
        print_status "🏠 Hybrid deployment:"
        print_status "  ./init.sh --deps      # Start MongoDB + RabbitMQ in Docker"
        print_status "  ./init.sh --local     # Run API locally"
        print_status ""
        print_status "⚡ Manual deployment:"
        print_status "  docker compose up -d  # Start all services"
        print_status "  docker compose up -d mongodb rabbitmq  # Dependencies only"
    fi
}

# Help function
show_help() {
    echo "Web Crawler API Initialization Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -h, --help     Show this help message"
    echo "  --docker       Start all services in Docker (full deployment)"
    echo "  --deps         Start only dependencies (MongoDB + RabbitMQ)"
    echo "  --local        Start API locally (requires --deps first)"
    echo "  --dev          Development mode (setup only)"
    echo ""
    echo "Deployment modes:"
    echo ""
    echo "🐳 Full Docker (recommended for production):"
    echo "  ./init.sh --docker"
    echo "  # Starts: MongoDB, RabbitMQ, API (all in Docker)"
    echo ""
    echo "🏠 Hybrid (recommended for development):"
    echo "  ./init.sh --deps    # Start dependencies"
    echo "  ./init.sh --local   # Run API locally (hot reload)"
    echo ""
    echo "⚡ Manual:"
    echo "  ./init.sh --dev     # Setup only"
    echo "  docker compose up -d  # Start services manually"
    echo ""
    echo "Services:"
    echo "  - MongoDB: localhost:27017 (crawler/crawler123)"
    echo "  - RabbitMQ: localhost:5672, UI: localhost:15672 (crawler/crawler123)"
    echo "  - API Server: localhost:8080"
    echo "  - Swagger UI: localhost:8080/swagger/index.html"
    echo ""
    echo "Useful commands:"
    echo "  docker compose logs -f           # View all logs"
    echo "  docker compose down              # Stop all services"
    echo "  docker compose restart crawler-api  # Restart API only"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        --docker)
            START_ALL=true
            shift
            ;;
        --deps)
            START_DEPS=true
            shift
            ;;
        --local)
            START_LOCAL=true
            shift
            ;;
        --dev)
            DEV_MODE=true
            shift
            ;;
        *)
            print_error "Unknown option $1"
            show_help
            exit 1
            ;;
    esac
done

# Run main function
if [[ "$DEV_MODE" == "true" ]]; then
    print_status "Development mode - setup only"
    check_go
    install_dependencies
    generate_swagger
    print_success "Development setup complete"
    echo ""
    print_status "To start services: docker compose up -d"
    print_status "To start API: go run . -api -port=8080"
else
    main
fi