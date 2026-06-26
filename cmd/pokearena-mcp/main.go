// pokearena-mcp is the MCP server that lets an external agent (Claude Code
// first; the protocol is agent-agnostic) play a PokéArena battle against
// a human. It runs on the user's machine as a stdio MCP server, opens a
// WebSocket to the gateway when the agent calls join_battle, and bridges
// the agent's tool-call surface to the gateway's frame protocol.
//
// Quick start (local dev):
//
//	# in one terminal, the gateway:
//	docker compose up -d
//
//	# in another, this binary, talking to it:
//	POKEARENA_GATEWAY_URL=ws://localhost:8080 go run ./cmd/pokearena-mcp
//
// To wire into Claude Code:
//
//	claude mcp add pokearena -- go run ./cmd/pokearena-mcp
//
// Then Claude can call join_battle / view / wait / act / leave_battle.
// In this commit the handlers all return "not implemented" — the next
// commit wires them to the gateway WS.
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

	"pokearena/internal/mcpserver"

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
// the user hits ^C (context cancelled), or the MCP client closes stdin
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
