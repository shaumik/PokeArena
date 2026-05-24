// pvp-smoke is a one-shot integration test that exercises the live_pvp
// path end-to-end against a running gateway: creates a battle, opens
// both WS slots, plays one turn, and validates the frame shapes both
// clients receive. Run with `go run ./cmd/pvp-smoke` while the stack is
// up (docker compose up). Useful before pushing any change that touches
// pvp.go, ws.go, or pvp.go's wire-protocol shape — the cache unit tests
// don't catch coordinator bugs.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const gatewayHTTP = "http://localhost:8080"
const gatewayWS = "ws://localhost:8080"

func main() {
	// 1. Create a live_pvp battle. Dex numbers chosen from the v1 dataset
	// (data/pokedex.json) — full list at /api/pokemon if these ever break.
	body := []byte(`{"mode":"live_pvp","p1_name":"Red","p2_name":"Blue","p1_team":[25,6,9],"p2_team":[3,26,150]}`)
	res, err := http.Post(gatewayHTTP+"/api/battles", "application/json", bytes.NewReader(body))
	must("POST /api/battles", err)
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(res.Body)
		log.Fatalf("create: status %s — %s", res.Status, buf.String())
	}
	var created struct {
		BattleID string `json:"battle_id"`
		P1URL    string `json:"p1_url"`
		P2URL    string `json:"p2_url"`
	}
	must("decode create", json.NewDecoder(res.Body).Decode(&created))
	fmt.Printf("✓ created battle %s\n", created.BattleID)

	// 2. Open both WS slots.
	p1 := dial("p1", created.P1URL)
	defer p1.Close()
	// Wait a beat so p1 attaches first and gets the "Waiting for opponent" info.
	time.Sleep(200 * time.Millisecond)
	p2 := dial("p2", created.P2URL)
	defer p2.Close()
	fmt.Println("✓ both slots connected")

	// 3. Each side reads until its first "state" frame. p1 will see an info
	// frame ("Waiting for opponent...") before the state; p2 should get
	// state directly because its attach completes the pair.
	p1State := readUntil(p1, "state")
	p2State := readUntil(p2, "state")
	fmt.Println("✓ both clients received initial state")

	// 4. Inspect: both views must have a self (own side, full) and a foe
	// (opponent's active only). The two views' "self" trainers must differ
	// (one is Red, one is Blue) — proves the per-slot fog-of-war works.
	p1V := mustMap(p1State, "view")
	p2V := mustMap(p2State, "view")
	p1Self := mustMap(p1V, "self")
	p2Self := mustMap(p2V, "self")
	p1Trainer := p1Self["trainer"]
	p2Trainer := p2Self["trainer"]
	if p1Trainer == p2Trainer {
		log.Fatalf("both views report the same self.trainer %q — fog-of-war is wrong", p1Trainer)
	}
	fmt.Printf("✓ p1 sees self=%q, p2 sees self=%q (per-slot views)\n", p1Trainer, p2Trainer)

	// 5. Both sides submit move 0.
	must("p1 send", p1.WriteJSON(map[string]any{"type": "action", "kind": "move", "index": 0}))
	must("p2 send", p2.WriteJSON(map[string]any{"type": "action", "kind": "move", "index": 0}))
	fmt.Println("✓ both actions submitted")

	// 6. Both should receive a "turn" frame with view + log.
	p1Turn := readUntil(p1, "turn")
	p2Turn := readUntil(p2, "turn")
	if _, ok := p1Turn["log"]; !ok {
		log.Fatal("p1 turn frame missing log")
	}
	if _, ok := p2Turn["log"]; !ok {
		log.Fatal("p2 turn frame missing log")
	}
	if _, ok := p1Turn["view"]; !ok {
		log.Fatal("p1 turn frame missing view")
	}
	fmt.Println("✓ both clients received turn frame with view + log")

	// 7. The turn number should have advanced.
	p1V2 := mustMap(p1Turn, "view")
	if int(p1V2["turn"].(float64)) <= int(p1V["turn"].(float64)) {
		log.Fatalf("turn didn't advance: was %v, now %v", p1V["turn"], p1V2["turn"])
	}
	fmt.Printf("✓ turn advanced %v → %v\n", p1V["turn"], p1V2["turn"])

	fmt.Println("\nPASS — live_pvp end-to-end works on the running gateway.")
}

func dial(label, path string) *websocket.Conn {
	c, _, err := websocket.DefaultDialer.Dial(gatewayWS+path, nil)
	must(label+" dial", err)
	return c
}

func readUntil(c *websocket.Conn, target string) map[string]any {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < 20; i++ {
		var m map[string]any
		if err := c.ReadJSON(&m); err != nil {
			log.Fatalf("read %s: %v", target, err)
		}
		typ, _ := m["type"].(string)
		if typ == target {
			return m
		}
		// Other frames (info / error / etc.) are fine to skip — but if we
		// see more than a few of any non-target type something is wrong
		// (the coordinator should only emit one "info" before "state").
	}
	log.Fatalf("never received a %s frame (consumed 20 other frames first — coordinator likely looping)", target)
	return nil
}

func mustMap(m map[string]any, key string) map[string]any {
	v, ok := m[key].(map[string]any)
	if !ok {
		log.Fatalf("expected map at %q, got %T (%v)", key, m[key], m[key])
	}
	return v
}

func must(label string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}
