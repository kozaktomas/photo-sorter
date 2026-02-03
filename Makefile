.PHONY: build build-web build-go clean dev serve test

# Build everything (frontend + Go binary)
build: build-web build-go

# Build the frontend
build-web:
	@echo "Building frontend..."
	cd web && npm install && npm run build
	@echo "Frontend built to internal/web/static/dist/"

# Build the Go binary
build-go:
	@echo "Building Go binary..."
	go build -o photo-sorter .
	@echo "Built: photo-sorter"

# Build Go binary without frontend (for development)
build-go-dev:
	@echo "Building Go binary (dev mode)..."
	go build -o photo-sorter .

# Clean build artifacts
clean:
	rm -f photo-sorter
	rm -rf internal/web/static/dist
	mkdir -p internal/web/static/dist
	touch internal/web/static/dist/.gitkeep
	rm -rf web/node_modules

# Run frontend in development mode (with hot reload)
dev-web:
	cd web && npm run dev

# Run Go server in development mode
dev-go:
	go run . serve

# Run both frontend and backend in development (in separate terminals)
dev:
	@echo "Run 'make dev-web' in one terminal and 'make dev-go' in another"

# Start the production server
serve: build
	./photo-sorter serve

# Export database to embeddings.gob
db-export:
	go run . db export

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Install frontend dependencies
web-install:
	cd web && npm install

# Lint frontend
web-lint:
	cd web && npm run lint

# Type check frontend
web-typecheck:
	cd web && npx tsc --noEmit
