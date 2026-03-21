.PHONY: build run test test-integration lint clean docker-up docker-down

build:
	go build -o bin/indexer ./cmd/indexer

run: build
	./bin/indexer

test:
	go test ./...

test-integration:
	go test -tags integration ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/

docker-up:
	docker compose up -d

docker-down:
	docker compose down

migrate-up:
	migrate -path internal/store/migrations -database "$$DATABASE_URL" up

migrate-down:
	migrate -path internal/store/migrations -database "$$DATABASE_URL" down
