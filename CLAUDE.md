# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this service is

notifications-service holds every user's activity feed and notification inbox, built for one-index-scan reads. It is a Cosmo federation subgraph (`feed`, `userActivity`, `notifications`, preferences, push subscriptions) plus two NATS JetStream consumers that write the tables:

- `consume activity` reads `user-activity` (published by list-service's outbox relay) and fans each activity out to the actor's accepted followers (`feed_items`), one row per recipient. Private lists (`lists_public = false`) and removals are stored but never fanned out.
- `consume follow` reads `user-follow` (published by user-service's outbox relay), writes `notifications` rows (`follow_requested`, `follow_accepted`, `new_follower`), seeds a new follower's feed, clears a feed on unfollow, and publishes `notification-deliver` for channels the recipient enabled (push, email) -- the hook for the delivery workers.

It stores only ids. `actor`, `anime` and `work` are resolved by user-service and anime-api through the router (`extend type ... @key`).

## Development Commands

### Build & Run
- `go run cmd/main.go serve` - GraphQL API on port 3000 (`/graphql`, `/ui/playground`, `/healthcheck`, `/metrics`)
- `go run cmd/main.go consume activity` / `consume follow` - the consumers
- `go run cmd/main.go migrate up|down`
- `docker compose up -d` - Postgres 16.4 and NATS JetStream for local runs and the end-to-end tests
- Config via env: `DBHOST/DBPORT/DBUSERNAME/DBPASSWORD/DBNAME/DBSSL/DBMIGRATIONTABLE`, `NATSURL`, `NATSCONSUMERGROUPNAME`, `NATSOFFSET`, `USER_SERVICE_URL` (the user-service subgraph, not the router)

### Code Generation
- `make gql` - gqlgen (`skip_mod_tidy` is set; run `go mod tidy` yourself). `graph/generated` and `graph/model/models_gen.go` are committed. `graph/model/entities.go` is hand-written: the external entities have no generated model.

### Testing
- `go test ./...` - unit tests (services use hand-rolled fakes; no database)
- `make test-integration` - end-to-end (build tag `integration`): embedded NATS JetStream, both consumers and the API in-process, a fake user-service, real Postgres with the migrations applied. Scenarios: fan-out, idempotent redelivery, private lists, seed on follow, clear on unfollow, withdrawn requests, preferences -> delivery, push subscriptions, malformed event -> dead-letter subject.
- Local toolchain notes are the same as user-service: `GOTOOLCHAIN=go1.24.0` for gqlgen/tidy, `DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer` to link on recent macOS.

## Layout
- `graph/` schema and generated code; `internal/resolvers` maps services to the schema
- `internal/db/repositories/{activities,notifications}` - storage
- `internal/services/feed` - ingest and fan-out; `internal/services/notifications` - inbox, preferences, delivery requests
- `internal/consumers` - ep/v2 NATS processors with retry and dead-letter subjects (`<subject>-retry`, `<subject>-dlq`)
- `internal/clients/userservice` - `publicUserByID` and `followerIDs` against user-service
- `internal/deliver` - `notification-deliver` publisher (`deliver.Noop` in the API process)
- `internal/wiring` - builds the service graph once for the API and the consumers
- `db/migrations` - golang-migrate SQL; `.github/workflows/migrations.yaml` verifies 5 tables / 38 columns

## Data model
- `activities(id = event id, actor_id, type, anime_id|work_id, status, previous_status, score, occurred_at)` with `(actor_id, occurred_at desc)`
- `feed_items(user_id, activity_id, actor_id, occurred_at)` with `(user_id, occurred_at desc)` and `(user_id, actor_id)`
- `notifications(id, event_id, user_id, type, actor_id, activity_id, payload, read_at, created_at)` unique `(user_id, event_id)`; partial index `(user_id) WHERE read_at IS NULL` makes the unread count an index-only scan
- `notification_preferences(user_id, type, channel, enabled)` stores overrides only; default is in-app on, push and email off
- `push_subscriptions(id, user_id, platform, endpoint UNIQUE, p256dh, auth, user_agent, ...)`

## Later phases (hooks are in place)
- `deliver push`: consume `notification-deliver`, Web Push via `push_subscriptions` (VAPID keys from a Secret)
- `deliver email`: consume `notification-deliver`, SMTP + MJML as in the auth service, daily digest for feed-type mail
