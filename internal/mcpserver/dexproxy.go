package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// dexproxy.go: read-only Pokédex queries for the agent. The gateway
// already serves the canonical dataset at GET /api/pokemon; the MCP
// server proxies that endpoint rather than embedding a copy of the
// data. This keeps the MCP binary thin, avoids drift, and means
// `make agent-data` doesn't apply here.
//
// The list is fetched on first use and cached in-process — the
// dataset only changes when an operator runs the data-sync pipeline,
// so a per-process cache is dramatically cheaper than fetching on
// every find_pokemon call and acceptable until we have a real
// hot-reload signal.

// dexEntry mirrors httpapi.pokedexEntry shape. Lives here (not in
// protocol/) because it's an internal-to-MCP cache shape, not a
// stable wire contract.
type dexEntry struct {
	DexNo int           `json:"dex_no"`
	Name  string        `json:"name"`
	Type1 string        `json:"type1"`
	Type2 string        `json:"type2"`
	Base  dexBaseStats  `json:"base"`
	Moves []dexMoveInfo `json:"moves"`
}

type dexBaseStats struct {
	HP    int `json:"hp"`
	Atk   int `json:"atk"`
	Def   int `json:"def"`
	SpAtk int `json:"spatk"`
	SpDef int `json:"spdef"`
	Speed int `json:"speed"`
}

type dexMoveInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Category string `json:"category"`
	Power    int    `json:"power"`
	PP       int    `json:"pp"`
}

// fetchDex returns the cached pokédex if loaded, otherwise hits the
// gateway's HTTP endpoint and caches the result for the life of the
// process. Returns (nil, err) only on transport / JSON failure; the
// resulting slice is read-only for callers.
func (s *Server) fetchDex(ctx context.Context) ([]dexEntry, error) {
	s.dexMu.Lock()
	if s.dexCache != nil {
		out := s.dexCache
		s.dexMu.Unlock()
		return out, nil
	}
	s.dexMu.Unlock()

	httpBase, err := dexURLFromGateway(s.cfg.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("derive http base from gateway URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpBase+"/api/pokemon", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /api/pokemon: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gateway /api/pokemon returned %d: %s", resp.StatusCode, string(body))
	}
	var entries []dexEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode /api/pokemon: %w", err)
	}

	s.dexMu.Lock()
	s.dexCache = entries
	s.dexMu.Unlock()
	return entries, nil
}

// dexURLFromGateway swaps ws/wss → http/https so the same configured
// gateway URL works for both the WS play path and the HTTP REST path.
// Any other scheme is rejected — silent fallthrough would point the
// proxy at the wrong host and surface as a confusing timeout later.
func dexURLFromGateway(gw string) (string, error) {
	u, err := url.Parse(gw)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
		// already an HTTP base — accept as-is.
	default:
		return "", fmt.Errorf("unsupported gateway scheme %q (want ws/wss/http/https)", u.Scheme)
	}
	// Strip any trailing path so callers can append /api/...
	u.Path = ""
	u.RawQuery = ""
	return u.String(), nil
}
