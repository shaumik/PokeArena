// Command pokearena-tui is a terminal front-end for PokéArena live_pvp battles.
//
// It is "just another reader" of the battle core: it speaks the same gateway
// WebSocket protocol (internal/protocol) the browser SPA and the MCP server
// speak — reading fog-of-war views and submitting moves — but renders the
// battle as a full-screen terminal UI instead of a web page. The battle engine
// is untouched; the terminal is purely a presentation layer.
//
// Usage:
//
//	# join an existing battle from a share URL (SPA "Pv-Player" / Pv-Agent shape)
//	pokearena-tui 'http://localhost:8080/?battle=ID&slot=p1&token=TOK'
//
//	# create a fresh live_pvp battle, take slot p1, and print the opponent's invite
//	pokearena-tui --create --gateway http://localhost:8080
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pokearena"
	"pokearena/internal/domain"
)

const (
	slotP1 = "p1"
	slotP2 = "p2"
)

func main() {
	create := flag.Bool("create", false, "create a fresh live_pvp battle, join as p1, and print the opponent invite")
	gateway := flag.String("gateway", "http://localhost:8080", "gateway base URL (http/https); used with --create")
	name := flag.String("name", "Terminal Trainer", "your trainer name")
	dataVersion := flag.String("data-version", "gen1-v1", "dataset version label; must match the gateway's DATA_VERSION")
	flag.Usage = usage
	flag.Parse()

	dex, err := domain.LoadDexFS(pokearena.DataFS(), *dataVersion)
	if err != nil {
		die("load embedded dataset: " + err.Error())
	}

	var wsBase, battleID, slot, token, invite string
	if *create {
		wsBase, battleID, slot, token, invite, err = createBattle(*gateway, *name)
		if err != nil {
			die("create battle: " + err.Error())
		}
	} else {
		share := flag.Arg(0)
		if share == "" {
			usage()
			os.Exit(2)
		}
		wsBase, battleID, slot, token, err = parseShareURL(share)
		if err != nil {
			die("invalid share URL: " + err.Error())
		}
	}

	cl, err := dial(context.Background(), wsBase, battleID, slot, token)
	if err != nil {
		die("dial gateway: " + err.Error())
	}
	defer cl.Close()

	m := newModel(cl, dex, battleID, slot)
	m.inviteURL = invite

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		die("tui: " + err.Error())
	}
}

// parseShareURL extracts the ws origin + join coordinates from an SPA share URL
// of the shape http(s)://host/?battle=ID&slot=p1|p2&token=TOK — the same shape
// the gateway hands a Pv-Player creator and that pokearena-agent consumes.
func parseShareURL(raw string) (wsBase, battleID, slot, token string, err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		err = perr
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		err = fmt.Errorf("scheme %q is not http(s); paste the share URL from the SPA", u.Scheme)
		return
	}
	q := u.Query()
	battleID, slot, token = q.Get("battle"), q.Get("slot"), q.Get("token")
	if battleID == "" || slot == "" || token == "" {
		err = errors.New("URL is missing one of battle/slot/token query params")
		return
	}
	wsBase = wsOrigin(u.Scheme, u.Host)
	return
}

// createBattle POSTs a live_pvp battle to the gateway, takes slot p1 for
// ourselves, and builds an opponent invite URL for slot p2.
func createBattle(gateway, name string) (wsBase, battleID, slot, token, invite string, err error) {
	gu, perr := url.Parse(gateway)
	if perr != nil {
		err = perr
		return
	}
	body, _ := json.Marshal(map[string]any{"mode": "live_pvp", "p1_name": name, "p2_name": "Challenger"})
	resp, perr := http.Post(strings.TrimRight(gateway, "/")+"/api/battles", "application/json", bytes.NewReader(body))
	if perr != nil {
		err = perr
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err = fmt.Errorf("gateway returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
		return
	}
	var out struct {
		BattleID string `json:"battle_id"`
		P1URL    string `json:"p1_url"`
		P2URL    string `json:"p2_url"`
	}
	if perr := json.NewDecoder(resp.Body).Decode(&out); perr != nil {
		err = perr
		return
	}
	p1Tok, p2Tok := playTokenOf(out.P1URL), playTokenOf(out.P2URL)
	if out.BattleID == "" || p1Tok == "" || p2Tok == "" {
		err = errors.New("create response missing battle_id or tokens")
		return
	}
	wsBase = wsOrigin(gu.Scheme, gu.Host)
	battleID, slot, token = out.BattleID, slotP1, p1Tok
	invite = fmt.Sprintf("%s://%s/?battle=%s&slot=%s&token=%s", gu.Scheme, gu.Host, battleID, slotP2, p2Tok)
	return
}

// playTokenOf pulls the token query param out of a gateway play path
// ("/api/battles/ID/play?slot=p2&token=TOK").
func playTokenOf(playPath string) string {
	u, err := url.Parse(playPath)
	if err != nil {
		return ""
	}
	return u.Query().Get("token")
}

func wsOrigin(scheme, host string) string {
	if scheme == "https" {
		return "wss://" + host
	}
	return "ws://" + host
}

func usage() {
	fmt.Fprintln(os.Stderr, `pokearena-tui — terminal front-end for PokéArena live_pvp battles

Usage:
  pokearena-tui <SPA-share-URL>          join an existing battle
  pokearena-tui --create [--gateway URL] create a battle, take p1, print the invite

Flags:`)
	flag.PrintDefaults()
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "pokearena-tui: "+msg)
	os.Exit(1)
}
