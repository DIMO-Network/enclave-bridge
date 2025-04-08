.PHONY: clean run build install dep test lint format docker

SHELL := /bin/bash
PATHINSTBIN = $(abspath ./bin)
export PATH := $(PATHINSTBIN):$(PATH)

BIN_NAME					?= sample-enclave-app
DEFAULT_INSTALL_DIR			:= $(go env GOPATH)/$(PATHINSTBIN)
DEFAULT_ARCH				:= $(shell go env GOARCH)
DEFAULT_GOOS				:= $(shell go env GOOS)
ARCH						?= $(DEFAULT_ARCH)
GOOS						?= $(DEFAULT_GOOS)
INSTALL_DIR					?= $(DEFAULT_INSTALL_DIR)
.DEFAULT_GOAL 				:= run


VERSION   := $(shell git describe --tags || echo "v0.0.0")
VER_CUT   := $(shell echo $(VERSION) | cut -c2-)

# Dependency versions
GOLANGCI_VERSION   = latest
PROTOC_VERSION             = 28.3
PROTOC_GEN_GO_VERSION      = $(shell go list -m -f '{{.Version}}' google.golang.org/protobuf)
PROTOC_GEN_GO_GRPC_VERSION = v1.5.1

help:
	@echo "\nSpecify a subcommand:\n"
	@grep -hE '^[0-9a-zA-Z_-]+:.*?## .*$$' ${MAKEFILE_LIST} | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[0;36m%-20s\033[m %s\n", $$1, $$2}'
	@echo ""

build:
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(ARCH) \
		go build -o $(PATHINSTBIN)/$(BIN_NAME) ./cmd/$(BIN_NAME)

build-enclave-bridge:
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(ARCH) \
		go build -o $(PATHINSTBIN)/enclave-bridge ./cmd/enclave-bridge

run: build
	@./$(PATHINSTBIN)/$(BIN_NAME)
all: clean target

clean:
	@rm -rf $(PATHINSTBIN)
	
install: build
	@install -d $(INSTALL_DIR)
	@rm -f $(INSTALL_DIR)/$(BIN_NAME)
	@cp $(PATHINSTBIN)/$(BIN_NAME) $(INSTALL_DIR)/$(BIN_NAME)

tidy: 
	@go mod tidy

test: ## run tests
	@go test ./...

lint: ## run linter
	@PATH=$$PATH golangci-lint run --timeout 10m

docker: dep ## build docker image
	@docker build -f ./docker/dockerfile . -t dimozone/$(BIN_NAME):$(VER_CUT)
	@docker tag dimozone/$(BIN_NAME):$(VER_CUT) dimozone/$(BIN_NAME):latest

docker-enclave: dep ## build docker image
	@docker build -f ./docker/enclave-runner/Dockerfile . --no-cache --progress=plain --build-arg ENCLAVE_NAME=sample-enclave-app --build-arg ENCLAVE_TAG=latest --build-arg ENCLAVE_IMAGE=dimozone/sample-enclave-runtime --platform linux/amd64 -t dimozone/sample-enclave-app:$(VER_CUT)
	# @docker tag dimozone/sample-enclave-app:$(VER_CUT) dimozone/sample-enclave-app:latest

docker-runtime-amd64: dep ## build docker image for amd64 platform
	@docker build --platform linux/amd64 --no-cache --progress plain -f ./docker/runtime-dockerfile . -t dimozone/sample-enclave-app:latest

tools-golangci-lint: ## install golangci-lint
	@mkdir -p $(PATHINSTBIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | BINARY=golangci-lint bash -s -- ${GOLANGCI_VERSION}


make tools: tools-golangci-lint ## install all tools

generate: generate-go  ## run all file generation for the project

generate-go:## run go generate
	@go generate ./...
