# Variables
BINARY_NAME=webdav
USER_BINARY=user
VERSION=$(shell git describe --tags --always --dirty)
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(shell git rev-parse --short HEAD)
LDFLAGS=-ldflags "-X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.gitCommit=${GIT_COMMIT}"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build directory
BUILD_DIR=build

.PHONY: all build clean test coverage deps run help user

all: deps build

## build: Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) cmd/server/main.go
	@echo "Building $(USER_BINARY)..."
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(USER_BINARY) cmd/user/main.go
	@echo "Build complete!"

## clean: Clean build files
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	@echo "Clean complete!"

## test: Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

## coverage: Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

## run: Run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME) -c config.yaml

## user-add: Add a new user
user-add: build
	@echo "Adding user..."
	./$(BUILD_DIR)/$(USER_BINARY) -config config.yaml -action add \
		-username $(username) -password $(password) -directory $(directory) \
		-permissions $(permissions) -quota $(quota)

## user-list: List all users
user-list: build
	@echo "Listing users..."
	./$(BUILD_DIR)/$(USER_BINARY) -config config.yaml -action list

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t webdav:$(VERSION) .
	docker tag webdav:$(VERSION) webdav:latest

## docker-run: Run Docker container
docker-run:
	@echo "Running Docker container..."
	docker run -d -p 6065:6065 \
		-v $(PWD)/configs:/etc/webdav \
		-v $(PWD)/data:/data \
		--name webdav \
		webdav:latest

## docker-stop: Stop Docker container
docker-stop:
	@echo "Stopping Docker container..."
	docker stop webdav
	docker rm webdav

## docker-compose-up: Start with docker-compose
docker-compose-up:
	@echo "Starting services with docker-compose..."
	docker-compose up -d

## docker-compose-down: Stop docker-compose services
docker-compose-down:
	@echo "Stopping services..."
	docker-compose down

## docker-compose-logs: View docker-compose logs
docker-compose-logs:
	docker-compose logs -f

## lint: Run linters
lint:
	@echo "Running linters..."
	golangci-lint run ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...
	goimports -w .

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'
