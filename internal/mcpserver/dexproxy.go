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
	DexNo     int           `json:"dex_no"`
	Name      string        `json:"name"`
	Type1     string        `json:"type1"`
	Type2     string        `json:"type2"`
	Base      dexBaseStats  `json:"base"`
	Abilities []string      `json:"abilities,omitempty"`
	Moves     []dexMoveInfo `json:"moves"`
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

	var entries []dexEntry
	if err := s.getJSON(ctx, "/api/pokemon", &entries); err != nil {
		return nil, err
	}

	s.dexMu.Lock()
	s.dexCache = entries
	s.dexMu.Unlock()
	return entries, nil
}

// itemEntry mirrors engine.ItemInfo, the shape GET /api/items returns.
// Same rationale as dexEntry: an internal-to-MCP cache shape, not a
// stable wire contract.
type itemEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// fetchItems returns the cached held-item catalog, fetching it from the
// gateway on first use. Mirrors fetchDex exactly — see that function for
// why the process-lifetime cache is acceptable.
func (s *Server) fetchItems(ctx context.Context) ([]itemEntry, error) {
	s.itemMu.Lock()
	if s.itemCache != nil {
		out := s.itemCache
		s.itemMu.Unlock()
		return out, nil
	}
	s.itemMu.Unlock()

	var entries []itemEntry
	if err := s.getJSON(ctx, "/api/items", &entries); err != nil {
		return nil, err
	}

	s.itemMu.Lock()
	s.itemCache = entries
	s.itemMu.Unlock()
	return entries, nil
}

// getJSON GETs path off the gateway's HTTP base and decodes the JSON body
// into out. Shared by fetchDex and fetchItems so the URL derivation,
// timeout, and status handling live in one place.
func (s *Server) getJSON(ctx context.Context, path string, out any) error {
	httpBase, err := dexURLFromGateway(s.cfg.GatewayURL)
	if err != nil {
		return fmt.Errorf("derive http base from gateway URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpBase+path, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("gateway %s returned %d: %s", path, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
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
