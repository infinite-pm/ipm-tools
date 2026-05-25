.PHONY: help build test vet sync-test-cases gen-test-md gen-invalid-sidecars update-test-docs \
        refs-rehash layout-test layout-fitness layout-check layout-check-baseline \
        build-rpc build-all build-dev build-notrace

BIN_DIR ?= bin

help:
	@echo "Available targets:"
	@echo "  build            - go build ./..."
	@echo "  build-notrace    - build + test with the debug trace compiled out (-tags l7notrace)"
	@echo "  test             - go test ./..."
	@echo "  vet              - go vet ./..."
	@echo "  build-rpc        - Build the ipm-rpc LSP binary into $(BIN_DIR)/ (for the VS Code extension)"
	@echo "  build-all        - Build every shipping + dev binary into $(BIN_DIR)/"
	@echo "  build-dev        - Build just the dev binaries (layout-debug, layout-explain)"
	@echo ""
	@echo "Layout regression testing:"
	@echo "  layout-test      - Run layout regression tests (verbose)"
	@echo "  layout-fitness   - Show the fitness score only"
	@echo "  layout-check     - Ratchet the universal-invariant findings vs the baseline"
	@echo ""
	@echo "Fixture maintenance (dev):"
	@echo "  sync-test-cases  - Verify and update generated fixture coverage"
	@echo "  gen-test-md      - Generate per-case .md files from sources.json"
	@echo "  gen-invalid-sidecars - Regenerate ipmt-invalid corpus rejection sidecars"
	@echo "  refs-rehash      - Refresh _refs.json outputs hashes after regenerating fixtures"
	@echo "  update-test-docs - Generate test documentation in temp/test-docs"

build:
	go build ./...

# proves the debug-stripped engine builds and passes its tests
# (docs/dev/layout-gen/layout-debug.md)
build-notrace:
	go build -tags l7notrace ./...
	go test -tags l7notrace ./pkg/layout7/

vet:
	go vet ./...

test:
	go test ./...

# Build the ipm-rpc language server consumed by the VS Code extension.
# Point the extension at this binary via the `ipm.serverPath` setting
# (or add ./bin to PATH).
build-rpc:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags='-s -w' -o $(BIN_DIR)/ipm-rpc ./cmd/ipm-rpc
	@echo "ipm-rpc built: $(BIN_DIR)/ipm-rpc"

# Build every shipping command into $(BIN_DIR)/.
SHIPPING := ipmt-parse layout-gen ipmsvg-gen ipm-validate md-embed md-html ipm-rpc
build-all: build-dev
	@mkdir -p $(BIN_DIR)
	@for c in $(SHIPPING); do \
		echo "building $$c"; \
		go build -trimpath -ldflags='-s -w' -o $(BIN_DIR)/$$c ./cmd/$$c || exit 1; \
	done
	@echo "binaries in $(BIN_DIR)/"

# Dev-only commands that are worth keeping in $(BIN_DIR)/ as binaries.
# They are NOT shipped, but they run the ENGINE — so a stale one silently
# disagrees with source and reports a layout nobody can reproduce
# (bin/layout-debug did exactly that, 2026-07-27, and sent a diagnosis the
# wrong way). build-all rebuilds them alongside the shipping set so the two
# can never drift; `make build-dev` refreshes just these.
DEV := layout-debug layout-explain
build-dev:
	@mkdir -p $(BIN_DIR)
	@for c in $(DEV); do \
		echo "building $$c (dev)"; \
		go build -trimpath -o $(BIN_DIR)/$$c ./cmd-dev/$$c || exit 1; \
	done

# Layout regression testing (base corpus + the layout-alg-ext combinations).
layout-test:
	@go run ./cmd-dev/layout-test-runner -v
	@go run ./cmd-dev/layout-test-runner -v --dir tests/layout-gen-ext

layout-fitness:
	@go run ./cmd-dev/layout-test-runner
	@go run ./cmd-dev/layout-test-runner --dir tests/layout-gen-ext

# Universal-invariant check (no node overlaps, no edge through/grazing a box,
# no crossings/covers, badges & chips clear of boxes, nothing reads-as-paired)
# over the fitness corpora (CHECK_PATHS below), ratcheted
# against the committed baseline: FAILS when any file's finding count grows.
# When your change shrinks the counts, tighten the ratchet with
# `make layout-check-baseline` and commit the updated baseline.
CHECK_PATHS := tests/layout-gen tests/layout-gen-ext

layout-check:
	@go run ./cmd-dev/layout-debug --check --baseline tests/layout-check-baseline.txt $(CHECK_PATHS) 2>/dev/null

layout-check-baseline:
	@go run ./cmd-dev/layout-debug --check --write-baseline tests/layout-check-baseline.txt $(CHECK_PATHS) 2>/dev/null

# Fixture maintenance. These REWRITE tracked files, so CI never runs them; CI
# runs the read-only counterparts instead (`sync-test-cases --check`,
# `md-embed --root . --check`) and fails if what is committed no longer matches
# the sources it was generated from.
sync-test-cases:
	go run ./cmd-dev/sync-test-cases --rm-extra

gen-test-md:
	@go run ./cmd-dev/gen-test-md --sources-config tests/sources.json

# Regenerate the ipmt-invalid corpus rejection sidecars (.parser.error.json /
# .validate.error.json). Run after editing the invalid examples in
# docs/ipmt-spec.md or the tests/ipmt/invalid corpus.
gen-invalid-sidecars:
	@go run ./cmd-dev/gen-invalid-sidecars

# Refresh the "outputs" SHA1 maps in the layout corpora's _refs.json after
# layout-test-runner / ipmsvg-gen / gen-test-md regenerated artifact files.
refs-rehash:
	@go run ./cmd-dev/refs-rehash

update-test-docs:
	@go run ./cmd-dev/gen-test-doc -sources-config tests/sources.json -out-dir temp/test-docs
