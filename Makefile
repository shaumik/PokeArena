.PHONY: build test vet fmt tidy run down ingest logs

build:
	go build ./...

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
