// Command pokearena-agent is the reference agent harness for PokéArena.
//
// It joins a live_pvp slot — the second seat in a "Pv-Player" battle
// created from the SPA — and plays the battle to completion using an
// LLM of your choice. The cloud services hold no LLM credentials; your
// API key lives on your machine and never leaves it.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=sk-ant-…
//	pokearena-agent 'http://localhost:8080/?battle=ABC&slot=p2&token=XYZ'
//
// See docs/agent-harness.md for the design and README §"Connect your
// agent (Pv-Agent)" for the four-step walkthrough.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pokearena/internal/agentloop"
	"pokearena/internal/domain"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[pokearena-agent] ")

	slotURL := flag.String("slot-url", "",
		"Share URL from the SPA, e.g. http://host/?battle=ID&slot=p2&token=TOK (or pass as positional arg)")
	model := flag.String("model", "claude-haiku-4-5-20251001",
		"Anthropic model id (latency-optimized by default; switch to opus for stronger play at higher cost)")
	turnTimeout := flag.Duration("turn-timeout", 12*time.Second,
		"Per-turn LLM call budget; the gateway will default-action the slot if a turn takes longer")
	dataVersion := flag.String("data-version", "gen1-v1",
		"Dataset version label — must match the gateway's DATA_VERSION")
	flag.Usage = usage
	flag.Parse()

	if *slotURL == "" && flag.NArg() == 1 {
		*slotURL = flag.Arg(0)
	}
	if *slotURL == "" {
		usage()
		os.Exit(2)
	}

	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		die("ANTHROPIC_API_KEY env var is required (your key, your machine)")
	}

	gatewayURL, battleID, slot, token, err := parseSlotURL(*slotURL)
	if err != nil {
		die("invalid slot URL: " + err.Error())
	}

	dataSub, err := fs.Sub(dataFS, "data")
	if err != nil {
		die("locate embedded dataset: " + err.Error())
	}
	dex, err := domain.LoadDexFS(dataSub, *dataVersion)
	if err != nil {
		die("load embedded dataset: " + err.Error())
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("model=%s, gateway=%s, battle=%s, slot=%s", *model, gatewayURL, battleID, slot)

	cfg := agentloop.Config{
		GatewayURL:     gatewayURL,
		BattleID:       battleID,
		Slot:           slot,
		Token:          token,
		Dex:            dex,
		LLM:            newAnthropicClient(key, *model),
		PerTurnTimeout: *turnTimeout,
	}
	if err := agentloop.Run(ctx, cfg); err != nil {
		die("agent loop exited: " + err.Error())
	}
}

// parseSlotURL extracts (ws gateway base URL, battleID, slot, token)
// from the SPA share URL the gateway hands to a Pv-Player creator.
// http → ws, https → wss; query params drive the rest.
func parseSlotURL(raw string) (gatewayURL, battleID, slot, token string, err error) {
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
	battleID = q.Get("battle")
	slot = q.Get("slot")
	token = q.Get("token")
	if battleID == "" || slot == "" || token == "" {
		err = fmt.Errorf("URL is missing one of battle/slot/token query params")
		return
	}
	ws := "ws"
	if u.Scheme == "https" {
		ws = "wss"
	}
	gatewayURL = ws + "://" + u.Host
	return
}

func usage() {
	fmt.Fprintln(os.Stderr, `pokearena-agent — reference LLM harness for PokéArena Pv-Agent

Usage:
  pokearena-agent <SPA-share-URL>
  pokearena-agent --slot-url <SPA-share-URL> [flags]

Env:
  ANTHROPIC_API_KEY   required; your Anthropic key, used locally

Flags:`)
	flag.PrintDefaults()
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "pokearena-agent: "+msg)
	os.Exit(1)
}
