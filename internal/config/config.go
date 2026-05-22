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
	AIDifficulty string        // easy | hard | nightmare
	AITimeBudget time.Duration // per-decision budget for the agent harness
	DataVersion  string        // namespaces the dataset and all caches
	AnthropicKey string        // optional; enables the LLM "nightmare" agent
}

// Load reads configuration from the environment, applying defaults that
// work for a developer running outside Docker (services on localhost).
func Load() Config {
	return Config{
		GatewayAddr:  env("GATEWAY_ADDR", ":8080"),
		DatabaseURL:  env("DATABASE_URL", "postgres://pokearena:pokearena@localhost:5432/pokearena?sslmode=disable"),
		RedisURL:     env("REDIS_URL", "redis://localhost:6379/0"),
		RabbitURL:    env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		AIDifficulty: env("AI_DIFFICULTY", "hard"),
		AITimeBudget: time.Duration(envInt("AI_TIME_BUDGET_MS", 400)) * time.Millisecond,
		DataVersion:  env("DATA_VERSION", "gen1-v1"),
		AnthropicKey: env("ANTHROPIC_API_KEY", ""),
	}
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
