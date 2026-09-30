.PHONY: test build run migrate-up migrate-down swagger

test:
	go test ./...

build:
	go build -o bin/server ./cmd/server
	go build -o bin/migrate ./cmd/migrate

run:
	go run ./cmd/server

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down 1

swagger:
	swag init --generalInfo main.go --dir ./cmd/server,./internal/httpapi,./internal/movement,./internal/operations --parseInternal --output ./docs
