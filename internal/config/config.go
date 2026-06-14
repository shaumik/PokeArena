// Package config loads service configuration from the environment.
// Every binary calls Load() once at startup; there is no config file.
package config

import (
	"os"
	"strconv"
	"time"
)

// Config is the union of every setting any service needs. Individual
// binaries simply ignore the fields they do not use.
type Config struct {
	GatewayAddr  string        // listen address for the gateway HTTP server
	DatabaseURL  string        // PostgreSQL connection string
	RedisURL     string        // Redis connection string
	RabbitURL    string        // RabbitMQ (AMQP) connection string
	AITimeBudget time.Duration // per-decision budget for the agent harness
	DataVersion  string        // namespaces the dataset and all caches
}

// Load reads configuration from the environment, applying defaults that
// work for a developer running outside Docker (services on localhost).
func Load() Config {
	return Config{
		GatewayAddr: gatewayAddr(),
		DatabaseURL: env("DATABASE_URL", "postgres://pokearena:pokearena@localhost:5432/pokearena?sslmode=disable"),
		RedisURL:    env("REDIS_URL", "redis://localhost:6379/0"),
		RabbitURL:   env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		// 1500ms is paired with ExpectimaxAgent.maxDepth=3 — depth-3 search needs
		// the room, depth 2 finishes in ~13ms so the old 400ms ceiling was
		// purely cosmetic. 1.5s in a live battle reads as "AI is thinking" to
		// the user without feeling broken. Override per-deploy if needed.
		AITimeBudget: time.Duration(envInt("AI_TIME_BUDGET_MS", 1500)) * time.Millisecond,
		DataVersion:  env("DATA_VERSION", "gen1-v1"),
	}
}

// gatewayAddr honors $PORT when present (PaaS platforms like Railway inject
// it), falling back to $GATEWAY_ADDR and then :8080.
func gatewayAddr() string {
	if p := os.Getenv("PORT"); p != "" {
		return ":" + p
	}
	return env("GATEWAY_ADDR", ":8080")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
