# Deploying PokéArena

The whole platform is one Docker image (five binaries) plus three
infrastructure dependencies — PostgreSQL, Redis, RabbitMQ. The supported
deployment path is the same `docker-compose.yml` this repo is developed and
tested against — same artifact, same command, zero translation.

A cloud-PaaS walkthrough (Railway-style: each binary as its own managed
service, managed Postgres + Redis, RabbitMQ from the official image) was
removed from this doc while we revisit hosting choices. The current
docker-compose setup runs equivalently on any VM that has Docker.

---

## Single VM (recommended)

Any box with Docker — a DigitalOcean droplet, a Hetzner VM, AWS Lightsail.

```bash
git clone <this-repo> pokearena && cd pokearena
cp .env.example .env          # optional: set ANTHROPIC_API_KEY for the LLM agent
docker compose up -d --build
```

That starts Postgres, Redis, RabbitMQ, runs `ingest` once, and starts the four
services. Open `http://<vm-ip>:8080`.

To put it behind a domain with TLS, run a reverse proxy (Caddy is one line:
`reverse_proxy localhost:8080`) — WebSocket upgrades pass through automatically.

The same `docker compose up` that a developer runs locally is what runs on the
server: "works on my machine" and "works on the server" are the *same command*.

### Notes for any cloud-PaaS deployment

If/when we re-add a PaaS path, the contract is unchanged: each binary is its
own deployable, all five (`gateway`, `battle-worker`, `ai-service`,
`leaderboard-worker`, `ingest`) build from the same `Dockerfile` with different
start commands. Postgres and Redis can be managed services; RabbitMQ runs from
`rabbitmq:3-management-alpine`. The gateway is the only public endpoint;
workers and the AI service don't need ingress.

For the `ANTHROPIC_API_KEY` policy in any deployment: set it on **both** the
gateway and `ai-service`, or leave it unset on both. The gateway uses it to
decide whether to accept `nightmare` battle requests at the API; the
ai-service uses it to make the actual call. If `AI_DIFFICULTY=nightmare` is
set without the key, the service refuses to start — silent downgrade is
intentionally not an option.

---

## Configuration reference

| Variable | Purpose | Default |
|---|---|---|
| `DATABASE_URL` | PostgreSQL DSN | local compose value |
| `REDIS_URL` | Redis URL | local compose value |
| `RABBITMQ_URL` | AMQP URL | local compose value |
| `PORT` / `GATEWAY_ADDR` | gateway listen port | `:8080` |
| `DATA_VERSION` | dataset + cache namespace | `gen1-v1` |
| `AI_DIFFICULTY` | `easy` \| `hard` \| `nightmare` | `hard` |
| `AI_TIME_BUDGET_MS` | per-decision AI budget | `1500` |
| `ANTHROPIC_API_KEY` | enables the LLM agent | *(unset → disabled)* |
