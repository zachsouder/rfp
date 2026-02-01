.PHONY: dev run-discovery run-client migrate test build clean

# Load .env if it exists
ifneq (,$(wildcard .env))
    include .env
    export
endif

# Development
dev:
	docker-compose up -d
	@echo "Postgres running on localhost:5432"

stop:
	docker-compose down

# Run services (use air or similar for hot reload in dev)
run-discovery:
	go run ./discovery/cmd/discovery

run-client:
	go run ./client/cmd/client

# Database
migrate:
	@for f in migrations/*.sql; do \
		echo "Running $$f"; \
		psql "$(DATABASE_URL)" -f "$$f"; \
	done

migrate-down:
	@echo "Manual rollback required - check migrations/"

# Testing
test:
	go test ./...

test-verbose:
	go test -v ./...

# Build
build:
	go build -o bin/discovery ./discovery/cmd/discovery
	go build -o bin/client ./client/cmd/client
	go build -o bin/rfp-cli ./cli/cmd/rfp-cli

# Clean
clean:
	rm -rf bin/
	docker-compose down -v
