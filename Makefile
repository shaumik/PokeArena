.PHONY: build test vet fmt tidy run down ingest logs agent-data

build:
	go build ./...

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
