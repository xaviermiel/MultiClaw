GO  ?= go
BIN ?= bin/oracle

.PHONY: build build-linux test vet fmt fmt-check check abi fuzz

build:
	$(GO) build -o $(BIN) ./cmd/oracle

# Static Linux binary for a server (deploy/multiclaw-oracle.service);
# GOARCH_LINUX=arm64 for ARM. `go version -m` shows the commit it was built from.
GOARCH_LINUX ?= amd64

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH_LINUX) $(GO) build -trimpath -ldflags='-s -w' \
		-o bin/oracle-linux-$(GOARCH_LINUX) ./cmd/oracle

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	@out="$$(gofmt -l cmd internal)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Everything a commit must pass.
check: fmt-check vet test

# Regenerate the contract ABIs embedded in internal/contracts from the Foundry
# build (out/ is gitignored, so the extracted ABIs are committed). After a
# contract change: `make abi` and commit the diff.
REPO_ROOT := ..
ABI_OUT   := internal/contracts/abi/generated
PARSERS   := AaveV3Parser KyberSwapParser MerklParser MorphoBlueParser MorphoParser \
             OneInchParser ParaswapParser UniswapV3Parser UniswapV4Parser UniversalRouterParser

abi:
	cd $(REPO_ROOT) && forge build
	mkdir -p $(ABI_OUT)
	jq '.abi' $(REPO_ROOT)/out/DeFiInteractorModule.sol/DeFiInteractorModule.json > $(ABI_OUT)/DeFiInteractorModule.json
	jq '.abi' $(REPO_ROOT)/out/ModuleRegistry.sol/ModuleRegistry.json > $(ABI_OUT)/ModuleRegistry.json
	jq -s '[.[].abi[] | select(.type == "error")] | unique_by([.name, [.inputs[].type]])' \
		$(foreach p,$(PARSERS),$(REPO_ROOT)/out/$(p).sol/$(p).json) > $(ABI_OUT)/ParserErrors.json

# Coverage-guided fuzzing of the spending state machine's invariants.
FUZZTIME ?= 2m

fuzz:
	$(GO) test -run='^$$' -fuzz=FuzzBuildSubAccountState -fuzztime=$(FUZZTIME) ./internal/spending
