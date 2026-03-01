🚀 COMMUNITY PROJECT – FULL CONTEXT HANDOFF

You are helping build a project called Community.

This message contains the full architectural and operational context.
Do not deviate from these decisions unless explicitly instructed.

1. PROJECT VISION

Community is a mobile-first collaborative web game.
Users collectively recreate an “image of the period” by claiming and redrawing sections (tiles) of the image.

The project must be:

A reference implementation of clean architecture

Scalable

Microservice-ready

Low-budget operationally

Strict about good engineering practices

2. CORE GAME MECHANIC

The homepage shows the image of the period.

Initially the image is divided into a small grid (e.g., 2×2 = 4 tiles).

Users can claim a tile.

Once claimed, it is temporarily locked.

User submits a camera photo recreating that tile.

When all tiles are completed:

The image is archived.

A new image appears.

Grid size increases (pattern configurable in DB).

Period logic:

Could be time-based (24h / 1 week)

Or completion-based

Must be configurable (not hardcoded).

3. ACCOUNT SYSTEM

Accounts are OPTIONAL.

Users can:

Play anonymously (enter nickname + social handle manually)

Or create an account

Authentication:

Self-hosted auth system

Google OAuth login supported

Auth must be modular and extractable to a microservice later

4. REPOSITORY STRUCTURE

Two repositories:

1️⃣ Backend  Repository

community-backend-service

2️⃣ Frontend Repository

community-frontend

React app

Mobile-first

Uses i18next for localization

5. BACKEND ARCHITECTURE

Language: Go 1.26
Framework: Uber Fx
Router: chi
Database: PostgreSQL
Migrations: Goose
Local object storage: MinIO
Remote object storage: AWS S3
Dependency updates: Dependabot

Architecture style:

Modular monolith initially

DDD-lite

Aggregates

Domain events (in-process only)

Infrastructure behind interfaces

Ready to evolve into microservices

NO event sourcing for MVP.

6. BACKEND MODULES (INITIAL) (propose ideas as well)

Modules inside modular monolith:

dailyimage

grid

claim

submission

featureflags

auth

users

Each module:

domain

application

infrastructure adapters

Domain events:

In-process dispatcher

Abstracted to allow future integration events

7. STORAGE STRATEGY
Local Development

Postgres via Docker

MinIO via Docker

Optional Redis (disabled by default)

Remote Dev

AWS S3 (dev bucket)

Managed Postgres (dev)

No MinIO

Production

AWS S3 (prod bucket)

Managed Postgres (prod)

Separate S3 buckets:

community-dev-assets

community-prod-assets

Uploads:

Use presigned URLs

Backend stores metadata in Postgres

8. REDIS POLICY

Redis is optional.

Not required for MVP.

May be used later for:

Fast tile locking

Rate limiting

Caching

Background job coordination

Template must support optional Redis toggle.

9. LOCAL DEVELOPMENT STANDARDS

Environment: Windows + WSL2

Use Docker Compose for:

Postgres

MinIO

Optional Redis

Makefile must include:

make env (generate .env automatically)

make dev (compose + migrate + run)

make migrate-up

make migrate-down

make db-reset

make test

make lint

.env is generated automatically from .env.example.

No manual local secret management.

10. CI/CD & BRANCHING STRATEGY

Default branch: master
Development branch: develop

Flow:

PR → develop → deploy to DEV environment

PR → master → stable branch (NO prod deploy)

Tag (vX.X.X) → deploy to PROD

Signed commits required.

Branch protection:

PR required

1 approval

Status checks required

No force push

No deletion

11. GITHUB ENVIRONMENTS

Two environments:

dev

prod

Dev deploys automatically from develop branch.
Prod deploys only from tags.
Prod requires manual approval.

Secrets stored in:

GitHub Environment secrets

Local env NOT pulled from GitHub.

12. DOCUMENTATION

MkDocs will be used later.
Docs live in /docs.
Deployed on push to master.
Not required immediately.

13. FRONTEND

React
Mobile-first
Flat modern UI
i18next for localization

Localization:

JSON files in repo

AI-assisted translation

No external TMS initially

15. DESIGN PRINCIPLES

Clean code

Clear boundaries

No premature microservices

Infrastructure behind interfaces

No global state

No business logic in handlers

Environment-driven configuration

Ready to scale but simple now

16. DO NOT DO

Do not introduce event sourcing

Do not overengineer

Do not introduce Kafka or brokers

Do not add unnecessary frameworks

Do not mix prod/dev buckets

Do not store images in Postgres

17. CURRENT TASK

We are starting from zero.
