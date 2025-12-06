LOCAL_BIN:=$(CURDIR)/bin

.PHONY: lint
lint:
	$(info Running lint...)
	GOBIN=$(LOCAL_BIN) go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.57.2 && \
	$(LOCAL_BIN)/golangci-lint run --config=.golangci.pipeline.yaml ./...

.PHONY: test
test:
	go test -count=1 -race ./...


generate-sql:
	go run ./cmd/sqlc

lint-sql:
	oh-my-pg-tool linter check migrations/*.sql

