# Game-model rework — single photo, concentric-ring reveal

> **Status**: design locked, M1 in progress.
> **Audience**: a future agent (Claude or human) picking any milestone up cold.
> **Rule of thumb while reading**: everything is deliberate. When something is
> under-specified, it means "either is fine — pick one and note it in the PR."

## 0 · Why this doc exists

We are changing the shape of the game. The pivot is small in the pitch but
touches most of the codebase (schema, sweeper, grid render, admin, i18n). This
doc is the single source of truth for the pivot so future turns don't
re-litigate decisions the user already made.

If you find yourself weighing an option that isn't in this doc, look in
`.claude/projects/-home-charles-dev-community/memory/` for related feedback
memories first. If it's genuinely a new decision, ask the user rather than
guess.

## 1 · What changes at the concept level

### 1.1 Before

Two parallel games ran side-by-side:

- **Photo mode**: users copy a daily reference photo, tile by tile. Each
  completed phase re-tiles the same photo at a denser grid (3×3 → 6×6 →
  10×10 → …). The reference image stays the same size; earlier drawings are
  replaced by finer-grained ones at the next phase.
- **Prompt mode**: users free-draw on an empty canvas from a text prompt.
  Same 3×3-then-denser subdivision.

Both had their own schedule table, their own sweeper branch, their own
`GameType` value. The user picked the mode on the landing before hitting
Play.

### 1.2 After

**One mode.** Prompt is deleted. Photo mode changes its progression model:

- Each period has a **final grid size** (default 9×9, admin-configurable).
- **Phase 1 only unlocks the center 3×3**. Everything outside the center is
  either visibly locked ("blocked" render mode) or not rendered at all
  ("hidden" render mode) — see §3.4.
- Each phase advance **unlocks the next concentric ring outward** — 3×3 →
  5×5 → 7×7 → 9×9 by default, admin-configurable.
- **Tiles freeze once drawn.** A drawing from phase 1 is preserved verbatim
  in phase 2 as an interior tile of the larger visible grid. The player who
  drew it in phase 1 owns that spot forever.
- When the **outermost ring's last tile is drawn**, the masterpiece is
  complete. The period transitions to `completed`. The frontend swaps to a
  **completed view**: the composed final image full-bleed with a live
  countdown to the next scheduled picture and a "come back tomorrow" message.

The countdown target is `next_period_starts_at`, computed from the daily
schedule and the current Copenhagen midnight cutoff (same source the sweeper
already uses).

### 1.3 Locked design decisions (do not re-open)

| Decision                       | Choice                                                                                                        |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| Phase 1 size                   | **3×3 center**                                                                                                |
| Ring progression               | Admin-configurable via `phase_grid_sizes` (JSON int array), **default `[3, 5, 7, 9]`**                        |
| Tiles freeze once drawn        | **Yes** — a drawn tile is immutable across all subsequent phases                                              |
| Completed-state UX             | Composed masterpiece + live countdown card (`HH:MM:SS`) to next picture                                       |
| Outer-tile display             | Admin-configurable via `outer_tile_display` (`"blocked"` \| `"hidden"`), **default `"blocked"`**              |
| Legacy data                    | **Fresh dev DB.** No effort spent grandfathering old periods. See §6.                                         |

## 2 · Concept glossary

Words to use consistently. If you disagree with a name, say so in the PR and
we'll rename — but do not use both the old and the new name.

- **Final grid size** — the side length of the fully-revealed grid (e.g. `9`
  means the last phase's grid is 9×9). Stored on `periods.final_grid_size`.
- **Phase grid size** — the side length of the currently-unlocked window. For
  a phase-1 default this is `3`. Grows to the final size by the last phase.
  Not stored; derived from `phase_grid_sizes[phase-1]`.
- **Ring** — the set of tiles unlocked when transitioning from phase N-1 to
  phase N. Ring N = tiles inside phase N's grid but outside phase N-1's grid.
- **Center** — the geometric center of the final grid. For a 9×9 that's
  `(4, 4)` (0-indexed). All phase grids are centered on this same point.
- **Tile phase** — the smallest phase whose window covers a tile. Assigned
  once, at period creation, when we seed all `final_grid_size²` tiles.
- **Outer tile display** — how the frontend renders tiles whose phase >
  current period phase.
  - `"blocked"`: rendered dark and inert, no photo peek, no click, no lock
    icon — just visibly *not yet*. Viewport is the full final grid from day
    one.
  - `"hidden"`: not rendered at all. Viewport is exactly the currently-
    unlocked window; it grows each phase, shrinking earlier drawings into
    the center.

## 3 · Milestones

Land these in order. Each is a self-contained PR-shaped chunk. A milestone
name in `git log` is fine — `feat(m2): drop prompt mode from frontend` works.

### M1 · Backend model change

**Goal**: the database and grid service reflect the new model. Old-shape data
is dropped. No frontend changes yet — the frontend keeps talking to the old
API and will fail loudly until M2/M3 land.

#### M1.1 · Migrations

Add these under `community-backend/migrations/`. Numbering picks up from
`00020_fix_extend_claim.sql` — so start at `00021`.

1. `00021_drop_prompt_schedule.sql` — drops `daily_prompt_schedule` and any
   supporting index. Down migration recreates it (copy from
   `00015_daily_prompt_schedule.sql`).
2. `00022_periods_final_grid.sql` — adds `periods.final_grid_size INT NOT
   NULL DEFAULT 9`. Backfill isn't needed (fresh DB), but the migration must
   handle the case where rows exist: default handles them.
3. `00023_seed_new_settings.sql` — inserts `phase_grid_sizes = '[3,5,7,9]'`
   and `outer_tile_display = 'blocked'` into `app_settings` if they don't
   already exist (`ON CONFLICT DO NOTHING`). Also seed the schedule cadence
   knob if we introduce one — see §3.5.
4. `00024_tile_phase_semantics.sql` — depends on how M1.3 encodes tile
   locking. Options:
   - **Option A** *(recommended)*: add `tiles.phase_locked BOOL NOT NULL
     DEFAULT FALSE`. Tiles with `phase_locked=TRUE` are future-locked; they
     become `FALSE` (and pick up `status='free'`) when their phase unlocks.
     Keep the existing `tiles.status` enum (`free`, `locked`, `drawn`).
   - **Option B**: add a `future_locked` value to the status enum. Cleaner
     conceptually but adds enum-cardinality risk to any consumer that
     switches on status. Only pick this if you're willing to touch every
     switch statement.

Drop `grid_configs` **only if you're sure no other subsystem reads it**.
Safer default: leave the table, stop populating/reading it. A follow-up PR
can remove it once you've grepped every module.

#### M1.2 · `appsettings` accessors

`community-backend/internal/shared/appsettings/appsettings.go` already has
the pattern (see `PeriodDuration`, `TileRetentionDays`). Add:

- `KeyPhaseGridSizes = "phase_grid_sizes"`, default `[3, 5, 7, 9]`.
- `KeyOuterTileDisplay = "outer_tile_display"`, default `"blocked"`.
- `PhaseGridSizes(ctx, db) ([]int, error)` — parses the JSON array,
  validates it (strictly increasing, all odd, first ≥ 3, last ≤ some sane
  cap like 21). Returns the default on any parse/validation error and logs
  a warning.
- `OuterTileDisplay(ctx, db) string` — returns `"blocked"` or `"hidden"`;
  anything else falls back to `"blocked"`.
- `SetPhaseGridSizes(ctx, db, sizes []int) error` — validates before
  writing.
- `SetOuterTileDisplay(ctx, db, mode string) error` — validates before
  writing.

**Why validation lives here**: we want a single choke point where invalid
config can't be persisted. The admin handler calls the setter; if it
returns an error the admin sees a 400.

#### M1.3 · Grid service — tile seeding at period start

`community-backend/internal/modules/grid/service.go`.

Current `CreatePhotoPeriod` calls `populatePhase1Tiles`, which creates a
`GetGridConfig(1)` grid of tiles. Rework:

- New `populateAllTiles(ctx, period, finalSize, phaseSizes)`:
  - For each `(row, col)` in `[0, finalSize) × [0, finalSize)`:
    - Compute `phase` = smallest `p` such that
      `abs(row - center) < phaseSizes[p-1]/2 AND abs(col - center) < phaseSizes[p-1]/2`
      where `center = finalSize / 2` (integer div; assumes odd finalSize).
    - Create the tile with that phase.
    - Set `phase_locked = phase > 1` (i.e. only phase-1 tiles start
      unlocked).
- Store `finalSize` on `periods.final_grid_size` at creation.
- Remove `CreatePromptPeriod` and `populatePhase1Tiles`.

The service reads `finalSize` from `app_settings.final_grid_size` if we
introduce a global default (recommended so admins can change it without a
per-period UI), else it's a constant.

**Assumption**: `finalSize` is always odd. Enforce in validation. Even
grids don't have a single-tile center; the game concept breaks.

#### M1.4 · Grid service — phase advance = unlock next ring

Rework `CheckPhaseCompletion`:

- "All drawn?" → count `WHERE status = 'drawn' AND phase <= period.phase`.
  Compare to total for that phase window.
- If not all drawn, return `PhaseIncomplete`.
- If phase is the last one in `phase_grid_sizes`, **compose masterpiece +
  complete period** (existing `CompletePeriod` + `ComposeFinalImage`).
  Return `PhaseAllComplete`.
- Otherwise: bump `periods.phase` and flip `phase_locked = FALSE` for all
  tiles at `phase = period.phase + 1`. Their status stays `free`. Return
  `PhaseAdvanced`.

**Do not create new tile rows on advance.** All tiles were seeded at period
start; advance is a status-flip only.

Delete `GetGridConfig`-per-phase call sites from this path. `grid_configs`
is no longer authoritative.

#### M1.5 · Sweeper — one game type, no rotation ambiguity

- Delete `GamePrompt` from `domain.go`. `GameType` becomes effectively a
  single-value enum; consider deleting the type entirely and hardcoding
  `"photo"` in the DB. If you keep the column for archive compatibility,
  set the default to `'photo'` and drop the `Valid()` check.
- `SweepExpiredPeriod` no longer loops game types.
- `RotateForToday` no longer takes a `GameType` param.
- Delete `rotatePromptForToday` and the `GetScheduledPromptByDate` repo
  call. Prompt-schedule tables/queries are gone as of M1.1.

#### M1.6 · `/periods/current` response

`community-backend/internal/modules/grid/handler.go`, `buildPeriodResponse`.

Add to the JSON response:

```json
{
  "period": {
    "status": "active" | "completed",
    "phase": 2,
    "final_grid_size": 9,
    "phase_grid_size": 5,
    "next_period_starts_at": "2026-07-10T00:00:00+02:00"
  },
  "grid": {
    "outer_tile_display": "blocked" | "hidden",
    "tiles": [
      { "row": 0, "col": 0, "status": "future_locked" | "free" | "locked" | "drawn", ... }
    ]
  }
}
```

Field notes:

- `phase_grid_size` = `phase_grid_sizes[phase-1]`. Frontend uses this to
  compute the unlocked window for both render modes.
- `next_period_starts_at`: for `active` periods, omit it. For `completed`
  periods, return the timestamp of the next Copenhagen midnight (or the
  next scheduled image's date, whichever the sweeper honors — they're the
  same for the default cadence).
- `outer_tile_display` is repeated on every response (small string; not
  worth a separate `/config` endpoint yet).
- **Tile status in the wire format**: expose `future_locked` for tiles
  where `phase_locked = TRUE`. Frontend distinguishes render treatment
  from the string; do not conflate with `locked` (which means "another
  player is currently drawing this").

#### M1.7 · SSE `period_updated` payload

The frontend re-fetches on this event. No change needed to the event shape;
just ensure the sweeper fires it whenever `phase_locked` flips (i.e. on
ring-unlock) so players see the newly-drawable tiles without a refresh.

#### M1.8 · Debug handler cleanup

`community-backend/internal/modules/debug/handler.go`:

- Delete `upsertPromptSchedule`, `POST /debug/prompt-schedule`, and any
  route registration.
- Keep `POST /debug/period` for creating an ad-hoc period; simplify to no
  longer accept a `game_type` param.

#### M1.9 · Verification checklist for M1

Before marking M1 done:

- [ ] Fresh dev DB migrates cleanly. `make migrate` (or the project's
      equivalent) runs green.
- [ ] `go build ./...` in `community-backend/` passes.
- [ ] `go test ./...` in `community-backend/` passes. If tests reference
      `GamePrompt`, delete or rewrite them — don't leave `t.Skip`.
- [ ] Manual: schedule an image for today via `POST /debug/schedule`,
      confirm a period spawns with 81 tiles for a 9×9 grid, 9 of which
      have `phase_locked=FALSE`.
- [ ] Manual: `POST /debug/draw-all` (or the equivalent) advances phase
      by phase until the masterpiece is composed, and
      `/api/v1/periods/current` returns `status: "completed"` with a
      `next_period_starts_at` timestamp.
- [ ] Frontend deliberately broken but crash mode is clear (e.g. mode
      picker sends `game_type=prompt` and gets 400) — this is expected
      until M2.

### M2 · Frontend prompt-mode removal

**Goal**: strip every prompt-mode code path so the app talks to the new
backend without runtime errors. No new UX yet — M3 adds the render changes.

Files to touch (this is a checklist, not a plan — the change in each is
mechanical):

- `community-frontend/src/app/[locale]/LandingView.tsx` — delete
  `ModeToggle`; single Play button; drop the `mode` state and the
  `useLockBodyScroll` mode-swap flash animation.
- `community-frontend/src/app/[locale]/play/[game]/page.tsx` → rename to
  `community-frontend/src/app/[locale]/play/page.tsx`. Drop the
  `[game]` segment. `PlayPage` no longer parses `game` from the URL.
- `community-frontend/src/components/game/DailyImageGrid.tsx` — remove
  the `gameType` prop, all `isPromptGame` branches, the `promptText`
  path, and the prompt-mode tile styling in `TileCell` (`bg-zinc-300`,
  the ruled-lines background, etc.). Everything is photo-mode now.
- `community-frontend/src/components/game/UploadSheet.tsx` — same
  treatment. The `prompt` prop goes.
- `community-frontend/src/components/layout/AppNav.tsx` — drop the
  `?game=${lastGameType}` param on the Museum link and the Play link
  routes to `/play` (no mode suffix).
- `community-frontend/src/lib/store/game.store.ts` — remove
  `lastGameType`, `setLastGameType`, and the persist merge fallback.
  The persisted key stays (`community-game`); old persisted state with
  `lastGameType` is silently ignored on load.
- `community-frontend/src/lib/api/period.ts` — `fetchCurrentPeriod` no
  longer takes `gameType`. Remove `?game_type=` from the URL.
- `community-frontend/src/types/api.ts` — remove `GameType`. `PeriodInfo`
  drops `game_type` (or keep it as a fixed literal `'photo'` if the
  archive still surfaces it — probably not worth the churn).
- `community-frontend/src/app/[locale]/archive/CalendarView.tsx` — drop
  the game filter UI and the `?game=` param handling. Museum shows
  every completed period, oldest-to-newest.
- `community-frontend/src/app/[locale]/archive/[id]/PeriodDetail.tsx` —
  drop the `period.game_type === 'prompt'` branch.
- `community-frontend/src/app/[locale]/admin/PromptScheduleSection.tsx`
  — delete outright.
- `community-frontend/src/app/[locale]/admin/AdminPanel.tsx` — remove
  the `<PromptScheduleSection />` mount and any prompt-related copy.
- `community-frontend/src/lib/api/debug.ts` — delete
  `listPromptSchedule`, `upsertPromptSchedule`, `deletePromptSchedule`,
  `PromptScheduleItem`.
- `community-frontend/public/locales/{en,es,da}/common.json` — delete
  every prompt key: `landing.mode_prompt`, `landing.play_prompt`,
  `landing.play_prompt_sub`, `archive.tab_prompt`, `nav.game`
  (revisit copy — probably just becomes "Play"), any others.

#### Verification for M2

- [ ] `pnpm tsc --noEmit` in `community-frontend/` passes.
- [ ] `pnpm eslint src` passes (only the pre-existing `<img>` warning
      in `DailyImageGrid` is acceptable — but check that lint didn't
      start flagging unused vars from the prompt cleanup).
- [ ] `pnpm dev` boots; landing, `/play`, `/archive`, `/blog`,
      `/feedback` all render without runtime errors. The game grid
      shape may look wrong until M3 — that's fine.

### M3 · Frontend new grid render

**Goal**: the game grid respects the new model — full grid rendered with
future-locked tiles inert (`"blocked"`) or only the current window
rendered (`"hidden"`).

`community-frontend/src/components/game/DailyImageGrid.tsx`:

- Read `outer_tile_display` off the period response. Default to
  `"blocked"` if the field is missing (so the frontend gracefully copes
  with an older backend during rollout).
- Read `final_grid_size` and `phase_grid_size` off the period response.
- **Render tiles by absolute position** (row/col over the full grid),
  not by index in the tile array. The tile array now contains all
  `final_grid_size²` tiles.
- Tile visual states:
  - `future_locked` + `outer_tile_display === "blocked"` → dark inert
    tile, no photo peek behind it, no cursor affordance, no click
    handler.
  - `future_locked` + `outer_tile_display === "hidden"` → do not render
    the tile at all. Use CSS grid with explicit column/row placement
    to skip it (or filter the array to only current-phase-and-below
    tiles).
  - `free`, `locked`, `drawn` — same as today, with the small
    detail that `drawn` tiles must render their submission image
    regardless of phase (frozen).
- **Reference photo positioning**: the source photo is aligned to the
  `final_grid_size × final_grid_size` frame. Each tile at `(row, col)`
  shows the `(row/finalSize, col/finalSize, 1/finalSize, 1/finalSize)`
  region of the photo. In `"blocked"` mode this is trivial (`object-fit:
  cover` on the whole grid with `background-position` per tile). In
  `"hidden"` mode the viewport is only `phase_grid_size × phase_grid_size`
  tiles wide, but the reference photo behind them should still show the
  correct crop — do the math in JS or use CSS `background-size` /
  `background-position` per tile with values scaled to the phase grid.

`community-frontend/src/components/game/PhaseIndicator.tsx`:

- Update the label. Was: `Phase II · 5×5`. Now: `Phase II · 5×5 / 9×9`
  (unlocked / total). The pip visualisation stays but pips scale with
  `phase_grid_sizes.length` if we can plumb that through — otherwise
  cap at 4 (the default length).

#### Verification for M3

- [ ] Toggle `outer_tile_display` in `app_settings` directly via SQL and
      reload the page — grid renders correctly in both modes.
- [ ] Drawings from earlier phases render on their tiles at every
      subsequent phase.
- [ ] The reference photo appears in the correct crop under each tile
      (no whole-photo-per-tile bug).

### M4 · Completed masterpiece + countdown view

**Goal**: when the last ring is drawn, the game screen switches to a
"come back tomorrow" resting state.

New component:
`community-frontend/src/components/game/MasterpieceCompleteView.tsx`:

- Props: `imageUrl` (the composed masterpiece), `nextStartsAt` (ISO
  timestamp).
- Renders the composed image full-bleed (respect the aspect ratio; letterbox
  if needed).
- Overlay card at the bottom (or top on wide screens): a live `HH:MM:SS`
  countdown to `nextStartsAt`, plus copy from `t('game.masterpiece_...')`.
- Countdown ticker: `setInterval(1000)`; on reach zero, `refetch` the
  period query — the new period will have kicked in by then.

`DailyImageGrid`:

- If `period.status === 'completed'`, render `MasterpieceCompleteView`
  instead of the grid. `PhaseIndicator` also hides (or shows a
  "Complete" state — designer's call).

i18n keys (add to `en/es/da/common.json`):

- `game.masterpiece_complete_title` — headline (e.g. "Masterpiece
  complete!" — reuse `phase.completed_title` copy if convenient).
- `game.masterpiece_complete_body` — subline (e.g. "Come back at midnight
  for a new picture.").
- `game.next_picture_in` — `Next picture in {{time}}` (frontend
  interpolates `HH:MM:SS`).

#### Verification for M4

- [ ] Drive a period to completion (M1's `/debug/draw-all`). Confirm the
      switch from grid → completed view is immediate (no refresh
      needed — SSE triggers a refetch).
- [ ] Countdown decrements every second and doesn't drift when the tab
      is backgrounded (compute against absolute timestamp, not
      accumulated ticks).
- [ ] On countdown reaching zero, the new period's grid appears.

### M5 · Admin controls

**Goal**: admin can tune `phase_grid_sizes` and `outer_tile_display`
without touching SQL.

`community-frontend/src/app/[locale]/admin/AdminPanel.tsx` — add two new
sections after the existing settings card:

1. **Phase progression** — an input for the ring sizes as
   comma-separated integers ("3, 5, 7, 9"). Validation client-side:
   strictly increasing odd ints, first ≥ 3, last ≥ first + 2. Save
   button posts to `POST /debug/settings/phase-grid-sizes` (or reuses
   the generic `POST /debug/settings` if we already have one) with the
   parsed array in the body. Show current value + reset-to-default
   link.
2. **Grid appearance** — a two-way toggle (`Blocked` / `Hidden`) that
   posts to `POST /debug/settings/outer-tile-display`. Small helper
   text under each choice describing the effect.

Backend: whichever handler you wire — reuse the existing app-settings
mutation pattern in `debug/handler.go`.

#### Verification for M5

- [ ] Change `phase_grid_sizes` from the admin UI. Rotate a fresh
      period (via `POST /debug/period` or by scheduling an image and
      waiting for midnight). Confirm the new period respects the new
      sizes.
- [ ] Change `outer_tile_display`. Reload the game page. Confirm the
      render mode swapped.

## 4 · Data model reference

Fresh DB after M1 lands. If you're reading this after M1 shipped, run the
migrations and this is what you'll see.

```
periods
  id                UUID PK
  daily_image_id    UUID (nullable — always set for photo periods; the
                          column stays nullable only because a legacy
                          migration left it that way, not because we
                          expect nulls)
  game_type         TEXT (always 'photo' — see §M1.5 note)
  status            TEXT ('active' | 'completed' | 'archived')
  phase             INT (1-indexed)
  final_grid_size   INT (odd, ≥ 3, e.g. 9)
  started_at        TIMESTAMPTZ
  ended_at          TIMESTAMPTZ (nullable)
  final_image_key   TEXT
  composed_at       TIMESTAMPTZ (nullable)

tiles
  id                UUID PK
  period_id         UUID FK
  row_index         INT (0-indexed, in final grid coordinates)
  col_index         INT (0-indexed, in final grid coordinates)
  phase             INT (the phase at which this tile unlocks; ≤ periods.phase
                         means it's currently drawable or drawn)
  phase_locked      BOOL (TRUE if phase > periods.phase — flipped to FALSE
                          when its ring unlocks)
  status            TEXT ('free' | 'locked' | 'drawn')
  submission_key    TEXT
  UNIQUE (period_id, row_index, col_index)

app_settings
  key = 'phase_grid_sizes'     value = '[3,5,7,9]'
  key = 'outer_tile_display'   value = 'blocked' | 'hidden'
  key = 'period_duration_hours'  (existing knob)
  key = 'tile_retention_days'     (existing knob)
```

The `tiles` row is uniquely `(period_id, row_index, col_index)` now — no
`phase` in the unique key, because a tile is a single entity across the
entire period lifetime (unlike the old model where each phase created a
fresh grid of tiles).

## 5 · Wire-format reference

`GET /api/v1/periods/current` response after M1.6:

```json
{
  "period": {
    "id": "…",
    "status": "active",
    "phase": 2,
    "final_grid_size": 9,
    "phase_grid_size": 5,
    "started_at": "2026-07-09T00:00:00+02:00",
    "next_period_starts_at": null,
    "image": { "image_url": "…", "width": …, "height": … },
    "phase_mosaics": [ … ]
  },
  "grid": {
    "outer_tile_display": "blocked",
    "columns": 9,
    "rows": 9,
    "total_tiles": 81,
    "drawn_count": 4,
    "tiles": [
      { "id": "…", "row": 3, "col": 3, "status": "drawn", "image_url": "…" },
      { "id": "…", "row": 3, "col": 4, "status": "free" },
      …
      { "id": "…", "row": 0, "col": 0, "status": "future_locked" }
    ]
  }
}
```

When `status === "completed"`, `next_period_starts_at` is populated;
`tiles` still contains the frozen final state (all drawn); `phase` is the
last phase reached.

## 6 · Rollout / legacy

Locked decision: **fresh dev DB**. Nothing to migrate.

Practically that means, when M1 is ready to review:

1. Push M1's migrations.
2. On the dev environment: `psql` in and `TRUNCATE periods, tiles,
   daily_images, daily_image_schedule CASCADE` (or drop-and-recreate the
   database — the `migrate down` path is not maintained for this pivot).
3. Re-schedule content via `POST /debug/schedule`.

If someone finds this doc after the pivot has shipped and there are real
periods in the DB, the migration path is out of scope of this doc — talk
to the user first.

## 7 · What's explicitly NOT in scope

- Redesigning the tile-claim / heartbeat / release lifecycle. Unchanged.
- Redesigning the submission upload flow (`UploadSheet` etc.). It just
  works over a tile whose status is `locked`; the fact that the tile
  belongs to a specific ring is transparent to it.
- Changing the composed-mosaic image format or storage layout.
- Reworking the archive detail view beyond what M2 requires (drop the
  `prompt` branch).
- Any changes to the phone-side `/upload` flow.
- Rewriting `docs/` in general. This is the one doc; if we need more,
  we'll write them one at a time.

## 8 · Working-agent conventions for this pivot

- **Do not commit unless asked.** The user's session pattern is design →
  implement → user says "push". Follow it.
- **Do not run the frontend dev server just to poke around.** M2/M3 have
  a natural "does it render" verification step; use it there, not
  everywhere.
- **When the user's ambiguity forces a choice, choose the safe boring
  option and note the choice in the PR body.** Do not schedule follow-up
  meetings via memory files — put it in the PR.
- **When in doubt about a field name**: match the closest existing field.
  We prefer snake_case in JSON, camelCase in Go struct fields with
  explicit `json:` tags, camelCase in TS.
- **This doc is authoritative for design.** If the code disagrees, the
  code is wrong (or the doc is out of date — update the doc in the same
  PR).
