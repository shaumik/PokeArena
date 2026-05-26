package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/gwclient"
	"pokearena/internal/protocol"
)

// Config wires Run together. Every field except Logger is required.
type Config struct {
	// GatewayURL is the base ws://host:port (or wss://) of the gateway.
	// The play path is appended internally; do not include it here.
	GatewayURL string

	// BattleID, Slot, Token are the join coordinates a Pv-Player creator
	// sees in the SPA share URL. Slot is "p1" or "p2".
	BattleID, Slot, Token string

	// Dex is needed to render move metadata in the prompt and to log
	// decisions in human-readable form.
	Dex *domain.Dex

	// LLM is the provider adapter. Implementations live in callers.
	LLM LLMClient

	// PerTurnTimeout bounds each LLM call. Zero means the loop trusts
	// the LLM/ctx to enforce time on its own (not recommended; the
	// gateway will default-action the slot if the agent is too slow).
	PerTurnTimeout time.Duration

	// Logger is optional; defaults to log.Default().
	Logger *log.Logger
}

// Run dials the gateway, plays the battle to completion, and returns
// when FrameEnd arrives or the connection ends. nil means the battle
// finished naturally (regardless of who won); an error means a protocol,
// network, or context failure.
//
// LLM errors and malformed replies do not abort the battle — the loop
// falls back to the first legal action so the battle continues and the
// failure shows up in the log. This is the "reference harness" trade-off:
// a real production agent would likely retry once or surface the error,
// but the demo experience benefits from the battle always finishing.
func Run(ctx context.Context, cfg Config) error {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	gc, err := gwclient.Dial(ctx, cfg.GatewayURL, cfg.BattleID, cfg.Slot, cfg.Token)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	defer gc.Close()
	logger.Printf("joined battle %s as %s", cfg.BattleID, cfg.Slot)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case u, ok := <-gc.Updates():
			if !ok {
				// Connection ended. Drain Closed for a terminal error;
				// if it's nil we initiated, which shouldn't happen mid-Run.
				if cerr := <-gc.Closed(); cerr != nil {
					return fmt.Errorf("gateway connection closed: %w", cerr)
				}
				return errors.New("gateway connection closed before battle ended")
			}

			switch u.Type {
			case protocol.FrameInfo:
				if u.Message != "" {
					logger.Printf("info: %s", u.Message)
				}
			case protocol.FrameError:
				logger.Printf("gateway error: %s", u.Message)
			case protocol.FrameState, protocol.FrameTurn:
				if u.View == nil {
					continue
				}
				if err := decideAndSend(ctx, cfg, gc, *u.View, logger); err != nil {
					return err
				}
			case protocol.FrameEnd:
				if u.Winner != nil {
					logger.Printf("battle ended: winner=%d (we are %d)", *u.Winner, sideFromSlot(cfg.Slot))
				} else {
					logger.Printf("battle ended: no winner")
				}
				return nil
			}
		}
	}
}

func decideAndSend(ctx context.Context, cfg Config, gc *gwclient.Client, v ai.View, logger *log.Logger) error {
	acts := ai.LegalActions(v)
	if len(acts) == 0 {
		return fmt.Errorf("turn %d: no legal actions", v.Turn)
	}

	action, reasoning := decide(ctx, cfg, v, acts, logger)
	logger.Printf("turn %d: %s — %s", v.Turn, describeAction(cfg.Dex, v, action), reasoning)

	if err := gc.Send(toClientMsg(action)); err != nil {
		return fmt.Errorf("turn %d: send action: %w", v.Turn, err)
	}
	return nil
}

// decide runs the LLM, parses, validates, and on any failure falls back
// to the first legal action. The reasoning string is the LLM's own
// (when available) or a human-readable explanation of why we fell back.
func decide(ctx context.Context, cfg Config, v ai.View, acts []engine.Action, logger *log.Logger) (engine.Action, string) {
	callCtx := ctx
	if cfg.PerTurnTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, cfg.PerTurnTimeout)
		defer cancel()
	}

	user := RenderUserPrompt(cfg.Dex, v, acts)
	reply, err := cfg.LLM.Complete(callCtx, SystemPrompt, user)
	if err != nil {
		logger.Printf("turn %d: LLM call failed: %v", v.Turn, err)
		return acts[0], "fallback: LLM call failed"
	}
	d, perr := ParseDecision(reply, len(acts))
	if perr != nil {
		logger.Printf("turn %d: parse failed: %v", v.Turn, perr)
		return acts[0], "fallback: malformed LLM reply"
	}
	return acts[d.Choice], d.Reasoning
}

func toClientMsg(a engine.Action) protocol.WsClientMsg {
	kind := protocol.ActionKindMove
	if a.Kind == engine.ActionSwitch {
		kind = protocol.ActionKindSwitch
	}
	return protocol.WsClientMsg{Type: "action", Kind: kind, Index: a.Index}
}

func sideFromSlot(slot string) int {
	if slot == "p2" {
		return 1
	}
	return 0
}
