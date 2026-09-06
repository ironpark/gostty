# gostty
#
# The Go package is generated from Zig sources, and the Go build links a native
# archive that Zig produces. Both live under zig/ and are driven from there.

ZIG     ?= zig
GO      ?= go
ZIG_DIR := zig
# The native archives and the C header are installed here, beside the Go
# package: they are what the Go build links, so they belong somewhere a Go
# developer would look rather than under zig-out. One `<goos>_<goarch>`
# subdirectory per platform, matching the `#cgo` constraints zigo writes into
# internal/raw/zigo_link_inputs_gen.go.
LIB_DIR := libs

# The native library dominates run time and Zig's default is Debug, which costs
# roughly 360x on VT parsing (feeding one line: 159us Debug, 5.8us ReleaseSafe,
# 0.44us ReleaseFast). ReleaseSafe keeps the bounds and overflow checks, which
# is what you want for code parsing whatever a program writes to a pty; trade
# them for the last of the speed with:
#
#   make build OPTIMIZE=ReleaseFast
OPTIMIZE ?= ReleaseSafe
# The install prefix is the repository root, so `libs/` lands beside the Go
# package. The generated cgo directives point at it, so it has to match.
ZIG_FLAGS := -Doptimize=$(OPTIMIZE) --prefix $(CURDIR)

.DEFAULT_GOAL := help
.PHONY: help all build generate align-macos-archives test race bench vet example verify check doctor coverage report fmt clean distclean

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

all: build test ## Build the native libraries and run the tests

# The platform matrix lives in zig/build.zig: one `zig build go` generates the
# Go tree once and builds the native archives for every platform, so there is
# no host-only build to offer. Zig caches per platform, so after the first run
# only what changed is rebuilt.
build: generate ## Regenerate bindings and build the native archives (alias of generate)

# Through the generator rather than `go-lib` so the committed link file and
# the archives always come from the same run. Generating is half a second once
# Zig's cache is warm, and it produces the same committed files every time.
generate: ## Regenerate bindings and build native archives for every platform
	cd $(ZIG_DIR) && $(ZIG) build go $(ZIG_FLAGS)
	@$(MAKE) --no-print-directory align-macos-archives

# Zig's archiver packs members on a 4-byte boundary, which Apple's linker
# rejects ("not 8-byte aligned") on some toolchain versions. Repack the macOS
# archives with libtool so any consumer's ld accepts them. libtool is Apple's;
# elsewhere the archives are left as Zig wrote them.
align-macos-archives:
	@command -v libtool >/dev/null 2>&1 && [ "$$(uname -s)" = Darwin ] || \
		{ echo "skipping macOS archive alignment: Apple libtool not available" >&2; exit 0; }; \
	set -eu; tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	for archive in $(LIB_DIR)/darwin_*/lib*.a; do \
		[ -e "$$archive" ] || continue; \
		rm -rf "$$tmp"/members; mkdir -p "$$tmp"/members; \
		(cd "$$tmp"/members && $(ZIG) ar x "$(CURDIR)/$$archive"); \
		chmod u+rw "$$tmp"/members/*.o; \
		(cd "$$tmp"/members && libtool -static -o "$(CURDIR)/$$archive.aligned" *.o); \
		mv "$$archive.aligned" "$$archive"; \
	done

define run-zig-tool
	cd $(ZIG_DIR) && $(ZIG) build $(1) $(ZIG_FLAGS)
endef

test: build ## Run the Go tests
	$(GO) test ./...

race: build ## Run the Go tests under the race detector
	$(GO) test -race -count=2 ./...

bench: build ## Run the benchmarks
	$(GO) test -bench . -benchmem -run '^$$' ./...

vet: build ## Run go vet
	$(GO) vet ./...

example: build ## Run the HyperCat example terminal emulator
	$(GO) run ./examples/hypercat

verify: ## Validate generated bindings, toolchain and native library
	$(call run-zig-tool,go-verify)

check: ## Fail if the committed Go bindings are stale
	$(call run-zig-tool,go-check)

doctor: ## Check the Go binding toolchain prerequisites
	$(call run-zig-tool,go-doctor)

coverage: ## Report which public Zig declarations are bound
	$(call run-zig-tool,go-coverage)

report: ## Explain the effective Go binding contract
	$(call run-zig-tool,go-report)

fmt: ## Format Zig and Go sources
	$(ZIG) fmt $(ZIG_DIR)/build.zig $(ZIG_DIR)/src
	$(GO) fmt ./...

clean: ## Remove build outputs
	rm -rf $(LIB_DIR) $(ZIG_DIR)/zig-out $(ZIG_DIR)/.zig-cache

distclean: clean ## Also remove fetched Zig packages
	rm -rf $(ZIG_DIR)/zig-pkg
