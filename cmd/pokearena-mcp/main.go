// pokearena-mcp is the MCP server that lets an external agent (Claude Code
// first; the protocol is agent-agnostic) play a PokéArena battle. It runs on
// the user's machine as a stdio MCP server and offers two ways in:
//
// start_battle runs the whole battle inside this process against the built-in
// opponent, on the dataset embedded in the binary. No gateway, no Docker, no
// second player, and no data/ directory — so it works from any directory,
// including a go install'ed binary with no checkout anywhere near it.
//
// join_battle attaches to a battle on a running gateway over a WebSocket, for
// playing a human or another agent in the live arena.
//
// Quick start — nothing to run first:
//
//	claude mcp add pokearena -- go run ./cmd/pokearena-mcp
//
// Then: start_battle → submit_team → (wait → act)* → leave_battle.
//
// For the live arena instead, bring the stack up and point at it:
//
//	docker compose up -d
//	POKEARENA_GATEWAY_URL=ws://localhost:8080 go run ./cmd/pokearena-mcp
//
// POKEARENA_GATEWAY_URL is read only by join_battle, so an unreachable
// gateway costs nothing until you ask to join one.
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/shaumik/PokeArena/internal/mcpserver"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg := mcpserver.Config{
		// Localhost default keeps `go run` zero-config during development.
		// Production deployment overrides via env so a single binary can
		// point at any gateway (staging, prod, local).
		GatewayURL: envOr("POKEARENA_GATEWAY_URL", "ws://localhost:8080"),
	}

	// SIGINT/SIGTERM unblocks Run cleanly so the MCP client sees a normal
	// disconnect rather than a hung stdin pipe. stdio MCP transport ends
	// naturally when the client closes stdin too, so most exits don't go
	// through this path — it's the signal-from-outside case.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := mcpserver.New(cfg)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !isCleanShutdown(err) {
		log.Fatalf("pokearena-mcp: %v", err)
	}
}

// isCleanShutdown recognizes the normal ways an MCP stdio session ends:
// the user hits ^C (context canceled), or the MCP client closes stdin
// (the SDK surfaces this as io.EOF, sometimes wrapped inside a "server
// is closing" jsonrpc2 error whose message string ends with "EOF").
// Treating these as fatal would make every clean Claude Code disconnect
// look like an error in logs.
func isCleanShutdown(err error) bool {
	switch {
	case errors.Is(err, context.Canceled):
		return true
	case errors.Is(err, io.EOF):
		return true
	case strings.HasSuffix(err.Error(), "EOF"):
		// The internal jsonrpc2 layer composes this string from a code +
		// message; the underlying EOF isn't always reachable via Unwrap.
		return true
	default:
		return false
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
