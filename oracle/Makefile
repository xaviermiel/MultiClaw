# Go oracle. The TypeScript oracle in src/ remains the deployed implementation
# until the Go port has completed its shadow run.

GO  ?= go
BIN ?= bin/oracle

.PHONY: build test vet fmt fmt-check check ts-config

build:
	$(GO) build -o $(BIN) ./cmd/oracle

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	@out="$$(gofmt -l cmd internal)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Everything a commit must pass: Go checks plus the TS suite that still ships.
check: fmt-check vet test
	npx vitest run

# Regenerate the TS config dump that the Go preset test compares against.
ts-config:
	npx tsx scripts/dump-ts-config.ts > internal/config/testdata/ts-config.json
