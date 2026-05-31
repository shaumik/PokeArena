.PHONY: build mcp test vet fmt tidy run down logs sync sync-diff sync-upstream validate-data

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

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

run:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f
