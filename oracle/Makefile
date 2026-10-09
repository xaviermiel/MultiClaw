# Go oracle. The TypeScript oracle in src/ remains the deployed implementation
# until the Go port has completed its shadow run.

GO  ?= go
BIN ?= bin/oracle

.PHONY: build test vet fmt fmt-check check ts-config abi golden golden-check fuzz

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
check: fmt-check vet test golden-check
	npx vitest run

# Regenerate the TS config dump that the Go preset test compares against.
ts-config:
	npx tsx scripts/dump-ts-config.ts > internal/config/testdata/ts-config.json

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

# Golden parity vectors: scenarios run through the real TS buildSubAccountState,
# replayed against the Go port by TestGoldenParity.
GOLDEN_DIR := internal/spending/testdata/golden

golden:
	npx tsx scripts/golden/gen.ts $(GOLDEN_DIR)

# Fails when the committed vectors are not what the current TS produces.
golden-check:
	npx tsx scripts/golden/gen.ts --check $(GOLDEN_DIR)

# Coverage-guided fuzzing of the spending state machine's invariants.
FUZZTIME ?= 2m

fuzz:
	$(GO) test -run='^$$' -fuzz=FuzzBuildSubAccountState -fuzztime=$(FUZZTIME) ./internal/spending
