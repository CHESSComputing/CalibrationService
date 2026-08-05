# FOXDEN Calibration / Configuration IOV Service

Stores time-dependent detector configurations and calibrations with
well-defined intervals of validity (IOV), in the spirit of CLEO3 constants
or CMS conditions data.

## DB Backend

We use a PostgreSQL backend for storing calibration constants.

## Running it

```bash
# pull the image
docker pull postgres:17

# run Postgres
docker run -d \
  --name foxden-calib-pg \
  -e POSTGRES_USER=foxden \
  -e POSTGRES_PASSWORD=foxden \
  -e POSTGRES_DB=foxden_calib \
  -p 5432:5432 \
  -v foxden-calib-pg-data:/var/lib/postgresql/data \
  postgres:17

# load the schema
cat static/schema/schema.sql | docker exec -i foxden-calib-pg psql -U foxden -d foxden_calib

# (existing database only: also run the till-removal migration once)
# cat static/schema/migration_remove_till.sql | docker exec -i foxden-calib-pg psql -U foxden -d foxden_calib

# fetch deps and run the service
go mod tidy
CALIB_DB_DSN="postgres://foxden:foxden@localhost:5432/foxden_calib?sslmode=disable" \
CALIB_ADDR=":8399" \
go run .
```

Useful Docker commands:
```bash
docker exec -it foxden-calib-pg psql -U foxden -d foxden_calib
docker logs foxden-calib-pg
docker stop foxden-calib-pg
docker start foxden-calib-pg
docker rm -f foxden-calib-pg
```

## Interval of validity (IOV)

An IOV has only `since` — no `till`. A row is valid starting at `since`
and stays in effect **until a later `since` (for the same label+channel)
supersedes it**. There's no explicit end date to set or maintain:

```
channel's timeline:  since=100000 ────────► since=105000 ────────►
                      (payload A)            (payload B)   ... still current
```

- Payload A is "valid" for any `at >= 100000` up until `at >= 105000`, at
  which point payload B takes over.
- Whatever has the **greatest active `since`** is always the current
  default — exactly "the last one is used until it's overwritten."

## API reference

| Method | Path              | Action                                        |
|--------|-------------------|------------------------------------------------|
| POST   | `""`              | Create (label in body)                         |
| GET    | `/label/*label`   | List active IOVs for a label                    |
| GET    | `/valid/*label`   | Resolve constants valid at a run/time (or the current default if `at` is omitted) |
| GET    | `/history/*label` | Full revision history                           |
| PUT    | `/correct/*label` | Overwrite an existing `since` point             |
| DELETE | `/iov/:id`        | Retract a single IOV                            |
| DELETE | `/label/*label`   | Retract every active IOV for a label            |

`{label}` is the full hierarchical path, e.g.
`/valid/3b/btr123/cycle123/sampleName?at=102000`. Note that List and
"Delete by label" share the same path — they're distinguished by HTTP
method (`GET` vs `DELETE`), not by URL.

Add `Content-Type: application/x-yaml` (request) and/or `Accept:
application/x-yaml` / `?format=yaml` (response) to any of the above to use
YAML instead of JSON.

## Example: creating a calibration from `calib.yaml`

Here is an example of using CHAP calibration detector constants:

```yaml
# request.yaml
label: /3b/btr123/cycle123/sampleName
channel_id: 0
since: 100000
inserted_by: alice
comment: initial eta mapping
data:
  detectors:
  - id: 0
    attrs:
      eta: -180.0
  - id: 1
    attrs:
      eta: -171.8181818181818
  # ... (rest of calib.yaml's detectors list)
```

```bash
curl -X POST http://localhost:8399 \
  -H "Content-Type: application/x-yaml" \
  --data-binary @request.yaml
```

The response comes back as YAML too (Content-Type mirroring), e.g.:

```yaml
id: 1
label_id: 1
label: /3b/btr123/cycle123/sampleName
channel_id: 0
payload_id: 1
since: 100000
revision: 1
is_active: true
inserted_at: 2026-07-31T12:00:00Z
inserted_by: alice
comment: initial eta mapping
```

The same request works as JSON with `Content-Type: application/json` and a
JSON body — the envelope fields and nested `data` structure are identical,
just serialized differently.

## Endpoint examples

### List active IOVs for a label
```bash
curl "http://localhost:8399/label/3b/btr123/cycle123/sampleName?channel_id=0"
```

### Valid-at lookup

`at` is optional: omit it to get the current default (the active IOV with
the greatest `since`); pass it to see what was in effect at a given
run/time.

```bash
# what's the calibration in effect right now?
curl "http://localhost:8399/valid/3b/btr123/cycle123/sampleName"

# what was it at run 102000? (as YAML)
curl "http://localhost:8399/valid/3b/btr123/cycle123/sampleName?at=102000&format=yaml"
```

### History
```bash
curl "http://localhost:8399/history/3b/btr123/cycle123/sampleName?channel_id=0"
```

### Adding a new since point
```bash
curl -X POST http://localhost:8399 \
  -H "Content-Type: application/json" \
  -d '{"label": "/3b/btr123/cycle123/sampleName", "channel_id": 0,
       "since": 105000, "data": {"dx": 0.011}, "inserted_by": "bob"}'
```

Adding a `since` that's greater than any existing active one automatically
makes it the new default — no separate step needed to "close" the previous
entry. If an *active* row already exists at that exact
`(label, channel, since)`, this returns `409 Conflict` (`ErrDuplicateSince`);
use `PUT /correct/{label}` instead to overwrite it.

### Correcting an existing since point

`PUT /correct/{label}` overwrites the **exact** `since` you pass. It
requires an active row already at that `since` (`404` otherwise, with a
message that there's nothing there to correct), deactivates it, and
inserts a new payload/IOV at the same `since` with `revision + 1`:

```bash
curl -X PUT "http://localhost:8399/correct/3b/btr123/cycle123/sampleName?channel_id=0" \
  -H "Content-Type: application/json" \
  -d '{"since": 100000, "data": {"dx": 0.0112}, "inserted_by": "bob",
       "comment": "fixed a typo in the original entry"}'
```

Use `POST` to add a new time period; use `PUT /correct` to fix a mistake
in an existing one.

### Deleting

`{id}` in `DELETE /iov/{id}` is the primary key of a single **IOV row** —
one specific `(label, channel, since, revision)` entry — not the label and
not the payload. You get it back in the `id` field of every
create/list/valid/history/correct response. This retracts exactly that one
entry:

```bash
curl -X DELETE http://localhost:8399/iov/17
# -> 204 No Content
```

To retract *every* active entry for a label in one call (optionally scoped
to a channel), use the bulk endpoint instead:

```bash
curl -X DELETE "http://localhost:8399/label/3b/btr123/cycle123/sampleName?channel_id=0"
```
```json
{"label": "/3b/btr123/cycle123/sampleName", "channel_id": 0, "deactivated": 3}
```
