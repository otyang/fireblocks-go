.PHONY: all generate build test cover lint vet fmt check-generated tidy

all: generate fmt vet test

generate: ## Regenerate models, services and API docs from api/openapi.yaml
	go generate ./...

build:
	go build ./...

test:
	go test -race -count=1 ./...
	cd internal/codegen && go test -count=1 ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...
	cd internal/codegen && go vet ./...

fmt:
	gofmt -s -w .

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
	cd internal/codegen && go mod tidy

check-generated: generate ## Fail if generated files are out of date
	@git status --porcelain -- '*.gen.go' docs/api
	@test -z "$$(git status --porcelain -- '*.gen.go' docs/api)"
