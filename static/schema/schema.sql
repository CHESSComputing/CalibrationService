-- FOXDEN calibration/configuration IOV schema (PostgreSQL)
--
-- Model:
--   tags     -- a named calibration/configuration set, e.g. "tracker-alignment"
--              (API-facing name: "label", see db.go/handlers.go)
--   payloads -- the actual JSONB constants blob, versioned/immutable once written
--   iovs     -- IOV rows: (tag, channel, since) -> payload
--
-- There is no "till". Each row is valid starting at "since" (a run number
-- or unix timestamp) and remains in effect until a later "since" (for the
-- same tag+channel) supersedes it. In other words: the calibration in
-- effect at any point in time is the active row with the greatest
-- since <= that point - the most recently started entry always wins. If
-- no "at" point is given, that's just the active row with the greatest
-- since overall, i.e. "the last one is the default until it's overwritten."
--
-- A partial UNIQUE index guarantees, at the database level, that no two
-- *active* rows for the same (tag, channel) share the same since - so
-- "the active row with the greatest since" is always unambiguous. This
-- replaces the old EXCLUDE USING gist overlap constraint, which is no
-- longer needed now that there's no "till" range to overlap.

CREATE TABLE IF NOT EXISTS tags (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS payloads (
    id         BIGSERIAL PRIMARY KEY,
    tag_id     BIGINT NOT NULL REFERENCES tags(id) ON DELETE RESTRICT,
    data       JSONB NOT NULL,
    checksum   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iovs (
    id          BIGSERIAL PRIMARY KEY,
    tag_id      BIGINT NOT NULL REFERENCES tags(id) ON DELETE RESTRICT,
    channel_id  BIGINT NOT NULL DEFAULT 0,     -- e.g. detector module/crate/channel; 0 = global
    payload_id  BIGINT NOT NULL REFERENCES payloads(id) ON DELETE RESTRICT,
    since       BIGINT NOT NULL,               -- run number or unix ts; valid from here until superseded
    revision    INT NOT NULL DEFAULT 1,        -- bumped on correction, old row deactivated
    is_active   BOOLEAN NOT NULL DEFAULT TRUE, -- false = superseded/retracted, kept for history
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    inserted_by TEXT,
    comment     TEXT
);

-- Core correctness guarantee: at most one ACTIVE row per (tag, channel,
-- since), so "the active row with the greatest since" is always
-- well-defined and there's no ambiguity about which entry is current.
CREATE UNIQUE INDEX IF NOT EXISTS idx_iovs_unique_active_since
    ON iovs (tag_id, channel_id, since) WHERE (is_active);

CREATE INDEX IF NOT EXISTS idx_iovs_tag_channel_since ON iovs (tag_id, channel_id, since DESC);
CREATE INDEX IF NOT EXISTS idx_payloads_tag ON payloads (tag_id);
