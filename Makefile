.PHONY: build build-web build-go clean dev serve test lint lint-fix fmt vet check

## build: Build everything (frontend + Go binary)
build: build-web build-go

## build-web: Build the frontend
build-web:
	@echo "Building frontend..."
	cd web && npm install && npm run build
	@echo "Frontend built to internal/web/static/dist/"

## build-go: Build the Go binary
build-go:
	@echo "Building Go binary..."
	go build -o photo-sorter .
	@echo "Built: photo-sorter"

## build-go-dev: Build Go binary without frontend (for development)
build-go-dev:
	@echo "Building Go binary (dev mode)..."
	go build -o photo-sorter .

## clean: Clean build artifacts
clean:
	rm -f photo-sorter
	rm -rf internal/web/static/dist
	mkdir -p internal/web/static/dist
	touch internal/web/static/dist/.gitkeep
	rm -rf web/node_modules

## dev-web: Run frontend in development mode (with hot reload)
dev-web:
	cd web && npm run dev

## dev-go: Run Go server in development mode
dev-go:
	go run . serve

## dev: Run both frontend and backend in development (in separate terminals)
dev:
	@echo "Run 'make dev-web' in one terminal and 'make dev-go' in another"

## serve: Start the production server
serve: build
	./photo-sorter serve

## db-export: Export database to embeddings.gob
db-export:
	go run . db export

## fmt: Format Go code
fmt:
	goimports -w . && go fmt ./...

## vet: Run go vet
vet:
	go vet . ./cmd/... ./internal/...

## test: Run tests with race detector
test:
	CGO_ENABLED=1 go test -race . ./cmd/... ./internal/...

## test-v: Run tests with verbose output and race detector
test-v:
	CGO_ENABLED=1 go test -race -v . ./cmd/... ./internal/...

## web-install: Install frontend dependencies
web-install:
	cd web && npm install

## web-lint: Lint frontend
web-lint:
	cd web && npm run lint

## web-typecheck: Type check frontend
web-typecheck:
	cd web && npx tsc --noEmit

## lint: Lint Go code (use explicit paths to avoid traversing root-owned volumes/ directory)
lint:
	golangci-lint run . ./cmd/... ./internal/...

## lint-fix: Lint and auto-fix Go code
lint-fix:
	golangci-lint run --fix . ./cmd/... ./internal/...

## check: Run full quality gate (fmt + vet + lint + test)
check: fmt vet lint test
