# gostty
#
# The Go package is generated from Zig sources, and calls a native shared
# library that Zig produces, through purego. Both live under zig/ and are
# driven from there.

ZIG     ?= zig
GO      ?= go
ZIG_DIR := zig
# The shared libraries and the C header are installed here, beside the Go
# package, which embeds them (library_<goos>_<goarch>.go): they belong
# somewhere a Go developer would look rather than under zig-out. One
# `<goos>_<goarch>` subdirectory per platform.
LIB_DIR := libs

# The native library dominates run time and Zig's default is Debug, which costs
# roughly 360x on VT parsing (feeding one line: 159us Debug, 5.8us ReleaseSafe,
# 0.44us ReleaseFast). ReleaseSafe keeps the bounds and overflow checks, which
# is what you want for code parsing whatever a program writes to a pty; trade
# them for the last of the speed with:
#
#   make build OPTIMIZE=ReleaseFast
OPTIMIZE ?= ReleaseSafe
# The example emulator is its own Go module, so `./...` from the root does not
# reach it; every Go target names both, through the workspace below.
GO_PKGS := ./... ./examples/hypercat/...
# The install prefix is the repository root, so `libs/` lands beside the Go
# package. The embed directives point at it, so it has to match.
ZIG_FLAGS := -Doptimize=$(OPTIMIZE) --prefix $(CURDIR)
# Build against a zigo checkout instead of the release build.zig.zon pins,
# for a zigo change that has not been tagged yet:
#
#   make generate ZIGO_FORK=../zigo
ZIGO_FORK ?=
ifneq ($(ZIGO_FORK),)
ZIG_FLAGS += --fork=$(abspath $(ZIGO_FORK))
endif

.DEFAULT_GOAL := help
.PHONY: help all build generate test test-plugins race bench vet example example-sync verify check doctor coverage report fmt clean distclean

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

all: build test ## Build the native libraries and run the tests

# The platform matrix lives in zig/build.zig: one `zig build go` generates the
# Go tree once and builds the native libraries for every platform, so there is
# no host-only build to offer. Zig caches per platform, so after the first run
# only what changed is rebuilt.
build: generate ## Regenerate bindings and build the native libraries (alias of generate)

# Through the generator rather than `go-lib` so the committed bindings and
# the libraries always come from the same run. Generating is half a second once
# Zig's cache is warm, and it produces the same committed files every time.
generate: ## Regenerate bindings and build native libraries for every platform
	cd $(ZIG_DIR) && $(ZIG) build go $(ZIG_FLAGS)
	@# Windows' import libraries and debug databases are for linking and
	@# debugging against the DLL, which purego does neither of.
	@rm -f $(LIB_DIR)/windows_*/*.lib $(LIB_DIR)/windows_*/*.pdb

define run-zig-tool
	cd $(ZIG_DIR) && $(ZIG) build $(1) $(ZIG_FLAGS)
endef

# The example's go.mod pins a published gostty so `go install
# .../examples/hypercat@latest` works outside this checkout. A workspace that
# uses both modules builds it against the checkout instead: a workspace module
# wins over any version required of it, so no replace is needed. Generated
# rather than committed, so the pin is the only thing to keep current and a
# developer's own go.work is left alone.
go.work:
	$(GO) work init . ./examples/hypercat

test: build test-plugins go.work ## Run the Go and binding plugin tests
	$(GO) test $(GO_PKGS)

test-plugins: ## Test binding plugin policies
	$(call run-zig-tool,test-plugins)

race: build go.work ## Run the Go tests under the race detector
	$(GO) test -race -count=2 $(GO_PKGS)

bench: build go.work ## Run the benchmarks
	$(GO) test -bench . -benchmem -run '^$$' $(GO_PKGS)

vet: build go.work ## Run go vet
	$(GO) vet $(GO_PKGS)

example: build go.work ## Run the HyperCat example terminal emulator
	$(GO) run ./examples/hypercat

# Run this after pushing bindings the example needs, then commit the example's
# go.mod and go.sum: `go install .../hypercat@latest` builds against the pin,
# not the checkout.
example-sync: ## Re-pin the example to the pushed gostty main
	cd examples/hypercat && GOWORK=off $(GO) get github.com/ironpark/gostty@main && GOWORK=off $(GO) mod tidy
	@cd examples/hypercat && echo "example pinned to gostty $$(GOWORK=off $(GO) list -m -f '{{.Version}}' github.com/ironpark/gostty)"

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
