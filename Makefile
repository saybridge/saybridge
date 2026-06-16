# ==============================================================================
# Rocket.Chat Replacement Platform - Backend Makefile
# ==============================================================================

.PHONY: dev dev-gateway test lint wire swagger seed clean help

help:
	@echo "=============================================================================="
	@echo " Rocket.Chat Replacement Platform - Backend Developer Automation CLI"
	@echo "=============================================================================="
	@echo "Available commands:"
	@echo "  make dev           Run REST API Server with hot-reload via Air"
	@echo "  make dev-gateway   Run WebSocket Gateway with hot-reload via Air"
	@echo "  make test          Run all unit tests for Go Backend"
	@echo "  make lint          Run syntax validation and code quality checks (go vet)"
	@echo "  make wire          Automatically generate dependency injection code via Google Wire"
	@echo "  make swagger       Automatically generate Swagger API documentation (swag init)"
	@echo "  make seed          Seed database with default tenant and super admin user"
	@echo "  make clean         Clean temporary build caches, binaries, and generated docs"
	@echo "=============================================================================="

dev:
	@echo "-> Starting REST API Server with Air hot-reload..."
	air -c .air.toml

dev-gateway:
	@echo "-> Starting WebSocket Gateway with Air hot-reload..."
	air -c .air.gateway.toml

test:
	@echo "-> Running Unit Tests..."
	go test -v ./...

lint:
	@echo "-> Running syntax validation (go vet)..."
	go vet ./...

wire:
	@echo "-> Generating Dependency Injection code with Google Wire..."
	wire ./internal/app/...

swagger:
	@echo "-> Generating Swagger documentation..."
	swag init -g cmd/api/main.go -o docs

seed:
	@echo "-> Seeding database with super admin..."
	go run ./cmd/seed/

clean:
	@echo "-> Cleaning compilation caches and documentation..."
	rm -rf tmp bin docs
	@echo "-> Complete!"
