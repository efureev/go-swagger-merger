#!/usr/bin/make
SHELL = /bin/sh

BIN        = bin/swagger-merger
PKG        = ./cmd/swagger-merger
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE      ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
LINT_VER   = v2.7.2

.PHONY: help build install test race cover lint fmt tidy fuzz golden image clean
.DEFAULT_GOAL := help

help: ## Show this help
	@printf "\033[33m%s:\033[0m\n" 'Available commands'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[32m%-9s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o "$(BIN)" $(PKG)

install: ## Install the binary into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

test: ## Run the tests
	go test ./...

race: ## Run the tests under the race detector
	go test -race -count=1 ./...

cover: ## Run the tests and open the coverage report
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

fuzz: ## Fuzz the merger for 60s
	go test ./merge/ -run FuzzMerge -fuzz FuzzMerge -fuzztime 60s

golden: ## Regenerate the golden fixtures
	go test ./merge/ -run TestGolden -update

lint: ## Run the linters
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VER) run

fmt: ## Format the source
	gofmt -s -w .
	go run golang.org/x/tools/cmd/goimports@latest -w .

tidy: ## Tidy the module
	go mod tidy

image: ## Build the Docker image
	docker build -t swagger-merger:$(VERSION) .

clean: ## Remove build artefacts
	rm -rf bin dist coverage.out
