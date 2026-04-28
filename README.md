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
| POST | `/api/v1/tiles/{id}/submit` | Confirm drawing submission |
| POST | `/api/v1/feedback` | Submit feedback (no auth required) |

### Debug-only (non-prod)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/debug/storage/upload-url?key=<key>` | Presigned PUT URL for any storage key |
| POST | `/debug/daily-image` | Set active daily image |
| POST | `/debug/period` | Create active period from current daily image |
