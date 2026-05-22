# Deploying PokéArena

The whole platform is one Docker image (five binaries) plus three
infrastructure dependencies — PostgreSQL, Redis, RabbitMQ. There are two ways
to stand it up.

- **[Option A — single VM](#option-a--single-vm-recommended)** runs the exact
  `docker-compose.yml` that this repo is developed and tested against. Same
  artifact, same command, zero translation. Recommended for a reliable demo.
- **[Option B — Railway](#option-b--railway)** runs each piece as a managed
  Railway service. Cloud-native, free-tier friendly, a public URL out of the box.

Either way the application image is built from the repo's `Dockerfile`; the
build has no network dependency (the dataset is vendored in `data/`).

---

## Option A — single VM (recommended)

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

---

## Option B — Railway

Railway has no `docker-compose` equivalent — each piece is its own **service**
inside one **project**. The repo ships `railway.json` so every service built
from it uses the `Dockerfile`; each service just overrides its start command.

> Deploying needs a Railway account, so this is a step you run — it cannot be
> done on your behalf. It takes ~10 minutes.

### 1. Project + datastores

```bash
npm i -g @railway/cli && railway login
railway init                      # create the project
```

In the Railway dashboard, **+ New** twice:

- **Database → PostgreSQL** — exposes `DATABASE_URL`.
- **Database → Redis** — exposes `REDIS_URL`.

### 2. RabbitMQ service

**+ New → Docker Image** → `rabbitmq:3-management-alpine`. Set variables:

| Variable | Value |
|---|---|
| `RABBITMQ_DEFAULT_USER` | `pokearena` |
| `RABBITMQ_DEFAULT_PASS` | *(a password you choose)* |

### 3. The four application services

Add **+ New → GitHub Repo** (this repo) **four times**. Railway builds the
`Dockerfile` each time; give each service a **Custom Start Command** and the
shared variables below.

| Service | Start command |
|---|---|
| `gateway` | `/app/bin/gateway` |
| `battle-worker` | `/app/bin/battle-worker` |
| `ai-service` | `/app/bin/ai-service` |
| `leaderboard-worker` | `/app/bin/leaderboard-worker` |

Shared variables (Railway **reference variables** wire the datastores in):

```
DATABASE_URL  = ${{Postgres.DATABASE_URL}}
REDIS_URL     = ${{Redis.REDIS_URL}}
RABBITMQ_URL  = amqp://pokearena:<password>@${{RabbitMQ.RAILWAY_PRIVATE_DOMAIN}}:5672/
DATA_VERSION  = gen1-v1
AI_DIFFICULTY = hard
AI_TIME_BUDGET_MS = 1500
```

Optional: to enable the LLM "nightmare" agent, set `ANTHROPIC_API_KEY` on
**both** `gateway` and `ai-service`. The gateway uses it to decide whether to
accept `nightmare` battle requests at the API; the ai-service uses it to make
the actual call. If either is missing, the relevant service will refuse to
start (`AI_DIFFICULTY=nightmare`) or reject requests at intake — silent
downgrade is intentionally not an option.

On the **gateway** service only: **Settings → Networking → Generate Domain**.
Railway injects `PORT`; the gateway already listens on it.

### 4. Seed the dataset

`ingest` runs once and exits. Either add a fifth service from the repo with
start command `/app/bin/ingest` and **restart policy = Never**, or run it as a
one-off against an existing service's environment:

```bash
railway run --service gateway /app/bin/ingest
```

### 5. Done

Open the gateway's generated domain. Each service scales with the **Replicas**
slider — the architecture is built for it: workers are competing consumers, the
gateway is stateless.

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
