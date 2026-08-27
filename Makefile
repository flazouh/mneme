.PHONY: test vet build lint

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/mneme ./cmd/mneme

lint:
	golangci-lint run ./...
