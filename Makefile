.PHONY: run build test migrate up down

run:
	go run ./cmd/server

build:
	go build -trimpath -o bin/ ./cmd/server ./cmd/migrate

test:
	go test -race ./...

# Requires an explicit DATABASE_URL.
migrate:
	go run ./cmd/migrate

up:
	docker compose up -d --build --wait

down:
	docker compose down
