-- PokéArena schema. Idempotent: safe to run on every boot.
-- Postgres is the system of record for *transactional* state (trainers,
-- ratings, battles, turns). Reference data (species, moves, typechart) lives
-- in committed JSON under data/ and is loaded into memory by each service —
-- it does not belong in the database. Drop any legacy tables that used to
-- hold it.
DROP TABLE IF EXISTS species_moves;
DROP TABLE IF EXISTS species;
DROP TABLE IF EXISTS moves;

CREATE TABLE IF NOT EXISTS trainers (
    id         UUID PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ratings (
    trainer_id UUID PRIMARY KEY REFERENCES trainers(id) ON DELETE CASCADE,
    rating     INT NOT NULL DEFAULT 1000,
    wins       INT NOT NULL DEFAULT 0,
    losses     INT NOT NULL DEFAULT 0,
    draws      INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS battles (
    id            UUID PRIMARY KEY,
    mode          TEXT NOT NULL,                 -- quicksim | live | live_pvp
    status        TEXT NOT NULL,                 -- pending | running | completed
    seed          BIGINT NOT NULL,
    p1_trainer    UUID REFERENCES trainers(id),
    p2_trainer    UUID REFERENCES trainers(id),
    p1_name       TEXT NOT NULL,
    p2_name       TEXT NOT NULL,
    p1_team       JSONB NOT NULL,                -- list of Pokédex numbers
    p2_team       JSONB NOT NULL,
    winner        INT  NOT NULL DEFAULT -1,      -- -1 ongoing, 0/1 side, 2 draw
    turn_count    INT  NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_battles_status  ON battles(status);
CREATE INDEX IF NOT EXISTS idx_battles_created ON battles(created_at DESC);
-- Difficulty is no longer a concept (the AI is always the expectimax agent).
-- Drop the legacy column on existing databases; new ones never create it.
ALTER TABLE battles DROP COLUMN IF EXISTS ai_difficulty;

CREATE TABLE IF NOT EXISTS battle_turns (
    battle_id    UUID NOT NULL REFERENCES battles(id) ON DELETE CASCADE,
    turn_no      INT  NOT NULL,
    log          JSONB NOT NULL,                -- the turn's log lines
    state_digest JSONB NOT NULL,                -- full battle state after the turn
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (battle_id, turn_no)            -- idempotent: re-applying a turn is a no-op
);

-- Guards the leaderboard worker against double-applying a battle result
-- when an event is redelivered.
CREATE TABLE IF NOT EXISTS rating_applied (
    battle_id  UUID PRIMARY KEY REFERENCES battles(id) ON DELETE CASCADE,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
