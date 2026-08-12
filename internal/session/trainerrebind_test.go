//go:build integration

package session_test

// RebindBattleTrainer is the write that makes the leaderboard describe who
// actually played. A live_pvp battle is created before its players arrive, so
// both trainer FKs are bound to names the creator invented ("Opponent"); the
// slot's real occupant only shows up later over the join WebSocket, and it is
// that occupant whose rating the result should move.
//
// The SQL is the whole mechanism, so it is verified against a real Postgres
// rather than mocked: the two failure modes that matter — writing the wrong
// slot's columns, and rewriting a battle whose rating is already settled — are
// both invisible to a unit test of the surrounding Go.

import (
	"context"
	"testing"
	"time"

	"pokearena/internal/store"

	"github.com/google/uuid"
)

// dialStore connects to Postgres alone. The shared dialInfra also dials Redis
// and RabbitMQ, which this test never touches — depending on them would make a
// pure SQL assertion fail for reasons that have nothing to do with the SQL.
func dialStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, err := store.New(ctx, env("DATABASE_URL", "postgres://pokearena:pokearena@localhost:5432/pokearena?sslmode=disable"))
	if err != nil {
		t.Fatalf("no Postgres: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func TestRebindBattleTrainer(t *testing.T) {
	st := dialStore(t)
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Distinct per-run names so repeated runs against a persistent database
	// don't collide on the trainers table's unique name constraint.
	run := uuid.NewString()[:8]
	creatorP1, creatorP2 := "Red-"+run, "Opponent-"+run
	joiner := "claude-haiku-" + run

	newBattle := func(t *testing.T) (string, string, string) {
		t.Helper()
		t1, err := st.UpsertTrainer(ctx, creatorP1)
		if err != nil {
			t.Fatalf("upsert p1: %v", err)
		}
		t2, err := st.UpsertTrainer(ctx, creatorP2)
		if err != nil {
			t.Fatalf("upsert p2: %v", err)
		}
		id := uuid.NewString()
		err = st.CreateBattle(ctx, store.Battle{
			ID: id, Mode: "live_pvp", Status: "open", Seed: 1,
			P1Trainer: t1, P2Trainer: t2, P1Name: creatorP1, P2Name: creatorP2,
			Winner: -1,
		})
		if err != nil {
			t.Fatalf("create battle: %v", err)
		}
		return id, t1, t2
	}

	t.Run("rebinds only the named slot", func(t *testing.T) {
		id, t1, _ := newBattle(t)

		joinerID, err := st.UpsertTrainer(ctx, joiner)
		if err != nil {
			t.Fatalf("upsert joiner: %v", err)
		}
		if err := st.RebindBattleTrainer(ctx, id, 1, joinerID, joiner); err != nil {
			t.Fatalf("rebind: %v", err)
		}

		b, err := st.GetBattle(ctx, id)
		if err != nil {
			t.Fatalf("get battle: %v", err)
		}
		if b.P2Trainer != joinerID {
			t.Errorf("p2_trainer = %q, want the joiner %q", b.P2Trainer, joinerID)
		}
		if b.P2Name != joiner {
			t.Errorf("p2_name = %q, want %q", b.P2Name, joiner)
		}
		// The opposite slot is the one a column-mapping slip would corrupt,
		// silently reassigning the *other* player's result.
		if b.P1Trainer != t1 {
			t.Errorf("p1_trainer = %q, want it untouched at %q", b.P1Trainer, t1)
		}
		if b.P1Name != creatorP1 {
			t.Errorf("p1_name = %q, want it untouched at %q", b.P1Name, creatorP1)
		}
	})

	t.Run("refuses to rebind a finished battle", func(t *testing.T) {
		id, _, t2 := newBattle(t)
		if err := st.CompleteBattle(ctx, id, 0, 12); err != nil {
			t.Fatalf("complete battle: %v", err)
		}

		joinerID, err := st.UpsertTrainer(ctx, joiner)
		if err != nil {
			t.Fatalf("upsert joiner: %v", err)
		}
		// A late or replayed join must not move a rating that was already
		// computed against the old trainer. The call is a silent no-op, not an
		// error — the join itself is still legitimate, it just cannot rename.
		if err := st.RebindBattleTrainer(ctx, id, 1, joinerID, joiner); err != nil {
			t.Fatalf("rebind after completion returned an error: %v", err)
		}

		b, err := st.GetBattle(ctx, id)
		if err != nil {
			t.Fatalf("get battle: %v", err)
		}
		if b.P2Trainer != t2 {
			t.Errorf("p2_trainer = %q, want the settled %q", b.P2Trainer, t2)
		}
		if b.P2Name != creatorP2 {
			t.Errorf("p2_name = %q, want the settled %q", b.P2Name, creatorP2)
		}
	})
}
