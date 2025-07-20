go build -o crawler-app .
go install github.com/swaggo/swag/cmd/swag@latest
$(go env GOPATH)/bin/swag init -g server.go
go mod tidy
docker compose down
docker compose build --no-cache
docker compose up -d