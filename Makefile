# gostty
#
# The Go package is generated from Zig sources, and the Go build links a native
# archive that Zig produces. Both live under zig/ and are driven from there.

ZIG     ?= zig
GO      ?= go
ZIG_DIR := zig
LIB     := $(ZIG_DIR)/zig-out/lib/libgostty_zigo.a

# The native library dominates run time and Zig's default is Debug, which costs
# roughly 360x on VT parsing (feeding one line: 159us Debug, 5.8us ReleaseSafe,
# 0.44us ReleaseFast). Ship speed by default; override for a safety-checked
# build while chasing a bug in the Zig side:
#
#   make build OPTIMIZE=ReleaseSafe
OPTIMIZE ?= ReleaseFast
ZIG_FLAGS := -Doptimize=$(OPTIMIZE)

ZIG_SOURCES := $(ZIG_DIR)/build.zig $(ZIG_DIR)/build.zig.zon $(wildcard $(ZIG_DIR)/src/*.zig)

.DEFAULT_GOAL := help
.PHONY: help all build generate test race bench vet example verify check doctor report fmt clean distclean

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

all: build test ## Build the native library and run the tests

build: $(LIB) ## Build and install the native binding library

$(LIB): $(ZIG_SOURCES)
	cd $(ZIG_DIR) && $(ZIG) build go-lib $(ZIG_FLAGS)

generate: ## Regenerate the Go tree from zig/src/bindings.zig, then build
	cd $(ZIG_DIR) && $(ZIG) build go $(ZIG_FLAGS)

test: build ## Run the Go tests
	$(GO) test ./...

race: build ## Run the Go tests under the race detector
	$(GO) test -race -count=2 ./...

bench: build ## Run the benchmarks
	$(GO) test -bench . -benchmem -run '^$$' ./... 

vet: build ## Run go vet
	$(GO) vet ./...
	cd example && $(GO) vet ./...

example: build ## Run the example terminal emulator
	cd example && $(GO) run .

verify: ## Validate generated bindings, toolchain and native library
	cd $(ZIG_DIR) && $(ZIG) build go-verify

check: ## Fail if the committed Go bindings are stale
	cd $(ZIG_DIR) && $(ZIG) build go-check

doctor: ## Check the Go binding toolchain prerequisites
	cd $(ZIG_DIR) && $(ZIG) build go-doctor

coverage: ## Report which public Zig declarations are bound
	cd $(ZIG_DIR) && $(ZIG) build go-coverage

report: ## Explain the effective Go binding contract
	cd $(ZIG_DIR) && $(ZIG) build go-report

fmt: ## Format Zig and Go sources
	$(ZIG) fmt $(ZIG_DIR)/build.zig $(ZIG_DIR)/src
	$(GO) fmt ./...

clean: ## Remove build outputs
	rm -rf $(ZIG_DIR)/zig-out $(ZIG_DIR)/.zig-cache

distclean: clean ## Also remove fetched Zig packages
	rm -rf $(ZIG_DIR)/zig-pkg
