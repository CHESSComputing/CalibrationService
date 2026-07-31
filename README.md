# FOXDEN Calibration / Configuration IOV Service

Stores time-dependent detector configurations and calibrations with
well-defined intervals of validity (IOV), in the spirit of CLEO3 constants
or CMS conditions data.

## DB Backend
we use PostgreSQL DB backend for storing calibration constants

## Running it

Install and manage your PostgresDB, e.g.
```bash
# pull out docker image
docker pull postgres:17

# run pdb
docker run -d \
  --name postgres17 \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=testdb \
  -p 5432:5432 \
  -v postgres-data:/var/lib/postgresql/data \
  postgres:17

# access db
docker exec -it postgres17 psql -U postgres -d testdb

# create schema
cat static/schema/schema.sql | docker exec -i postgres17 psql -U postgres -d testdb

# usefull commands
docker logs postgres17
docker stop postgres17
docker start postgres17
docker rm -f postgres17
```

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
CALIB_ADDR=":8399" \
go run .
```

## API

| Action              | URI Path           |
|---------------------|--------------------|
| Create              | `""`               |
| List                | `/label/*label`    |
| Valid-at lookup     | `/valid/*label`    |
| History             | `/history/*label`  |
| Correct             | `/correct/*label`  |
| Delete IOV          | `/iov/:id`         |
| Delete by label     | `/label/*label`    |


where `{label}` is the full hierarchical path, e.g.
`/calibrations/valid/3b/btr123/cycle123/sampleName?at=102000`.



### Example: creating a calibration from `calib.yaml`
Here is an example of using CHAP calibration detector constants:

```yaml
# request.yaml
label: /3b/btr123/cycle123/sampleName
channel_id: 0
since: 100000
till: 105000
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
till: 105000
revision: 1
is_active: true
inserted_at: 2026-07-31T12:00:00Z
inserted_by: alice
comment: initial eta mapping
```

The same request works as JSON with `Content-Type: application/json` and a
JSON body — the envelope fields and nested `data` structure are identical,
just serialized differently.

## Server end-point reference

```
POST                             create (label in body)
GET    /label/{label}?channel_id= list active IOVs for a label
GET    /valid/{label}?at=&channel_id=  resolve constants valid at a run/time
GET    /history/{label}?channel_id=    full revision history
PUT    /correct/{label}?channel_id=    supersede overlapping IOV(s)
DELETE /iov/{id}                  retract a single IOV
DELETE /label/{label}?channel_id= retract every active IOV for a label
```

`{label}` is the full hierarchical path, e.g.
`/valid/3b/btr123/cycle123/sampleName?at=102000`.

Add `Content-Type: application/x-yaml` (request) and/or `Accept:
application/x-yaml` / `?format=yaml` (response) to any of the above to use
YAML instead of JSON.

### Resolve the calibration valid at a given run/time (YAML response)
```
GET /valid/3b/btr123/cycle123/sampleName?at=102000&format=yaml
```

### List active IOVs for a label (JSON, default)
```
GET /label/3b/btr123/cycle123/sampleName?channel_id=0
```

### Correct/replace a range
```
PUT /correct/3b/btr123/cycle123/sampleName?channel_id=0
Content-Type: application/json
{
  "since": 100000,
  "till":  105000,
  "data": {"detectors": [...]},
  "inserted_by": "bob",
  "comment": "reprocessed with better cosmic sample"
}
```

### Deleting

`{id}` in `DELETE /calibrations/iov/{id}` is the primary key of a single
**IOV row** — one specific `(label, channel, since, till, revision)`
validity interval — not the label and not the payload. You get it back in
the `id` field of every create/list/valid/history/correct response. This
retracts exactly that one interval:

```bash
curl -X DELETE http://localhost:8399/iov/17
# -> 204 No Content
```

To retract *every* active interval for a label in one call (optionally
scoped to a channel), use the new bulk endpoint instead:

```bash
curl -X DELETE "http://localhost:8399/label/3b/btr123/cycle123/sampleName?channel_id=0"
```
```json
{"label": "/3b/btr123/cycle123/sampleName", "channel_id": 0, "deactivated": 3}
```
