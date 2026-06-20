# syntax=docker/dockerfile:1

# ---- build stage: compile the long-running service binaries ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Dependencies first, so the layer caches across source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN set -eux; \
    for cmd in gateway battle-worker battle-session ai-service leaderboard-worker; do \
      CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/$cmd" "./cmd/$cmd"; \
    done

# ---- runtime stage: minimal image, one per binary chosen by `command` ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/ /app/bin/
COPY data/ /app/data/
COPY web/ /app/web/

EXPOSE 8080
# Default entrypoint; docker-compose overrides `command` per service.
CMD ["/app/bin/gateway"]
