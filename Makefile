#!/usr/bin/make

SHELL = /bin/sh
DC_RUN_ARGS = --rm --user "$(shell id -u):$(shell id -g)"

.PHONY : help fmt lint gotest test clean
.DEFAULT_GOAL : help
.SILENT : lint gotest

help: ## Show this help
	@printf "\033[33m%s:\033[0m\n" 'Available commands'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[32m%-11s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Run source code formatter tools
	docker-compose run $(DC_RUN_ARGS) -e "GO111MODULE=off" --no-deps go sh -c 'go get golang.org/x/tools/cmd/goimports && $$GOPATH/bin/goimports -d -w .'
	docker-compose run $(DC_RUN_ARGS) --no-deps go gofmt -s -w -d .
	docker-compose run $(DC_RUN_ARGS) --no-deps go go mod tidy

lint: ## Run go linters
	docker-compose run --rm --no-deps golint golangci-lint run

gotest: ## Run go tests
	docker-compose run $(DC_RUN_ARGS) --no-deps go go test -v -race -timeout 5s ./...

test: lint gotest ## Run go tests and linters

shell: ## Start shell into container with golang
	docker-compose run $(DC_RUN_ARGS) go bash

clean: ## Make clean
	docker-compose down -v -t 1
