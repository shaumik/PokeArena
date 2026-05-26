.PHONY: build test vet fmt tidy run down ingest logs agent-data sync sync-diff sync-upstream validate-data

build:
	go build ./...

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

# Syncs the curated dataset into cmd/pokearena-agent/data/ so go:embed
# picks up the latest content. Run after any change to data/*.json.
agent-data:
	cp data/pokedex.json data/moves.json data/typechart.json cmd/pokearena-agent/data/

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

ingest:
	docker compose run --rm ingest

logs:
	docker compose logs -f
