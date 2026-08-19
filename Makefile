.PHONY: build mcp test test-royale test-integration vet fmt lint lint-fix lint-install tidy run down logs sync sync-diff sync-upstream validate-data check-stat-preview hooks

# Pin the linter version so local runs match CI exactly.
GOLANGCI_LINT_VERSION := v2.12.2
GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

# golangci-lint refuses to analyse a module whose Go directive is newer than
# the toolchain the linter itself was built with:
#
#   can't load config: the Go language version (go1.25) used to build
#   golangci-lint is lower than the targeted Go version (1.26)
#
# A bare `go install ...@$(GOLANGCI_LINT_VERSION)` uses the machine's *default*
# toolchain, which on a box that has not caught up to this module's go directive
# produces a correctly-versioned linter that cannot run here. Building it with
# the same toolchain that builds the module — the one `go build ./...` already
# selected — avoids that. GOVERSION reports the active toolchain, so this tracks
# the go directive automatically rather than hardcoding a second version to keep
# in sync.
GO_TOOLCHAIN := $(shell go env GOVERSION)

# What a correct install looks like in `golangci-lint version` output. Both
# halves matter: the pinned version (so a stale binary already sitting in
# GOPATH/bin from another project does not silently defeat the pin) and the
# build toolchain (so the unrunnable-binary case above self-heals instead of
# failing on every invocation).
GOLANGCI_LINT_STAMP := version $(GOLANGCI_LINT_VERSION:v%=%) built with $(GO_TOOLCHAIN)

# Project name for the throwaway integration-test infra stack. A distinct name
# keeps it off the dev `make run` stack (they publish the same host ports).
TEST_COMPOSE = docker compose -f docker-compose.test.yml -p pokearena-test

build:
	go build ./...

# Rebuilds the MCP server binary that Claude Code spawns as a subprocess.
# The frame protocol is in lockstep with this tree; run this after pulling
# changes that touch internal/protocol or internal/mcpserver, then restart
# Claude Code so it picks up the new binary.
mcp:
	go build -o bin/pokearena-mcp ./cmd/pokearena-mcp

# --- data pipeline (see tools/data-sync/README.md) ---
#
# sync-upstream  refresh the committed Showdown snapshot. Rare. Node helper.
# sync           Go ETL: extract → filter → transform → stage → validate → swap.
# sync-diff      same as sync but stops before the swap and prints the diff.
# validate-data  run the live validator over data/ (sanity check, no writes).

sync-upstream:
	cd tools/data-sync/refresh-upstream && npm install --silent && node refresh.js

sync:
	go run ./cmd/data-sync

sync-diff:
	go run ./cmd/data-sync -no-swap
	@echo "--- diff: data/.staging vs data/ ---"
	@for f in pokedex.json moves.json typechart.json; do \
		echo ">>> $$f"; \
		diff -u data/$$f data/.staging/$$f || true; \
	done

validate-data:
	go run ./cmd/data-validate

# The team builder previews derived stats in JS, mirroring engine.calcStat.
# This checks the two agree; engine.TestStatPreviewChecksum is the Go half.
check-stat-preview:
	node tools/check-stat-preview.js

test: test-royale
	go test ./... -count=1

# The royale report generator parses agent-authored markdown — the least
# trustworthy input in the pipeline, and the one whose parsing slips turn into
# factual claims on a published page.
test-royale:
	python3 royale/test_report.py

# Full suite *including* the //go:build integration tests, which dial real
# Postgres/Redis/RabbitMQ. Brings the backends up (--wait blocks until every
# healthcheck passes), runs the tests, then always tears the stack down — and
# propagates the test exit code so CI fails on a red run. Unlike plain `make
# test`, a missing backend is a hard failure here, not a silent skip.
test-integration:
	$(TEST_COMPOSE) up -d --wait
	@status=0; go test ./... -count=1 -tags=integration || status=$$?; \
		$(TEST_COMPOSE) down -v; \
		exit $$status

vet:
	go vet ./...

# golangci-lint — the meta-linter (config in .golangci.yml). `make lint`
# reports; `make lint-fix` applies the auto-fixable findings (formatting,
# misspellings, simplifications). Run `make lint-install` once to get the
# pinned binary, or it's fetched automatically by these targets if missing.
lint: lint-install
	$(GOLANGCI_LINT) run ./...

lint-fix: lint-install
	$(GOLANGCI_LINT) run --fix ./...

# Installs only when the binary is missing, the wrong version, or built with a
# toolchain this module has outgrown — see GOLANGCI_LINT_STAMP above. Checking
# the stamp rather than mere existence is what makes the pin actually binding.
lint-install:
	@$(GOLANGCI_LINT) version 2>/dev/null | grep -qF "$(GOLANGCI_LINT_STAMP)" || { \
		echo "installing golangci-lint $(GOLANGCI_LINT_VERSION) with $(GO_TOOLCHAIN)..."; \
		GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	}

fmt:
	gofmt -w .

# Enable the repo's git hooks (.githooks/pre-commit runs build + lint, the same
# fast gates CI does) by pointing git at .githooks. Opt-in and idempotent — run
# once per clone/worktree. Bypass a single commit with PRECOMMIT_SKIP=1.
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks enabled (.githooks/pre-commit: build + lint)."

tidy:
	go mod tidy

run:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f
