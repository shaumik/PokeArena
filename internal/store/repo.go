package store

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --- types returned by the repository ---

// Battle is a battle record.
type Battle struct {
	ID          string     `json:"id"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Seed        int64      `json:"seed"`
	P1Trainer   string     `json:"p1_trainer"`
	P2Trainer   string     `json:"p2_trainer"`
	P1Name      string     `json:"p1_name"`
	P2Name      string     `json:"p2_name"`
	P1Team      []int      `json:"p1_team"`
	P2Team      []int      `json:"p2_team"`
	Winner      int        `json:"winner"`
	TurnCount   int        `json:"turn_count"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Turn is one stored turn of a battle.
type Turn struct {
	TurnNo      int             `json:"turn_no"`
	Log         json.RawMessage `json:"log"`
	StateDigest json.RawMessage `json:"state_digest"`
}

// LeaderEntry is one row of the leaderboard.
type LeaderEntry struct {
	Name   string `json:"name"`
	Rating int    `json:"rating"`
	Wins   int    `json:"wins"`
	Losses int    `json:"losses"`
	Draws  int    `json:"draws"`
}

// RatingUpdate is a trainer's name and post-battle rating.
type RatingUpdate struct {
	Name   string
	Rating int
}

type rowScanner interface{ Scan(dest ...any) error }

// --- trainers ---

// UpsertTrainer ensures a trainer (and its ratings row) exists, returning its id.
func (s *Store) UpsertTrainer(ctx context.Context, name string) (string, error) {
	id := uuid.NewString()
	var got string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO trainers (id,name) VALUES ($1,$2)
		ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name
		RETURNING id`, id, name).Scan(&got)
	if err != nil {
		return "", err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO ratings (trainer_id) VALUES ($1) ON CONFLICT DO NOTHING`, got)
	return got, err
}

// --- battles ---

const battleColumns = `id,mode,status,seed,p1_trainer,p2_trainer,
	p1_name,p2_name,p1_team,p2_team,winner,turn_count,created_at,completed_at`

func scanBattle(row rowScanner) (Battle, error) {
	var b Battle
	var t1, t2 []byte
	if err := row.Scan(&b.ID, &b.Mode, &b.Status, &b.Seed,
		&b.P1Trainer, &b.P2Trainer, &b.P1Name, &b.P2Name, &t1, &t2,
		&b.Winner, &b.TurnCount, &b.CreatedAt, &b.CompletedAt); err != nil {
		return Battle{}, err
	}
	_ = json.Unmarshal(t1, &b.P1Team)
	_ = json.Unmarshal(t2, &b.P2Team)
	return b, nil
}

// CreateBattle inserts a new battle record.
func (s *Store) CreateBattle(ctx context.Context, b Battle) error {
	t1, _ := json.Marshal(b.P1Team)
	t2, _ := json.Marshal(b.P2Team)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO battles (id,mode,status,seed,p1_trainer,p2_trainer,p1_name,p2_name,p1_team,p2_team)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		b.ID, b.Mode, b.Status, b.Seed,
		b.P1Trainer, b.P2Trainer, b.P1Name, b.P2Name, string(t1), string(t2))
	return err
}

// GetBattle fetches one battle. Returns pgx.ErrNoRows if absent.
func (s *Store) GetBattle(ctx context.Context, id string) (Battle, error) {
	return scanBattle(s.pool.QueryRow(ctx, `SELECT `+battleColumns+` FROM battles WHERE id=$1`, id))
}

// ListBattles returns the most recent battles.
func (s *Store) ListBattles(ctx context.Context, limit int) ([]Battle, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+battleColumns+` FROM battles ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Battle
	for rows.Next() {
		b, err := scanBattle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListRunningLiveBattleIDs returns the ids of live battles currently in the
// "running" state — the candidates a battle-session failover scan checks for an
// expired ownership lease. Quick Sim battles are excluded: they have no live
// coordinator to take over.
func (s *Store) ListRunningLiveBattleIDs(ctx context.Context) ([]string, error) {
	return s.scanLiveBattleIDs(ctx,
		`SELECT id FROM battles WHERE status='running' AND mode IN ('live','live_pvp')`)
}

// ListStaleOpenLiveBattleIDs returns the ids of live battles still in the "open"
// (picker) state that were created before olderThan. The failover scan uses it
// to find picker rooms whose owner died before the room reached "running": the
// running scan can't see them, the picker state is never persisted (so they
// can't be resumed), and the owner's room-deadline timer died with it — so they
// would otherwise sit "open" forever. The olderThan cutoff excludes freshly
// created rooms a healthy owner simply hasn't claimed yet.
func (s *Store) ListStaleOpenLiveBattleIDs(ctx context.Context, olderThan time.Time) ([]string, error) {
	return s.scanLiveBattleIDs(ctx,
		`SELECT id FROM battles WHERE status='open' AND mode IN ('live','live_pvp') AND created_at < $1`,
		olderThan)
}

func (s *Store) scanLiveBattleIDs(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetBattleStatus updates a battle's lifecycle status.
func (s *Store) SetBattleStatus(ctx context.Context, id, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE battles SET status=$2 WHERE id=$1`, id, status)
	return err
}

// CompleteBattle marks a battle finished with its winner and turn count.
func (s *Store) CompleteBattle(ctx context.Context, id string, winner, turnCount int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE battles SET status='completed', winner=$2, turn_count=$3, completed_at=now()
		WHERE id=$1`, id, winner, turnCount)
	return err
}

// AppendTurn stores one turn. The (battle_id,turn_no) primary key makes a
// redelivered turn a harmless no-op.
func (s *Store) AppendTurn(ctx context.Context, battleID string, turnNo int, log, stateDigest []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO battle_turns (battle_id,turn_no,log,state_digest)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (battle_id,turn_no) DO NOTHING`,
		battleID, turnNo, string(log), string(stateDigest))
	return err
}

// GetTurns returns every stored turn of a battle in order.
func (s *Store) GetTurns(ctx context.Context, battleID string) ([]Turn, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT turn_no,log,state_digest FROM battle_turns WHERE battle_id=$1 ORDER BY turn_no`, battleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		var t Turn
		var lg, sd []byte
		if err := rows.Scan(&t.TurnNo, &lg, &sd); err != nil {
			return nil, err
		}
		t.Log, t.StateDigest = lg, sd
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- leaderboard ---

// ApplyResult updates Elo ratings for a finished battle. It is idempotent:
// guarded by rating_applied, a redelivered result changes nothing and returns
// no updates. winner is 0 (p1), 1 (p2), or 2 (draw).
func (s *Store) ApplyResult(ctx context.Context, battleID, t1, t2 string, winner int) ([]RatingUpdate, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once the tx commits; error is moot

	tag, err := tx.Exec(ctx,
		`INSERT INTO rating_applied (battle_id) VALUES ($1) ON CONFLICT DO NOTHING`, battleID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, tx.Commit(ctx) // already applied — no-op
	}

	r1, n1, err := readRating(ctx, tx, t1)
	if err != nil {
		return nil, err
	}
	r2, n2, err := readRating(ctx, tx, t2)
	if err != nil {
		return nil, err
	}

	s1 := 0.5
	switch winner {
	case 0:
		s1 = 1.0
	case 1:
		s1 = 0.0
	}
	const k = 32.0
	e1 := 1.0 / (1.0 + math.Pow(10, float64(r2-r1)/400.0))
	new1 := r1 + int(math.Round(k*(s1-e1)))
	new2 := r2 + int(math.Round(k*((1.0-s1)-(1.0-e1))))

	if err := writeRating(ctx, tx, t1, new1, winner == 0, winner == 1, winner == 2); err != nil {
		return nil, err
	}
	if err := writeRating(ctx, tx, t2, new2, winner == 1, winner == 0, winner == 2); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return []RatingUpdate{{Name: n1, Rating: new1}, {Name: n2, Rating: new2}}, nil
}

func readRating(ctx context.Context, tx pgx.Tx, trainerID string) (rating int, name string, err error) {
	err = tx.QueryRow(ctx, `
		SELECT r.rating, t.name FROM ratings r JOIN trainers t ON t.id=r.trainer_id
		WHERE r.trainer_id=$1`, trainerID).Scan(&rating, &name)
	return
}

func writeRating(ctx context.Context, tx pgx.Tx, trainerID string, rating int, win, loss, draw bool) error {
	_, err := tx.Exec(ctx, `
		UPDATE ratings SET
		  rating=$2,
		  wins=wins+$3, losses=losses+$4, draws=draws+$5,
		  updated_at=now()
		WHERE trainer_id=$1`,
		trainerID, rating, b2i(win), b2i(loss), b2i(draw))
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Leaderboard returns the top trainers by rating.
func (s *Store) Leaderboard(ctx context.Context, limit int) ([]LeaderEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.name, r.rating, r.wins, r.losses, r.draws
		FROM ratings r JOIN trainers t ON t.id=r.trainer_id
		ORDER BY r.rating DESC, r.wins DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaderEntry
	for rows.Next() {
		var e LeaderEntry
		if err := rows.Scan(&e.Name, &e.Rating, &e.Wins, &e.Losses, &e.Draws); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
