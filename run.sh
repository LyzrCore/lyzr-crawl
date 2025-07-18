go build -o crawler .
go install github.com/swaggo/swag/cmd/swag@latest
$(go env GOPATH)/bin/swag init -g api.go
go mod tidy
docker-compose build crawler-api && docker-compose restart crawler-api