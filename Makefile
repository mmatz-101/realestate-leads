.PHONY: build build-all clean run test install help

# Default target
.DEFAULT_GOAL := help

# Build for current platform
build:
	@echo "Building for current platform..."
	@go build -o build/realestate-leads cmd/main.go
	@echo "✓ Build complete: build/realestate-leads"

# Build for all platforms
build-all:
	@echo "Building for all platforms..."
	@GOOS=linux GOARCH=amd64 go build -o dist/realestate-leads-linux-amd64 cmd/main.go
	@GOOS=linux GOARCH=arm64 go build -o dist/realestate-leads-linux-arm64 cmd/main.go
	@GOOS=darwin GOARCH=amd64 go build -o dist/realestate-leads-darwin-amd64 cmd/main.go
	@GOOS=darwin GOARCH=arm64 go build -o dist/realestate-leads-darwin-arm64 cmd/main.go
	@GOOS=windows GOARCH=amd64 go build -o dist/realestate-leads-windows-amd64.exe cmd/main.go
	@echo "✓ All builds complete in dist/"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf build/* dist/*
	@rm -f franchise_tax_cache.db
	@rm -f auth-session.json
	@rm -rf browser-data/
	@echo "✓ Clean complete"

# Clean data files
clean-data:
	@echo "Cleaning data files..."
	@rm -f data/uploads/*.csv
	@rm -f data/output/*.csv
	@echo "✓ Data cleaned"

# Run the application
run: build
	@echo "Starting application..."
	@./build/realestate-leads

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Install dependencies
install:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy
	@echo "✓ Dependencies installed"

# Show help
help:
	@echo "Available targets:"
	@echo "  build       - Build for current platform (output: build/)"
	@echo "  build-all   - Build for all platforms (output: dist/)"
	@echo "  clean       - Remove build artifacts and cache"
	@echo "  clean-data  - Remove uploaded and output CSV files"
	@echo "  run         - Build and run the application"
	@echo "  test        - Run all tests"
	@echo "  install     - Install/update dependencies"
	@echo "  help        - Show this help message"
