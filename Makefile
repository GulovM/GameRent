SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command

APP_NAME := gamerent-api
MAIN := ./cmd/api
BIN_DIR := bin
BIN := $(BIN_DIR)/$(APP_NAME).exe
WEB_DIR := web

.PHONY: help setup deps web-deps up infra-up down restart run dev web-dev build build-api build-web docker-build test test-unit test-integration tidy fmt clean logs ps

help:
	@Write-Output "Available targets:"; Write-Output "  make setup             create .env from .env.example if missing"; Write-Output "  make deps              download Go modules and install web deps"; Write-Output "  make web-deps          install frontend dependencies"; Write-Output "  make up                start full Docker stack: web, API, PostgreSQL, Redis"; Write-Output "  make infra-up          start PostgreSQL and Redis only"; Write-Output "  make down              stop Docker services"; Write-Output "  make restart           restart full Docker stack"; Write-Output "  make run               run API locally"; Write-Output "  make dev               start DB/Redis and run API locally"; Write-Output "  make web-dev           run frontend locally with Vite"; Write-Output "  make build             build API binary and frontend"; Write-Output "  make build-api         build API binary into bin/"; Write-Output "  make build-web         build frontend bundle"; Write-Output "  make docker-build      build API and web Docker images"; Write-Output "  make test              run all tests that do not require external services"; Write-Output "  make test-integration  run DB and E2E tests with local services"; Write-Output "  make fmt               format Go code"; Write-Output "  make tidy              clean go.mod and go.sum"; Write-Output "  make clean             remove local build/runtime artifacts"

setup:
	@if (-not (Test-Path .env)) { Copy-Item .env.example .env }

deps:
	@go mod download
	@npm --prefix $(WEB_DIR) install

web-deps:
	@npm --prefix $(WEB_DIR) install

up: setup
	@docker compose up -d postgres redis api web

infra-up: setup
	@docker compose up -d postgres redis

down:
	@docker compose down

restart: down up

run: setup
	@go run $(MAIN)

dev: infra-up run

web-dev:
	@npm --prefix $(WEB_DIR) run dev

build: build-api build-web

build-api:
	@New-Item -ItemType Directory -Force $(BIN_DIR) | Out-Null
	@go build -o $(BIN) $(MAIN)

build-web:
	@npm --prefix $(WEB_DIR) run build

docker-build: setup
	@docker compose build api web

test:
	@go test -count=1 ./...

test-unit: test

test-integration: infra-up
	@$$env:RUN_INTEGRATION_TESTS='1'; go test -count=1 ./...

fmt:
	@go fmt ./...

tidy:
	@go mod tidy

clean:
	@foreach ($$path in @('$(BIN_DIR)', 'out', 'logs', '$(WEB_DIR)/dist')) { if (Test-Path $$path) { Remove-Item -Recurse -Force $$path } }

logs:
	@docker compose logs -f postgres redis api web

ps:
	@docker compose ps
