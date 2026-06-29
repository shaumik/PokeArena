package main

import (
	"context"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// The TUI deliberately does NOT reuse internal/gwclient. That client decodes
// frames into protocol.MatchUpdate, whose View is an *ai.View — and ai.View
// has a redacting MarshalJSON (it emits the foe's hp_pct) but no matching
// struct field to receive hp_pct on the way back in. A Go client that decodes
// the foe into engine.Pokemon therefore silently loses the opponent's HP
// percentage and revealed-move ids (which is why the reference LLM agent plays
// semi-blind to foe HP). The browser SPA avoids this because JavaScript reads
// foe.hp_pct straight off the JSON. To render the foe HP bar the way the SPA
// does, the TUI decodes the foe through its own wire types below — a faithful
// reader — while still sharing internal/protocol for everything it sends and
// for the room/lifecycle frame shapes.

// frame mirrors protocol.MatchUpdate but swaps in a foe-aware view decoder.
type frame struct {
	Type    string               `json:"type"`
	View    *battleView          `json:"view,omitempty"`
	Log     []engine.LogLine     `json:"log,omitempty"`
	Winner  *int                 `json:"winner,omitempty"`
	Turn    int                  `json:"turn,omitempty"`
	Message string               `json:"message,omitempty"`
	Room    *protocol.RoomUpdate `json:"room,omitempty"`
}

// battleView mirrors ai.View's wire shape. Self is unredacted and decodes
// straight into the engine type; the foe goes through foeView so the
// percentage HP and revealed-move ids the wire carries survive the round trip.
type battleView struct {
	Me                int                   `json:"me"`
	Self              engine.Side           `json:"self"`
	Foe               foeView               `json:"foe"`
	FoeBenchAlive     int                   `json:"foe_bench_alive"`
	Phase             engine.Phase          `json:"phase"`
	Turn              int                   `json:"turn"`
	Replace           bool                  `json:"replace"`
	Weather           *engine.WeatherState  `json:"weather,omitempty"`
	Terrain           *engine.TerrainState  `json:"terrain,omitempty"`
	PseudoWeather     engine.PseudoWeather  `json:"pseudo_weather"`
	FoeConditions     engine.SideConditions `json:"foe_conditions"`
	FoeSlotConditions ai.FoeSlotConditions  `json:"foe_slot_conditions"`
}

// foeView is the opponent's active as it appears on the wire: percentage HP
// (no absolute count), revealed move ids (no PP), plus the public status,
// stages and volatiles. It mirrors ai.foeWire exactly.
type foeView struct {
	Name      string            `json:"name"`
	Type1     domain.Type       `json:"type1"`
	Type2     domain.Type       `json:"type2"`
	Status    engine.StatusCond `json:"status"`
	Stages    engine.Stages     `json:"stages"`
	Volatiles engine.Volatiles  `json:"volatiles"`
	HPPct     int               `json:"hp_pct"`
	Moves     []foeMove         `json:"moves"`
}

type foeMove struct {
	MoveID string `json:"move_id"`
}

// toAIView reconstructs an ai.View sufficient for ai.LegalActions: our own
// side in full plus a minimal foe active. Every legality gate (PP, partial
// trap, charging, Taunt, Choice lock, …) reads OUR side; the foe pokemon only
// needs to exist as the active, so the fog-bucketed foe HP/stats we lack
// don't change which of our actions are legal.
func (v *battleView) toAIView() ai.View {
	return ai.View{
		Me:   v.Me,
		Self: v.Self,
		Foe: engine.Pokemon{
			Name:      v.Foe.Name,
			Type1:     v.Foe.Type1,
			Type2:     v.Foe.Type2,
			Status:    v.Foe.Status,
			Stages:    v.Foe.Stages,
			Volatiles: v.Foe.Volatiles,
			Moves:     foeMoveSlots(v.Foe.Moves),
		},
		FoeBenchAlive:     v.FoeBenchAlive,
		Phase:             v.Phase,
		Turn:              v.Turn,
		Replace:           v.Replace,
		Weather:           v.Weather,
		Terrain:           v.Terrain,
		PseudoWeather:     v.PseudoWeather,
		FoeConditions:     v.FoeConditions,
		FoeSlotConditions: v.FoeSlotConditions,
	}
}

func foeMoveSlots(ms []foeMove) []engine.MoveSlot {
	out := make([]engine.MoveSlot, len(ms))
	for i, m := range ms {
		out[i] = engine.MoveSlot{MoveID: m.MoveID}
	}
	return out
}

// wsClient is a single gateway WebSocket connection, modeled on
// internal/gwclient.Client: one read pump, typed frames out via Updates, typed
// actions in via Send, idempotent Close. The only difference is the frame type
// (see the package comment above).
type wsClient struct {
	conn    *websocket.Conn
	updates chan frame
	closed  chan error
	stop    chan struct{}
	once    sync.Once
	lastErr error
}

// dial opens a WS to baseURL + the live_pvp play path for (battleID, slot,
// token) and starts the read pump. baseURL is a ws:// or wss:// origin.
func dial(ctx context.Context, baseURL, battleID, slot, token string) (*wsClient, error) {
	return dialPath(ctx, baseURL, protocol.PlayPath(battleID, slot, token))
}

// dialLive opens a WS to a single-player (mode=live) battle against the
// built-in CPU. The live join path carries no slot or token: the gateway pins
// the human to p1 and the battle id is the entire auth model (see
// httpapi.handleLiveWS). A slot/token query would instead route to the
// live_pvp handler and be rejected, so this path is the only way in.
func dialLive(ctx context.Context, baseURL, battleID string) (*wsClient, error) {
	return dialPath(ctx, baseURL, liveJoinPath(battleID))
}

// liveJoinPath is the WS path for a mode=live battle. It deliberately carries
// no slot or token query: that is what distinguishes it from a live_pvp join
// (protocol.PlayPath) and routes it to httpapi.handleLiveWS.
func liveJoinPath(battleID string) string {
	return "/api/battles/" + battleID + "/play"
}

// dialPath opens a WS to baseURL + path and starts the read pump.
func dialPath(ctx context.Context, baseURL, path string) (*wsClient, error) {
	u, err := joinURL(baseURL, path)
	if err != nil {
		return nil, err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	c := &wsClient{
		conn:    conn,
		updates: make(chan frame, 8), // matches the gateway's per-slot buffer
		closed:  make(chan error, 1),
		stop:    make(chan struct{}),
	}
	go c.readPump()
	return c, nil
}

// Updates is the stream of frames from the server, closed when the connection
// ends. Send writes one client frame; it must be called from a single
// goroutine at a time (the bubbletea Update loop is that goroutine).
func (c *wsClient) Updates() <-chan frame             { return c.updates }
func (c *wsClient) Closed() <-chan error              { return c.closed }
func (c *wsClient) Send(m protocol.WsClientMsg) error { return c.conn.WriteJSON(m) }

func (c *wsClient) Close() error {
	c.once.Do(func() {
		close(c.stop)
		_ = c.conn.Close()
	})
	return nil
}

func (c *wsClient) readPump() {
	defer close(c.updates)
	defer func() {
		select {
		case c.closed <- c.terminalErr():
		default:
		}
	}()
	for {
		var f frame
		if err := c.conn.ReadJSON(&f); err != nil {
			c.lastErr = err
			return
		}
		select {
		case c.updates <- f:
		case <-c.stop:
			return
		}
	}
}

// terminalErr suppresses the "use of closed connection" noise that our own
// Close triggers, distinguishing it from a genuine server/network failure.
func (c *wsClient) terminalErr() error {
	select {
	case <-c.stop:
		return nil
	default:
		return c.lastErr
	}
}

// joinURL combines a ws:// origin with a path that may carry a query, via
// net/url so a trailing slash or missing leading slash can't produce a broken
// URL. Same approach as gwclient.joinURL.
func joinURL(baseURL, path string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	p, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u.Path = p.Path
	u.RawQuery = p.RawQuery
	return u.String(), nil
}
