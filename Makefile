BUILD_DIR := builds

.PHONY: build build-api build-indexer clean

build: build-api build-indexer

build-api:
	go build -o $(BUILD_DIR)/api ./cmd/api

build-indexer:
	go build -o $(BUILD_DIR)/indexer ./cmd/indexer

clean:
	rm -rf $(BUILD_DIR)
