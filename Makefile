.PHONY: lint test

lint:
	golangci-lint run

test:
	go test ./...
