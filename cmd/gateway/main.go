// Command gateway is the PokéArena edge service: REST API, WebSocket live
// battles, SSE spectating, and the static SPA.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/httpapi"
	"pokearena/internal/mq"
	"pokearena/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[gateway] ")

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Boot self-check: refuse to start if the gateway's declared default
	// difficulty cannot be served by this deployment. Catches the operator
	// who sets AI_DIFFICULTY=nightmare on the gateway but forgets the API
	// key. Without this, the gateway would accept "nightmare" battles at the
	// API and silently fall back to the local HeuristicAgent for every turn
	// when the ai-service drops the jobs — the exact silent-degradation we
	// forbid in NewHarness.
	if err := ai.ValidateDifficulty(cfg.AIDifficulty, cfg.AnthropicKey); err != nil {
		log.Fatalf("invalid gateway default (AI_DIFFICULTY=%q): %v", cfg.AIDifficulty, err)
	}

	dex, err := domain.LoadDex(envOr("DATA_DIR", "data"), cfg.DataVersion)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	rc, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("connect redis: %v", err)
	}
	defer rc.Close()

	broker, err := mq.Connect(ctx, cfg.RabbitURL)
	if err != nil {
		log.Fatalf("connect rabbitmq: %v", err)
	}
	defer broker.Close()

	eq, err := broker.NewEventQueue()
	if err != nil {
		log.Fatalf("event queue: %v", err)
	}
	hub := httpapi.NewHub(eq)
	go func() {
		if err := hub.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("event hub stopped: %v", err)
		}
	}()

	srv := httpapi.NewServer(cfg, dex, st, rc, broker, hub, envOr("WEB_DIR", "web"))
	httpServer := &http.Server{Addr: cfg.GatewayAddr, Handler: srv.Routes()}

	go func() {
		log.Printf("listening on %s", cfg.GatewayAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
