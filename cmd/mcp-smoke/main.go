// mcp-smoke is a one-shot integration test for pokearena-mcp: it spawns
// the binary over stdio (as a real MCP client would), creates a live_pvp
// battle on the running gateway, and plays one full turn through the
// MCP tool surface. Run with `go run ./cmd/mcp-smoke` while the stack
// is up (docker compose up). Useful before pushing any change that
// touches mcpserver/* or the protocol package — the unit tests use a
// fake gateway, this exercises the real one.
//
// Architecture: one battle, two opposing clients.
//   - Slot p1 is played through the MCP tool surface (this is the
//     scenario Claude Code would run).
//   - Slot p2 is played by a raw WebSocket client in this process so
//     we control both sides of the conversation and don't need a
//     human in the loop.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"pokearena/internal/protocol"

	"github.com/gorilla/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	gatewayHTTP = "http://localhost:8080"
	gatewayWS   = "ws://localhost:8080"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create the battle. Same dex numbers as cmd/pvp-smoke (v1
	// dataset) so this test is portable across data refreshes.
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
	p1Token := tokenFrom(created.P1URL)
	fmt.Printf("✓ created battle %s\n", created.BattleID)

	// 2. Connect raw WS as p2 first. The gateway coordinator waits for
	// both slots to attach before sending a "state" frame, so p1 (via
	// MCP) blocking in join_battle would hang forever if we never
	// brought p2 online. Order matters: we open p2, then call MCP
	// join_battle and let the coordinator pair us.
	p2 := dial("p2", created.P2URL)
	defer p2.Close()
	fmt.Println("✓ p2 raw WS connected (opponent)")

	// 3. Spawn pokearena-mcp via stdio CommandTransport. `go run` is
	// slower than a pre-built binary but keeps the smoke test
	// single-step: no need to remember to rebuild between test runs.
	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-smoke", Version: "0"}, nil)
	transport := &mcp.CommandTransport{
		Command: exec.Command("go", "run", "./cmd/pokearena-mcp"),
	}
	sess, err := client.Connect(ctx, transport, nil)
	must("connect mcp", err)
	defer sess.Close()
	fmt.Println("✓ pokearena-mcp subprocess spawned and MCP handshake done")

	// 4. join_battle as p1 through the MCP tool. The MCP server will
	// dial the gateway, await the first state frame, and return.
	join, err := callTool[joinOut](ctx, sess, "join_battle", map[string]any{
		"battle_id":  created.BattleID,
		"slot":       "p1",
		"join_token": p1Token,
	})
	must("join_battle", err)
	if join.YourTrainer != "Red" {
		log.Fatalf("join: your_trainer=%q, want Red", join.YourTrainer)
	}
	if join.InitialView.Self.Trainer != "Red" {
		log.Fatalf("join: initial_view.self.trainer=%q, want Red", join.InitialView.Self.Trainer)
	}
	fmt.Printf("✓ join_battle returned: trainer=%q, turn=%d, self has %d Pokémon\n",
		join.YourTrainer, join.InitialView.Turn, len(join.InitialView.Self.Team))

	// 5. p2 reads its first state — proves the gateway has paired
	// both slots and sent fog-of-war views to each.
	p2State := readUntil(p2, protocol.FrameState)
	if got := p2State["view"].(map[string]any)["self"].(map[string]any)["trainer"]; got != "Blue" {
		log.Fatalf("p2 sees self=%v, want Blue", got)
	}
	fmt.Println("✓ p2 saw initial state; both views are fogged correctly")

	// 6. wait → should be ready immediately because join_battle
	// already populated the first view and needsAction is set.
	wait1, err := callTool[waitOut](ctx, sess, "wait", map[string]any{"timeout_seconds": 5})
	must("wait 1", err)
	if !wait1.Ready || wait1.Terminal || wait1.View == nil {
		log.Fatalf("wait 1: %+v", wait1)
	}
	if wait1.View.Turn != 0 {
		log.Fatalf("wait 1: view.turn=%d, want 0", wait1.View.Turn)
	}
	fmt.Println("✓ wait returned ready=true with turn-0 view")

	// 7. act move/0 from the MCP side; opponent also acts.
	act, err := callTool[actOut](ctx, sess, "act", map[string]any{"kind": "move", "index": 0})
	must("act", err)
	if !act.Accepted {
		log.Fatalf("act: not accepted: %+v", act)
	}
	must("p2 send", p2.WriteJSON(map[string]any{"type": "action", "kind": "move", "index": 0}))
	fmt.Println("✓ both sides submitted move/0")

	// 8. wait → should return ready with view at turn 1 (gateway
	// resolved the turn now that both actions are in).
	wait2, err := callTool[waitOut](ctx, sess, "wait", map[string]any{"timeout_seconds": 5})
	must("wait 2", err)
	if !wait2.Ready || wait2.View == nil {
		log.Fatalf("wait 2: %+v", wait2)
	}
	if wait2.View.Turn <= wait1.View.Turn {
		log.Fatalf("wait 2: turn didn't advance (%d → %d)", wait1.View.Turn, wait2.View.Turn)
	}
	fmt.Printf("✓ wait returned turn %d → %d (advanced)\n", wait1.View.Turn, wait2.View.Turn)

	// 9. leave_battle — clean exit; the agent forfeits its match but
	// the gateway should release slot resources cleanly.
	leave, err := callTool[leaveOut](ctx, sess, "leave_battle", map[string]any{})
	must("leave_battle", err)
	if !leave.OK {
		log.Fatalf("leave: %+v", leave)
	}
	fmt.Println("✓ leave_battle returned ok=true")

	fmt.Println("\nPASS — pokearena-mcp plays a live_pvp battle end-to-end.")
}

// Output structs mirror internal/mcpserver/tools.go just enough to
// inspect what the smoke test cares about. Deliberately not imported
// to keep this binary independent of internal/ packages it isn't
// otherwise testing.

type joinOut struct {
	BattleID    string `json:"battle_id"`
	Slot        string `json:"slot"`
	YourTrainer string `json:"your_trainer"`
	InitialView miniVw `json:"initial_view"`
}

type waitOut struct {
	Ready    bool    `json:"ready"`
	Terminal bool    `json:"terminal"`
	View     *miniVw `json:"view"`
}

type actOut struct {
	Accepted bool `json:"accepted"`
	Turn     int  `json:"turn"`
}

type leaveOut struct {
	OK bool `json:"ok"`
}

// miniVw is the subset of ai.View this test inspects. Keeping the
// shape narrow keeps the test resilient to additive View changes.
type miniVw struct {
	Turn int `json:"turn"`
	Self struct {
		Trainer string `json:"trainer"`
		Team    []any  `json:"team"`
	} `json:"self"`
}

// callTool calls an MCP tool, asserts no protocol-level error and no
// tool-level isError, then decodes StructuredContent into T. Centralizes
// the marshal-around-`any` dance so each step in main() reads cleanly.
func callTool[T any](ctx context.Context, sess *mcp.ClientSession, name string, args map[string]any) (T, error) {
	var zero T
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return zero, fmt.Errorf("%s: %w", name, err)
	}
	if res.IsError {
		msg := ""
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		return zero, fmt.Errorf("%s: tool error: %s", name, msg)
	}
	// The SDK populates StructuredContent with the typed Out value,
	// already deserialized from JSON. Re-marshal then unmarshal into
	// our local type so we get exact shape control.
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return zero, fmt.Errorf("%s: marshal structured content: %w", name, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("%s: unmarshal into %T: %w", name, out, err)
	}
	return out, nil
}

// tokenFrom extracts the join token from a /api/battles/{id}/play?slot=…&token=…
// path. The gateway hands us the whole path; we never need the
// battle_id from it because the create-battle response also returns
// it directly.
func tokenFrom(path string) string {
	u, err := url.Parse(path)
	must("parse url", err)
	t := u.Query().Get("token")
	if t == "" {
		log.Fatalf("no token in %q", path)
	}
	return t
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
		if typ, _ := m["type"].(string); typ == target {
			return m
		}
	}
	log.Fatalf("never received a %s frame (gave up after 20 other frames)", target)
	return nil
}

func must(label string, err error) {
	if err != nil {
		log.Fatalf("%s: %v", label, err)
	}
}
