# community-backend

Collaborative tile-drawing web app backend. Go 1.26, PostgreSQL 17, MinIO/S3.

See [CONTEXT.md](CONTEXT.md) for full architecture and module documentation.

## Quick Start

```bash
make env        # creates .env from .env.example (skips if exists)
make dev        # docker compose up + go run ./cmd/api
```

This starts PostgreSQL on `:5432`, MinIO on `:9000` (console `:9001`), and the API on `:8080`.
Migrations run automatically on startup.

## Seeding a Daily Image

The game needs a daily image and an active period before tiles appear in the frontend. Use the debug endpoints (available in `ENV=local` and `ENV=dev` only).

### Step 1 — Upload an image to MinIO

You can upload via the MinIO CLI inside the container:

```bash
docker cp /path/to/your/image.jpg docker-minio-1:/tmp/image.jpg
docker exec docker-minio-1 mc alias set local http://localhost:9000 community community
docker exec docker-minio-1 mc cp /tmp/image.jpg local/community-assets/photos/today.jpg
```

Or use the MinIO web console at `http://localhost:9001` (login: `community` / `community`).

### Step 2 — Set the active daily image

Tell the backend which storage key to use and the image dimensions:

```bash
curl -X POST http://localhost:8080/debug/daily-image \
  -H 'Content-Type: application/json' \
  -d '{"storage_key": "photos/today.jpg", "width": 1920, "height": 1080}'
```

The `date` field is optional and defaults to today. To set a specific date:

```bash
curl -X POST http://localhost:8080/debug/daily-image \
  -H 'Content-Type: application/json' \
  -d '{"storage_key": "photos/today.jpg", "width": 1920, "height": 1080, "date": "2026-04-18"}'
```

### Step 3 — Create a period (generates the tile grid)

```bash
curl -X POST http://localhost:8080/debug/period \
  -H 'Content-Type: application/json' \
  -d '{}'
```

This creates an active period linked to the current daily image and generates a 3x3 tile grid (phase 1). Only one active period can exist at a time.

### Verify

```bash
curl http://localhost:8080/api/v1/periods/current
```

Returns the active period with the image (presigned URL) and all tiles with their statuses.

## How It Works

1. **Daily image** is uploaded to MinIO/S3 and registered via the debug endpoint. The backend stores only the storage key — presigned download URLs are generated on the fly.

2. **Period** represents one game session tied to a daily image. When created, it generates tiles based on the grid config for the current phase (phase 1 = 3x3, phase 2 = 6x6, phase 3 = 10x10).

3. **Tiles** start as `free`. When a user claims one, it becomes `locked` for 30 minutes. If the user uploads their drawing, it becomes `drawn`. If the claim expires, the tile returns to `free`.

4. **Claiming** uses PostgreSQL row-level locking (`SELECT ... FOR UPDATE` inside PL/pgSQL functions) to handle concurrent users safely. Only one user can claim a tile at a time — concurrent attempts get a 409 conflict.

5. **Submission** — the user gets a presigned PUT URL, uploads their drawing directly to MinIO/S3, then confirms the submission. The tile is marked `drawn` and the claim is released, all atomically.

6. **Expired claim sweep** — a background goroutine runs every 30 seconds, releasing any claims past their TTL and setting those tiles back to `free`.

7. **Storage cleanup sweep** — a separate background goroutine runs every 30 minutes (with a 5-minute startup delay) and prunes per-tile JPGs from R2/MinIO once their phase mosaic has been composed. The DB row stays and is stamped with `storage_cleaned_at`; only the orphaned object disappears. See [Object storage layout](#object-storage-layout) below.

## Object storage layout

The bucket (`community-assets` locally, configurable in prod) is structured by purpose. Every key here is a Postgres-tracked storage_key — nothing in the bucket is ever discovered by listing.

| Prefix | Written by | Referenced from | Lifetime |
|---|---|---|---|
| `photos/<basename>` | Admin upload (`POST /debug/daily-image`) | `daily_images.storage_key` | Forever — the original picture is shown alongside the mosaic in the archive detail page. |
| `schedule/<date>-<basename>` | Admin upload (Schedule grid in admin panel) | `daily_image_schedule.storage_key`, then `daily_images.storage_key` once promoted at midnight Cph | Forever — promotion just copies the key reference into `daily_images`; the object itself is reused. |
| `staging/<tile_uuid>-<session_id>.jpg` | Phone QR upload (presigned PUT) | Not tracked in DB — picked up by the laptop via `GET /uploads/staged` | **Pruned** immediately on successful submit; orphaned files (laptop abandoned the flow) stay until a future sweeper pass. |
| `tiles/<tile_uuid>.jpg` | User submission (presigned PUT) | `submissions.storage_key` | **Pruned** once a phase mosaic exists for the tile's `(period_id, phase)`. The DB row is kept and stamped via `storage_cleaned_at`; the object is deleted by the storage sweeper. |
| `archive/<period_uuid>/phase-<N>.jpg` | Backend `ComposeFinalImage` | `period_mosaics.storage_key`, `periods.final_image_key` | Forever — this is what the archive renders. |
| `archive/<period_uuid>/phase-<N>-thumb.jpg` | Backend `ComposeFinalImage` (thumbnail variant) | Reconstructed from the full key via `ThumbnailKey()` | Forever — the calendar list presigns the thumb to keep payloads light. |

### Why tiles get pruned

The mosaic produced by `ComposeFinalImage` is a single JPEG containing every drawing for that phase. Once it exists, the per-tile JPGs are dead weight: the archive only ever shows the mosaic. At phase 1 (3×3) that's 9 stale objects per period; at phase 6 (28×28) it's 784. Without cleanup, R2 storage grows roughly with `Σ phases² · periods`.

The sweeper:

1. Selects up to `submission.CleanupBatchSize` (500) submissions where `period_mosaics` has a row for `(period_id, phase)` and `storage_cleaned_at IS NULL`.
2. Calls `Storage.DeleteObjects` (a single S3/R2 DeleteObjects round-trip per 1000-key batch).
3. Stamps the cleaned rows so the next tick skips them.

Failure mode: if a delete fails mid-batch, the un-removed keys simply stay un-stamped and get retried next tick. The sweep is idempotent.

### What never gets cleaned automatically

- `photos/`, `schedule/`, and `archive/` keys live for the lifetime of the bucket. Old completed periods are still browsable in the archive, so removing them would break the historical view.
- If you need to drop a period entirely (e.g. a moderation issue), the debug `DELETE /debug/period?game_type=…` endpoint removes the DB rows but **does not** delete R2 objects — that's a deliberate split so we don't blow away references that the archive still expects. Pruning the storage in that case is currently a manual admin task.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | DB + storage health check |
| GET | `/api/v1/periods/current` | Active period with image and tile grid |
| GET | `/api/v1/periods/archive` | Past completed periods |
| GET | `/api/v1/periods/{id}` | Single period detail |
| POST | `/api/v1/tiles/{id}/claim` | Claim a tile (body: `nickname`, `session_id`) |
| DELETE | `/api/v1/tiles/{id}/claim` | Release a claim (body: `session_id`) |
| POST | `/api/v1/uploads/presign` | Get presigned upload URL (body: `tile_id`, `session_id`) |
| POST | `/api/v1/uploads/stage-presign` | Phone-side presigned PUT for the raw camera photo (body: `tile_id`, `session_id`) |
| GET | `/api/v1/uploads/staged?tile=…&session=…` | Laptop polls this until the phone has uploaded; returns `download_url` or 404 |
| POST | `/api/v1/tiles/{id}/submit` | Confirm drawing submission |
| POST | `/api/v1/feedback` | Submit feedback (no auth required) |

### Debug-only (non-prod)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/debug/storage/upload-url?key=<key>` | Presigned PUT URL for any storage key |
| POST | `/debug/daily-image` | Set active daily image |
| POST | `/debug/period` | Create active period from current daily image |
