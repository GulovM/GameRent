SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command

APP_NAME := gamerent-api
MAIN := ./cmd/api
BIN_DIR := bin
BIN := $(BIN_DIR)/$(APP_NAME).exe

.PHONY: help setup deps up down restart run dev build test test-unit test-integration tidy fmt clean logs ps

help:
	@Write-Output "Available targets:"; Write-Output "  make setup             create .env from .env.example if missing"; Write-Output "  make deps              download Go modules"; Write-Output "  make up                start PostgreSQL and Redis"; Write-Output "  make down              stop PostgreSQL and Redis"; Write-Output "  make restart           restart local services"; Write-Output "  make run               run API locally"; Write-Output "  make dev               start services and run API"; Write-Output "  make build             build API binary into bin/"; Write-Output "  make test              run all tests that do not require external services"; Write-Output "  make test-integration  run DB and E2E tests with local services"; Write-Output "  make fmt               format Go code"; Write-Output "  make tidy              clean go.mod and go.sum"; Write-Output "  make clean             remove local build/runtime artifacts"

setup:
	@if (-not (Test-Path .env)) { Copy-Item .env.example .env }

deps:
	@go mod download

up: setup
	@docker compose up -d postgres redis

down:
	@docker compose down

restart: down up

run: setup
	@go run $(MAIN)

dev: up run

build:
	@New-Item -ItemType Directory -Force $(BIN_DIR) | Out-Null
	@go build -o $(BIN) $(MAIN)

test:
	@go test -count=1 ./...

test-unit: test

test-integration: up
	@$$env:RUN_INTEGRATION_TESTS='1'; go test -count=1 ./...

fmt:
	@go fmt ./...

tidy:
	@go mod tidy

clean:
	@foreach ($$path in @('$(BIN_DIR)', 'out', 'logs')) { if (Test-Path $$path) { Remove-Item -Recurse -Force $$path } }

logs:
	@docker compose logs -f postgres redis

ps:
	@docker compose ps
