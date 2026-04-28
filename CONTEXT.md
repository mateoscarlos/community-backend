# COMMUNITY BACKEND — FULL CONTEXT

This file is the single source of truth for any agent or developer working on this repository.
Read it entirely before making any changes.

---

## 1. WHAT IS COMMUNITY

Community is a **mobile-first collaborative web platform** where users collectively create art
by drawing tiles of a shared image or prompt — every day.

A new challenge is published ideally every 24 hours, but this period can vary. The challenge image or prompt is divided into a
grid of tiles. Each user claims one tile, draws it (on paper or digitally), and uploads a photo
of their drawing. As tiles are submitted, the drawn versions replace the original — building a
live composite that is half-reference, half-community art. When all tiles are drawn the grid
subdivides into smaller tiles and the cycle repeats. After the period of time set (24h/1w/...) the image is retired to the Archive.

**Visual reference for design:** Wordle (wordle.nytimes.com) — study it before starting.

---

## 2. GAMES

Community supports **multiple game types** sharing the same underlying tile/claim/submission
mechanics. Each game type has its own daily challenge.

### Game 1 — Photo of the Day (implemented)
A curated reference photo is published. It is divided into tiles. Users draw their assigned tile
to recreate that section of the photo. The goal is a collaborative hand-drawn version of the image.

### Game 2 — Prompt of the Day (planned)
A text prompt is published (e.g. "draw a forest at sunset"). All tiles start empty — no
reference image. Each user draws whatever they imagine for that prompt. The result is a
collage of independent interpretations stitched together.

**Shared mechanics across all games:**
- Same tile states: FREE → LOCKED → DRAWN
- Same claim flow: user selects tile → locked immediately → draws → uploads
- Same grid progression logic
- Same 24h cycle

**Open decisions (TBD):**
- Grid sizes and progression sequence (e.g. 4 → 12 → ?) — stored in DB, not hardcoded
- Whether multiple games run simultaneously or one per day
- Fusion/composite rendering: server-side or frontend overlay

---

## 3. TILE STATES

| State | Meaning |
|---|---|
| FREE | Available to claim |
| LOCKED | Claimed by a user, being drawn |
| DRAWN | Submitted and visible in the composite |

---

## 4. USERS

| Type | Access |
|---|---|
| Guest (no account) | Pick tile · enter nickname · draw & upload · leave feedback |
| Registered | Same + persistent profile · contribution history · auto-filled nickname |

**Sign-up is never required for the core action.** Accounts are optional.
Sign-up options: email or social login (providers TBD).

---

## 5. UPLOAD FLOW

Users **draw their tile** (on paper or digitally), then upload a photo of the drawing.

**On desktop:**
- A QR code is shown on screen
- User scans it with their phone
- Phone opens camera directly in the browser
- User photographs their drawing and uploads

**On mobile:**
- Camera opens directly from the web page

**After capture (both paths):**
- User can crop, resize, zoom in/out to align their drawing with surrounding tiles
- Then confirms and submits

The backend provides a **presigned PUT URL** — the frontend uploads directly to MinIO/S3.
The backend never handles raw image bytes.

---

## 6. PAGES (from product doc)

### Homepage — primary page, contains the full experience
- Current composite image (photo tiles + drawn tiles merged)
- Progress counter: e.g. "143 / 1,000 tiles drawn"
- Single action: select a tile → enter drawing flow
- Live count of users online (post-launch)

### Tile Drawing View — modal or dedicated page
- Cropped reference of the user's tile
- Mini-map: position of the tile in the full image
- Upload field: photo of paper drawing or digital file (JPG/PNG)
- Nickname input (if not already set)

### Archive
- Gallery of every completed image, ordered by date
- Each entry: date · contributor count · grid size reached · votes
- Clicking an entry shows the full phase evolution: Phase 1 → Phase 2 → … → Final

### Feedback
- Plain text form. No login required. Optional contact field.

### Rankings (post-launch)
- Most active users (by tiles submitted)
- Most voted artworks of all time

---

## 7. REPOSITORIES

| Repo | Purpose |
|---|---|
| `community-backend` | Go API — **this repository** |
| `community-frontend` | React app (separate repo) |

---

## 8. BACKEND STACK

| Concern | Choice |
|---|---|
| Language | Go 1.26 |
| DI framework | Uber Fx |
| Router | chi v5 |
| DB | PostgreSQL 17 |
| Migrations | Goose v3 (embedded FS, runs automatically on startup) |
| Query layer | SQLC + `database/sql` + `lib/pq` |
| Object storage | MinIO (local) / AWS S3 (dev + prod) |
| Logging | zerolog → stdout + optional Grafana Cloud Loki HTTP push |
| Config | `envconfig` (env vars only, fail-fast, no Viper) |
| `.env` loading | `godotenv.Load()` in `init()` — silently ignored in production |
| Hosting | Railway (dev + prod environments) |
| CI/CD | GitHub Actions |

**Module path:** `github.com/community-app/community-backend`

---

## 9. ARCHITECTURE

**Style:** Modular monolith, DDD-lite. Infrastructure behind interfaces.
Ready to evolve into microservices — each module is independently extractable.

**Layer rules (strictly enforced):**
- `cmd/api/main.go` — Fx wiring only, zero business logic
- `domain.go` — pure domain types, no DB tags, no JSON tags, no infra imports
- `service.go` — application logic, depends only on the Repository interface
- `repository.go` — Repository interface + SQLC postgres implementation + `toDomain()` mapping
- `handler.go` — HTTP only (parse request, call service, write response)
- `module.go` — Fx module registration, nothing else

**Route registration via Fx groups:**
Each module's handler implements `httpserver.RouteRegistrar` and is contributed to the
`group:"routes"` Fx value group. `newRouter` in `main.go` collects all registrars and
calls `RegisterRoutes` on each. **Adding a new module requires zero changes to `newRouter`**
— only add `fx.Options(mymodule.Module)` to `fx.New(...)`.

**SQLC per-module packages:**
Each module has its own SQLC config entry in `sqlc.yaml` generating to
`internal/modules/<name>/repository/db/` with package `<name>db`.
The shared package `internal/shared/postgres/db` contains only the `Ping` query.
SQLC-generated types **never leave the repository layer** — always map to domain types in `toDomain()`.

---

## 10. DIRECTORY STRUCTURE

```
community-backend/
├── cmd/
│   └── api/
│       └── main.go                          # Fx bootstrap, router, health endpoint
├── internal/
│   ├── config/
│   │   └── config.go                        # All env vars in one struct
│   ├── shared/
│   │   ├── httpserver/
│   │   │   ├── routes.go                    # RouteRegistrar interface
│   │   │   └── respond.go                   # WriteJSON, WriteError helpers
│   │   ├── logger/
│   │   │   └── logger.go                    # zerolog + optional Loki HTTP writer
│   │   ├── postgres/
│   │   │   ├── postgres.go                  # Opens *sql.DB, runs Goose migrations
│   │   │   ├── queries/health.sql           # Ping query (shared only)
│   │   │   └── db/                          # SQLC generated (package: db)
│   │   └── storage/
│   │       └── storage.go                   # MinIO/S3 client, presigned URLs
│   └── modules/
│       ├── dailyimage/                      # ✅ COMPLETE
│       │   ├── domain.go                    # DailyImage domain struct
│       │   ├── repository.go                # Repository interface + postgres impl
│       │   ├── service.go                   # GetActive()
│       │   ├── handler.go                   # GET /daily-image
│       │   ├── module.go                    # Fx module
│       │   └── repository/
│       │       ├── queries/daily_images.sql
│       │       └── db/                      # SQLC generated (package: dailyimagedb)
│       └── debug/                           # ✅ COMPLETE — non-prod only
│           ├── handler.go                   # GET /debug/storage/upload-url
│           │                                # POST /debug/daily-image
│           ├── module.go                    # Returns nopRegistrar in prod
│           └── nop.go                       # No-op RouteRegistrar for production
├── migrations/
│   ├── embed.go                             # go:embed *.sql for Goose
│   ├── 00001_init.sql                       # schema_info table
│   └── 00002_daily_images.sql               # daily_images table
├── docker/
│   ├── docker-compose.yml                   # Postgres 17 + MinIO
│   └── docker-compose.redis.yml             # Optional Redis overlay
├── Dockerfile                               # Multi-stage, VERSION via -ldflags
├── .env.example
├── Makefile
├── sqlc.yaml
└── .github/
    ├── workflows/
    │   └── ci.yml                           # go vet + go test + go build
    └── dependabot.yml
```

---

## 11. DATABASE SCHEMA

### `schema_info`
| column | type | notes |
|---|---|---|
| key | TEXT PK | |
| value | TEXT | |

### `daily_images`
| column | type | notes |
|---|---|---|
| id | UUID PK | `gen_random_uuid()` |
| date | DATE UNIQUE | one image per calendar date |
| storage_key | TEXT | object key in MinIO/S3 — **never** a signed URL |
| width | INT | |
| height | INT | |
| is_active | BOOLEAN | partial unique index: at most one active row at DB level |
| created_at | TIMESTAMPTZ | |
| updated_at | TIMESTAMPTZ | |

**Rule:** `storage_key` stores only the object key (e.g. `photos/2026-03-14.jpg`).
Presigned URLs are always generated on-the-fly and never persisted.

---

## 12. LIVE API ENDPOINTS

| Method | Path | Description |
|---|---|---|
| GET | `/health` | DB ping + storage ping + version |
| GET | `/daily-image` | Active daily image with 15-min presigned download URL |
| GET | `/debug/storage/upload-url?key=<key>` | Presigned PUT URL — **non-prod only** |
| POST | `/debug/daily-image` | Activate an image by storage key — **non-prod only** |

### `/health` response
```json
{ "status": "ok", "db": "ok", "storage": "ok", "version": "dev" }
```

### `/daily-image` response
```json
{
  "id": "uuid",
  "date": "2026-04-11",
  "image_url": "http://localhost:9000/...",
  "width": 1920,
  "height": 1080,
  "expires_in_seconds": 900
}
```

---

## 13. PLANNED API ENDPOINTS (not yet built)

```
GET    /api/v1/periods/current       active period + game type + grid + all tile statuses
GET    /api/v1/periods/archive       paginated past periods
GET    /api/v1/periods/:id           single period (phase evolution for archive)
POST   /api/v1/tiles/:id/claim       claim a tile (anon or authenticated)
DELETE /api/v1/tiles/:id/claim       release a claim
POST   /api/v1/uploads/presign       presigned upload URL for tile drawing submission
POST   /api/v1/tiles/:id/submit      confirm submission
POST   /api/v1/auth/register
POST   /api/v1/auth/login
POST   /api/v1/auth/logout
GET    /api/v1/auth/google
GET    /api/v1/auth/google/callback
GET    /api/v1/auth/me
POST   /api/v1/feedback              plain text feedback, no login required
```

---

## 14. CONFIGURATION (ENV VARS)

```
# Server
PORT=8080
ENV=local                        # local | dev | prod
CORS_ALLOWED_ORIGINS=...         # []string via envconfig — comma-separated

# Logging
LOG_LEVEL=debug                  # debug | info | warn | error
LOKI_URL=                        # Grafana Cloud push URL (empty = stdout only)
LOKI_USERNAME=
LOKI_PASSWORD=

# Database
DATABASE_URL=postgres://community:community@localhost:5432/community?sslmode=disable

# Storage
STORAGE_DRIVER=minio             # minio | s3
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=community
MINIO_SECRET_KEY=community
MINIO_BUCKET=community-assets
AWS_REGION=
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=
S3_BUCKET=
```

---

## 15. LOCAL DEVELOPMENT

**Prerequisites:** Docker, Go 1.26, `make`

```bash
make env        # creates .env from .env.example (skips if already exists)
make dev        # docker compose up -d + go run ./cmd/api
make build      # compiles to bin/api
make test       # go test ./...
make lint       # go vet ./...
```

**Docker services:**
- Postgres 17 → `localhost:5432` (user/pass/db: `community`)
- MinIO S3 API → `localhost:9000`
- MinIO Console → `http://localhost:9001` (login: `community` / `community`)

**Migrations:** run automatically on app startup via Goose embedded FS. No manual step needed.

**Testing the full local flow:**
```bash
# 1. Get presigned upload URL
curl "http://localhost:8080/debug/storage/upload-url?key=photos/test.jpg"

# 2. Upload image to MinIO
curl -X PUT "<upload_url>" --upload-file ./image.jpg -H "Content-Type: image/jpeg"

# 3. Set as active daily image
curl -X POST "http://localhost:8080/debug/daily-image" \
  -H "Content-Type: application/json" \
  -d '{"storage_key":"photos/test.jpg","width":1920,"height":1080}'

# 4. Fetch active image
curl "http://localhost:8080/daily-image"
```

---

## 16. ADDING A NEW MODULE

Follow this order exactly:

1. `migrations/000XX_<name>.sql` — Up + Down sections
2. Add entry to `sqlc.yaml` pointing to `internal/modules/<name>/repository/queries/`
3. Write SQL query files
4. `export PATH=$PATH:$(go env GOPATH)/bin && sqlc generate`
5. `internal/modules/<name>/domain.go` — pure domain struct, no tags, no infra imports
6. `internal/modules/<name>/repository.go` — Repository interface + `postgresRepository` + `toDomain()`
7. `internal/modules/<name>/service.go` — business logic, depends only on Repository interface
8. `internal/modules/<name>/handler.go` — HTTP, implements `httpserver.RouteRegistrar`
9. `internal/modules/<name>/module.go`:
```go
var Module = fx.Options(
    fx.Provide(
        NewPostgresRepository,
        NewService,
        fx.Annotate(
            NewHandler,
            fx.As(new(httpserver.RouteRegistrar)),
            fx.ResultTags(`group:"routes"`),
        ),
    ),
)
```
10. Add `fx.Options(<name>.Module)` to `fx.New(...)` in `cmd/api/main.go`

---

## 17. SHARED PACKAGES — USAGE RULES

### `internal/shared/httpserver`
- Use `WriteJSON(w, status, body)` for **all** successful responses
- Use `WriteError(w, status, message)` for **all** error responses
- Never call `json.NewEncoder` directly in handlers

### `internal/shared/storage`
- `PresignedGetURL(ctx, key, expiry)` — download URL
- `PresignedPutURL(ctx, key, expiry)` — upload URL
- `Ping(ctx)` — health check
- Always store the **object key**, never the signed URL

### `internal/shared/postgres`
- Returns `*sql.DB` — each module calls `<namedb>.New(sqlDB)` in its repository
- Runs Goose migrations on startup automatically

### `internal/shared/logger`
- Always use the injected `zerolog.Logger`
- Never use `fmt.Println`, `log.Print`, or stdlib logger

---

## 18. HOSTING & DEPLOYMENT

**Railway:**
- Dev → auto-deploys from `develop` branch
- Prod → auto-deploys from `master` branch
- Railway provides `PORT` automatically — never hardcode it
- Env vars set in Railway dashboard per environment

**GitHub Actions (`.github/workflows/ci.yml`):**
- Triggers on every PR to `develop` and `master`
- Steps: `go vet ./...` → `go test ./...` → `go build ./cmd/api`
- golangci-lint temporarily replaced with `go vet` until it supports Go 1.26

**Observability:**
- Logs: zerolog JSON → stdout (always) + Grafana Cloud Loki (when `LOKI_URL` set)
- Loki label: `{app="community-backend", env="<ENV>"}`
- View logs: Grafana Cloud → Explore → datasource `grafanacloud-<stack>-logs`

---

## 19. MODULES ROADMAP

| Module | Status | What it does |
|---|---|---|
| `dailyimage` | ✅ Done | Serve active daily image with presigned URL |
| `debug` | ✅ Done | Upload image + set active image (local/dev only) |
| `grid` | ⬜ Next | Periods, tile generation, grid progression, phase transitions, period rotation cron |
| `claim` | ⬜ Planned | Tile claiming + TTL lock sweep |
| `submission` | ⬜ Planned | Presigned upload + crop metadata + confirm + completion trigger |
| `users` | ⬜ Planned | Registered user accounts |
| `auth` | ⬜ Planned | Server-side sessions + Google OAuth |
| `feedback` | ⬜ Planned | Plain text feedback form, no login required |
| `featureflags` | ⬜ Planned | DB-backed runtime feature toggles |

---

## 20. OPEN / TBD DECISIONS

These are unresolved. Do not hardcode assumptions — design for configurability.

| Decision | Status | Notes |
|---|---|---|
| Grid progression sequence | TBD | e.g. 4 → 12 → ? Must scale to 1,000+ tiles. Stored in DB. |
| Multiple games: simultaneous or one/day | TBD | Both games share tile/claim/submission mechanics |
| Fusion/composite rendering | TBD | Server-side image compositing vs frontend CSS overlay |
| Tile selection | TBD | Manual (user picks) / Random (system assigns) / Hybrid |
| Grid overlay at 1,000+ tiles | TBD | How to render FREE/LOCKED/DRAWN states at scale |
| Phase transition UX | TBD | How to communicate grid subdivision to the user |
| Lock expiry UX | TBD | How to show a tile is about to expire and return to FREE |
| Dark mode | TBD | MVP or post-launch |
| Social login providers | TBD | Google confirmed, others TBD |

---

## 21. POST-LAUNCH FEATURES (out of scope for MVP)

- User accounts with contribution history and rankings
- Lock expiry: auto-release LOCKED tile after timeout
- Online counter: live count of users on the page
- Voting on completed archive images
- Comments on archive images
- Dark mode

---

## 22. WHAT NOT TO DO

- No business logic in handlers
- No SQLC-generated types outside the repository layer
- No hardcoded URLs, ports, or secrets
- No hardcoded grid progression sequences — always read from DB
- No event sourcing
- No Kafka or message brokers
- No Redis for MVP (optional later for rate limiting / caching)
- No global state
- No images stored in Postgres (only metadata + storage key)
- No presigned URLs persisted in the DB
- No `debug` routes in `ENV=prod`
- No `fmt.Println` or stdlib logger (use injected zerolog)

---

## 23. FRONTEND QUICK REFERENCE

- Framework: Next.js 16 (App Router) + TypeScript + Tailwind CSS v4
- UI: shadcn/ui + Framer Motion
- Server state: TanStack Query v5
- Client state: Zustand
- Localization: i18next + react-i18next (`en` + `es` + `da`)
- API base URL env var: `NEXT_PUBLIC_API_URL`
- Auth: session cookies (`credentials: 'include'` on all requests)
- Local frontend URL: `http://localhost:3000`
- Backend CORS must allow the frontend origin via `CORS_ALLOWED_ORIGINS`
- PWA: installable on mobile (post-MVP)
