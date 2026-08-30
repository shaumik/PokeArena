// Package mcpserver is pokearena-mcp's core: a Server that registers the
// agent-facing tools (join/view/wait/act/leave) and bridges them to a
// running gateway over WebSocket.
//
// Process model: one long-running server per Claude Code (or other MCP
// client) session. A single session can play many sequential battles —
// concurrent battles are explicitly out of scope for v1, so the Server
// holds at most one battle's state at a time (see backlog/mcp-server.md
// §7). The state itself lives behind methods on Server that future
// commits will wire in; this commit only proves the tool surface boots.
package mcpserver

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config is the per-process configuration. Today just the gateway URL;
// any future knobs (timeout caps, log level) belong here so main.go
// stays a thin entry point.
type Config struct {
	// GatewayURL is the base wss:// (or ws://) of the pokearena gateway,
	// e.g. "wss://pokearena.example". The server appends the per-battle
	// /api/battles/{id}/play?slot=...&token=... path itself.
	GatewayURL string
}

// Server holds the per-process state. It owns its registered tools but
// not its transport — the caller wires it to a transport via Run. The
// embedded session is what the tool handlers operate on; see session.go.
type Server struct {
	cfg     Config
	mcp     *mcp.Server
	session *session

	// dexCache is the result of one GET /api/pokemon, fetched on first
	// use and cached for the life of the process. See dexproxy.go for
	// why we cache and why a process-lifetime TTL is acceptable.
	dexMu    sync.Mutex
	dexCache []dexEntry

	// itemCache is the result of one GET /api/items, cached on the same
	// terms as dexCache: the held-item catalog only changes when an
	// operator runs the data-sync pipeline.
	itemMu    sync.Mutex
	itemCache []itemEntry

	// natureCache and rulesCache are GET /api/natures and GET /api/rules
	// on the same terms again. Both are small, fixed tables — 25 natures
	// and a handful of format constants — that only move with a deploy.
	natureMu    sync.Mutex
	natureCache []natureEntry

	rulesMu    sync.Mutex
	rulesCache *formatRules

	// offline is the dataset compiled into this binary: the dex and the
	// opponent roster start_battle plays against. Nil only if the embedded
	// data failed to load, in which case offlineErr says why — the gateway
	// tools still work, so a bad embed degrades the server rather than
	// killing it at boot.
	offline    *offlineData
	offlineErr error
}

// New builds a Server, registers every agent-facing tool, and returns
// it ready to Run. Tool handlers live in tools.go; the session state
// machine lives in session.go.
func New(cfg Config) *Server {
	s := &Server{
		cfg: cfg,
		mcp: mcp.NewServer(&mcp.Implementation{
			Name:    "pokearena-mcp",
			Version: "0.1.0",
		}, nil),
		session: newSession(cfg),
	}

	// Load the embedded dataset up front. Beyond backing start_battle, this
	// seeds the reference caches so find_pokemon / get_pokemon / list_items /
	// list_natures answer without a gateway — they proxy the gateway's REST
	// API otherwise (dexproxy.go), and four tools that fail without a server
	// would defeat the point of an offline mode.
	if d, err := loadOfflineData(); err != nil {
		s.offlineErr = err
	} else {
		s.offline = d
		s.seedCaches(d)
	}

	s.registerTools()
	return s
}

// Run serves the MCP protocol over the given transport (StdioTransport
// in normal use; an in-memory transport in tests). Blocks until the
// peer disconnects or ctx is canceled.
func (s *Server) Run(ctx context.Context, t mcp.Transport) error {
	return s.mcp.Run(ctx, t)
}
