# FOXDEN Calibration / Configuration IOV Service

Stores time-dependent detector configurations and calibrations with
well-defined intervals of validity (IOV), in the spirit of CLEO3 constants
or CMS conditions data.

## DB Backend
we use PostgreSQL DB backend for the following reasons:

- **Range types + exclusion constraints.** `iovs.validity` is an `int8range`
  generated column, guarded by a `GiST` `EXCLUDE` constraint
  (`tag_id`, `channel_id`, `validity &&`). The database itself refuses to let
  two *active* IOVs for the same tag+channel overlap — this is not
  re-implemented in Go, so there's no race window between "check" and
  "insert."
- **ACID transactions.** Every write (new payload + new IOV, or a
  correction that deactivates old IOVs and inserts a new one) happens in a
  single transaction, so readers never see a payload without its IOV or a
  half-applied correction.
- **JSONB payloads.** The calibration/config blob itself (`payloads.data`)
  is schema-flexible JSONB, so different detector subsystems can store
  differently-shaped constants without a schema migration, while the IOV
  bookkeeping around it stays fully relational and indexable.

`since`/`till` are plain `BIGINT`, so a tag can use either run numbers or
Unix timestamps depending on subsystem convention. Validity is half-open:
`[since, till)`.

Corrections never delete data: superseding an IOV sets `is_active = false`
on the old row and inserts a new payload + IOV with an incremented
`revision`. `GET /calibrations/:tag/history` returns the full series, so a
query like "what did we believe the tracker alignment was for run 12345 as
of last Tuesday" stays answerable.

## Running it

```bash
# 1. start Postgres (any 14+ works; needs btree_gist, created by schema.sql)
docker run -d --name foxden-calib-pg -e POSTGRES_USER=foxden \
  -e POSTGRES_PASSWORD=foxden -e POSTGRES_DB=foxden_calib \
  -p 5432:5432 postgres:16

# 2. load the schema
psql "postgres://foxden:foxden@localhost:5432/foxden_calib" -f schema.sql

# 3. fetch deps and run
go mod tidy
CALIB_DB_DSN="postgres://foxden:foxden@localhost:5432/foxden_calib?sslmode=disable" \
CALIB_ADDR=":8888" \
go run .
```

## API

### Create a calibration
```
POST /calibrations
{
  "tag": "tracker-alignment",
  "channel_id": 12,
  "since": 100000,
  "till":  105000,
  "data": {"dx": 0.012, "dy": -0.004, "dz": 0.0},
  "inserted_by": "alice",
  "comment": "initial alignment from cosmic run"
}
```
`201 Created` with the new IOV. `409 Conflict` if it overlaps an existing
active IOV for that tag+channel.

### Resolve the calibration valid at a given run/time
```
GET /calibrations/tracker-alignment/valid?at=102000&channel_id=12
```
Returns the IOV plus its payload, or `404` if nothing is active there.

### List active IOVs for a tag
```
GET /calibrations/tracker-alignment?channel_id=12
```

### Full revision history (including superseded IOVs)
```
GET /calibrations/tracker-alignment/history?channel_id=12
```

### Correct/replace a range
```
PUT /calibrations/tracker-alignment/correct?channel_id=12
{
  "since": 100000,
  "till":  105000,
  "data": {"dx": 0.011, "dy": -0.0038, "dz": 0.0},
  "inserted_by": "bob",
  "comment": "reprocessed with better cosmic sample"
}
```
Deactivates any active IOV(s) overlapping `[since, till)` for that
tag+channel and inserts a new payload+IOV at `revision+1`.

### Retract a single IOV
```
DELETE /calibrations/iov/{id}
```
Soft-deletes (`is_active = false`); the row and its payload remain for
history.

## Notes / extension points

- `channel_id` defaults to `0` for tags that don't need per-channel
  granularity (e.g. a single global run-conditions tag).
- Add auth/middleware (FOXDEN's existing token/scope layer) in `main.go`
  before `NewHandler(store).RegisterRoutes(r)`.
- If you need range-overlap listing (e.g. "all IOVs touching runs
  100000-110000") that's a straightforward addition to `db.go` using
  `validity && int8range($1, $2, '[)')`.
