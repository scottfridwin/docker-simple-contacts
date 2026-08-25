SHELL := /bin/bash
COVERAGE_PKGS := ./internal/config/... ./internal/httpapi/... ./internal/person/...
COVERAGE_MIN := 70

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-22s\033[0m %s\n", $$1, $$2}'

## ---- Local stack ----
.PHONY: up
up: ## Start the full stack (postgres + app + frontend)
	docker compose up --build

.PHONY: down
down: ## Stop the stack and remove volumes
	docker compose down -v

.PHONY: logs
logs: ## Tail stack logs
	docker compose logs -f

## ---- Backend ----
.PHONY: backend-build
backend-build: ## Build the backend binary
	cd backend && go build ./...

.PHONY: backend-test
backend-test: ## Run backend unit tests
	cd backend && go test ./...

.PHONY: backend-cover
backend-cover: ## Run backend tests with coverage gate
	cd backend && go test -covermode=atomic -coverprofile=coverage.out $(COVERAGE_PKGS)
	cd backend && go tool cover -func=coverage.out | tail -1
	cd backend && total=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
		echo "coverage: $$total% (min $(COVERAGE_MIN)%)"; \
		awk "BEGIN{exit !($$total >= $(COVERAGE_MIN))}"

.PHONY: backend-integration
backend-integration: ## Run backend integration tests (needs TEST_DATABASE_URL)
	cd backend && go test -tags=integration ./...

.PHONY: backend-lint
backend-lint: ## Lint the backend
	cd backend && golangci-lint run

.PHONY: backend-fmt
backend-fmt: ## Format backend code
	cd backend && gofmt -w . && goimports -w .

## ---- Migrations ----
.PHONY: migrate-up
migrate-up: ## Apply migrations (uses DATABASE_URL)
	migrate -path backend/migrations -database "$${DATABASE_URL}" up

.PHONY: migrate-create
migrate-create: ## Create a migration: make migrate-create name=add_x
	migrate create -ext sql -dir backend/migrations -seq $(name)

## ---- Frontend ----
.PHONY: frontend-install
frontend-install: ## Install frontend dependencies
	cd frontend && npm ci

.PHONY: frontend-lint
frontend-lint: ## Lint the frontend
	cd frontend && npm run lint

.PHONY: frontend-test
frontend-test: ## Run frontend unit tests
	cd frontend && npm run test

.PHONY: frontend-build
frontend-build: ## Build the frontend
	cd frontend && npm run build

.PHONY: e2e
e2e: ## Run Playwright end-to-end tests
	cd frontend && npm run test:e2e
