// pvp-smoke is a one-shot integration test that exercises the live_pvp
// path end-to-end against a running gateway: creates a battle, opens
// both WS slots, runs the picker phase (submit_team on both sides),
// plays one turn, and validates the frame shapes both clients receive.
//
// Run with `go run ./cmd/pvp-smoke` while the stack is up
// (docker compose up). Useful before pushing any change that touches
// pvp.go, ws.go, team_validation.go, or the protocol wire shape —
// unit tests don't catch coordinator + multi-slot bugs.
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

// pokedexEntry mirrors the wire shape served by /api/pokemon — we only
// care about dex_no and the move IDs in each species' learn list.
type pokedexEntry struct {
	DexNo int    `json:"dex_no"`
	Name  string `json:"name"`
	Moves []struct {
		ID string `json:"id"`
	} `json:"moves"`
}

type pick struct {
	DexNo   int      `json:"dex_no"`
	MoveIDs []string `json:"moves"`
}

func main() {
	// 0. Build a legal team straight from /api/pokemon: first 6 species,
	// each with their first 4 learnset moves. Dataset-independent — if
	// the curated set ever shrinks below 6 species this fails loudly.
	pokedex := fetchPokedex()
	if len(pokedex) < 6 {
		log.Fatalf("dataset has only %d species — need at least 6 for a legal team", len(pokedex))
	}
	team := make([]pick, 6)
	for i := 0; i < 6; i++ {
		sp := pokedex[i]
		moves := make([]string, 0, 4)
		for j, m := range sp.Moves {
			if j >= 4 {
				break
			}
			moves = append(moves, m.ID)
		}
		if len(moves) == 0 {
			log.Fatalf("species %s has no learnset — refusing to test against a broken dataset", sp.Name)
		}
		team[i] = pick{DexNo: sp.DexNo, MoveIDs: moves}
	}
	fmt.Printf("✓ built legal 6-pick team from /api/pokemon\n")

	// 1. Create a live_pvp battle. Teams are NO LONGER in the create
	// body — they arrive via submit_team during the picker phase.
	body := []byte(`{"mode":"live_pvp","p1_name":"Red","p2_name":"Blue"}`)
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
	time.Sleep(200 * time.Millisecond) // let p1 attach first to see the partial-room state
	p2 := dial("p2", created.P2URL)
	defer p2.Close()
	fmt.Println("✓ both slots connected")

	// 3. Drain to the first FrameRoom that reports both attached.
	p1Room := readUntilRoomBothAttached(p1)
	p2Room := readUntilRoomBothAttached(p2)
	fmt.Println("✓ both clients received room frame with both attached")

	// 4. Trainer perspective check — "you" must differ between the two
	// slots even before either has submitted (proves per-slot projection).
	if name1, name2 := youName(p1Room), youName(p2Room); name1 == name2 {
		log.Fatalf("both rooms report same you.trainer %q — server isn't projecting per-slot", name1)
	}

	// 5. Both sides submit their team. Same picks shape sent to both —
	// the engine builds a separate Pokémon instance per side.
	submit := map[string]any{"type": "submit_team", "picks": team}
	must("p1 submit_team", p1.WriteJSON(submit))
	must("p2 submit_team", p2.WriteJSON(submit))
	fmt.Println("✓ both teams submitted")

	// 6. Both sides should now receive a "state" frame — the room has
	// transitioned into ACTIVE and the engine is built.
	p1State := readUntil(p1, "state")
	p2State := readUntil(p2, "state")
	fmt.Println("✓ both clients received initial state after picker close")

	p1V := mustMap(p1State, "view")
	p2V := mustMap(p2State, "view")
	if mustMap(p1V, "self")["trainer"] == mustMap(p2V, "self")["trainer"] {
		log.Fatalf("both views report the same self.trainer — fog-of-war is wrong")
	}

	// 7. Both submit move 0.
	must("p1 action", p1.WriteJSON(map[string]any{"type": "action", "kind": "move", "index": 0}))
	must("p2 action", p2.WriteJSON(map[string]any{"type": "action", "kind": "move", "index": 0}))
	fmt.Println("✓ both actions submitted")

	// 8. Both should receive a "turn" frame.
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

	p1V2 := mustMap(p1Turn, "view")
	if int(p1V2["turn"].(float64)) <= int(p1V["turn"].(float64)) {
		log.Fatalf("turn didn't advance: was %v, now %v", p1V["turn"], p1V2["turn"])
	}
	fmt.Printf("✓ turn advanced %v → %v\n", p1V["turn"], p1V2["turn"])

	fmt.Println("\nPASS — picker room + live_pvp end-to-end works on the running gateway.")
}

func fetchPokedex() []pokedexEntry {
	res, err := http.Get(gatewayHTTP + "/api/pokemon")
	must("GET /api/pokemon", err)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Fatalf("pokedex fetch status %s", res.Status)
	}
	var out []pokedexEntry
	must("decode pokedex", json.NewDecoder(res.Body).Decode(&out))
	return out
}

func dial(label, path string) *websocket.Conn {
	c, _, err := websocket.DefaultDialer.Dial(gatewayWS+path, nil)
	must(label+" dial", err)
	return c
}

// readUntil consumes frames until one of the given type arrives. Bails
// after 20 unrelated frames — a healthy coordinator emits at most a
// handful before the target.
func readUntil(c *websocket.Conn, target string) map[string]any {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < 20; i++ {
		var m map[string]any
		if err := c.ReadJSON(&m); err != nil {
			log.Fatalf("read %s: %v", target, err)
		}
		if typ, _ := m["type"].(string); typ == target {
			return m
		}
	}
	log.Fatalf("never received a %s frame (consumed 20 other frames first)", target)
	return nil
}

// readUntilRoomBothAttached waits for a FrameRoom in which both you and
// them report Attached. Earlier "you only" rooms are passed through.
func readUntilRoomBothAttached(c *websocket.Conn) map[string]any {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	for i := 0; i < 20; i++ {
		var m map[string]any
		if err := c.ReadJSON(&m); err != nil {
			log.Fatalf("read room: %v", err)
		}
		if typ, _ := m["type"].(string); typ != "room" {
			continue
		}
		room := mustMap(m, "room")
		you := mustMap(room, "you")
		them := mustMap(room, "them")
		if youAttached, _ := you["attached"].(bool); !youAttached {
			continue
		}
		if themAttached, _ := them["attached"].(bool); themAttached {
			return m
		}
	}
	log.Fatalf("never received a room frame with both attached")
	return nil
}

func youName(roomFrame map[string]any) string {
	room := mustMap(roomFrame, "room")
	you := mustMap(room, "you")
	s, _ := you["trainer"].(string)
	return s
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
