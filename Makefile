.PHONY: all test cover lint vuln fmt tidy sync-spec

# Контракт API — копія server/api/openapi.yaml репозиторію сервісу.
SPEC ?= ../../uakey/server/api/openapi.yaml

all: lint test

test:
	go test -race -count=1 ./...

cover:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run ./...

# govulncheck собирається тим самим toolchain, що й модуль, — інакше
# go run візьме мінімальну версію Go, якої вимагає сам x/vuln.
vuln:
	GOTOOLCHAIN=$$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@latest ./...

fmt:
	golangci-lint fmt ./...

tidy:
	go mod tidy

# Оновлює копію контракту; contract_test.go покаже, що змінилося.
sync-spec:
	cp $(SPEC) testdata/openapi.yaml
