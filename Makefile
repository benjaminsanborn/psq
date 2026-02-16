.PHONY: test test-unit test-integration build clean install help

# Default target
help:
	@echo "psq - PostgreSQL Query Monitor"
	@echo ""
	@echo "Available targets:"
	@echo "  make test            - Run all tests (unit + integration)"
	@echo "  make test-unit       - Run only unit tests (no DB required)"
	@echo "  make test-integration - Run integration tests (requires postgres)"
	@echo "  make build           - Build the psq binary"
	@echo "  make install         - Build and install to /usr/local/bin"
	@echo "  make clean           - Remove build artifacts"
	@echo "  make coverage        - Run tests with coverage report"

# Run all tests
test: test-unit

# Run unit tests (no external dependencies)
test-unit:
	@echo "Running unit tests..."
	go test -v -race -timeout 30s ./...

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run integration tests (requires postgres)
test-integration:
	@echo "Integration tests not yet implemented"
	@echo "TODO: Add tests that connect to a real postgres instance"

# Build the binary
build:
	@echo "Building psq..."
	go build -o psq

# Install to system
install: build
	@echo "Installing psq to /usr/local/bin..."
	cp psq /usr/local/bin/

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f psq
	rm -f coverage.out coverage.html
	go clean
