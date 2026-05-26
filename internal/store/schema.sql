-- PokéArena schema. Idempotent: safe to run on every boot.
-- PostgreSQL is the system of record; Redis holds only derived/ephemeral state.

CREATE TABLE IF NOT EXISTS species (
    dex_no       INT  PRIMARY KEY,
    name         TEXT NOT NULL,
    type1        TEXT NOT NULL,
    type2        TEXT NOT NULL DEFAULT '',
    base_hp      INT  NOT NULL,
    base_atk     INT  NOT NULL,
    base_def     INT  NOT NULL,
    base_spatk   INT  NOT NULL,
    base_spdef   INT  NOT NULL,
    base_speed   INT  NOT NULL,
    data_version TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS moves (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    type           TEXT NOT NULL,
    category       TEXT NOT NULL,
    power          INT  NOT NULL,
    accuracy       INT  NOT NULL,
    pp             INT  NOT NULL,
    priority       INT  NOT NULL,
    target         TEXT NOT NULL DEFAULT '',
    flags          JSONB,
    primary_effect JSONB,
    self_effect    JSONB,
    secondaries    JSONB
);
-- Forward-migrate older databases that pre-date the schema change. Postgres
-- 9.6+ supports ADD/DROP COLUMN IF [NOT] EXISTS, so these are idempotent.
ALTER TABLE moves DROP   COLUMN IF EXISTS effect;
ALTER TABLE moves ADD    COLUMN IF NOT EXISTS target         TEXT NOT NULL DEFAULT '';
ALTER TABLE moves ADD    COLUMN IF NOT EXISTS flags          JSONB;
ALTER TABLE moves ADD    COLUMN IF NOT EXISTS primary_effect JSONB;
ALTER TABLE moves ADD    COLUMN IF NOT EXISTS self_effect    JSONB;
ALTER TABLE moves ADD    COLUMN IF NOT EXISTS secondaries    JSONB;

CREATE TABLE IF NOT EXISTS species_moves (
    species_dex INT  NOT NULL REFERENCES species(dex_no) ON DELETE CASCADE,
    move_id     TEXT NOT NULL REFERENCES moves(id)       ON DELETE CASCADE,
    slot        INT  NOT NULL,
    PRIMARY KEY (species_dex, slot)
);

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
    ai_difficulty TEXT NOT NULL DEFAULT '',
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
