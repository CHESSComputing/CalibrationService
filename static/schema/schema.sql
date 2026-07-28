-- FOXDEN calibration/configuration IOV schema (PostgreSQL)
--
-- Model:
--   tags     -- a named calibration/configuration set, e.g. "tracker-alignment"
--   payloads -- the actual JSONB constants blob, versioned/immutable once written
--   iovs     -- interval-of-validity rows: (tag, channel, since, till) -> payload
--
-- "since"/"till" are plain BIGINT so you can use either run numbers or unix
-- timestamps (seconds) depending on the detector subsystem's convention.
-- "till" is exclusive, i.e. validity = [since, till).
--
-- The EXCLUDE USING gist constraint on iovs guarantees, at the database level,
-- that no two *active* IOVs for the same (tag, channel) can have overlapping
-- validity ranges. This is the key correctness property CLEO3/CMS-style
-- conditions databases rely on.

CREATE EXTENSION IF NOT EXISTS btree_gist;

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
    since       BIGINT NOT NULL,               -- inclusive: run number or unix ts
    till        BIGINT NOT NULL,               -- exclusive
    validity    INT8RANGE GENERATED ALWAYS AS (int8range(since, till, '[)')) STORED,
    revision    INT NOT NULL DEFAULT 1,        -- bumped on correction, old row deactivated
    is_active   BOOLEAN NOT NULL DEFAULT TRUE, -- false = superseded, kept for history/reproducibility
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    inserted_by TEXT,
    comment     TEXT,

    CONSTRAINT since_before_till CHECK (since < till),

    -- Core correctness guarantee: no two ACTIVE IOVs for the same tag+channel
    -- may have overlapping validity ranges.
    CONSTRAINT no_overlap EXCLUDE USING gist (
        tag_id     WITH =,
        channel_id WITH =,
        validity   WITH &&
    ) WHERE (is_active)
);

CREATE INDEX IF NOT EXISTS idx_iovs_tag_channel ON iovs (tag_id, channel_id);
CREATE INDEX IF NOT EXISTS idx_payloads_tag ON payloads (tag_id);
