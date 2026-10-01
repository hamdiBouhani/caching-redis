# caching-redis

---
## User Service

> A Go + Redis reference service demonstrating production-grade patterns:
> cache-aside with singleflight, sliding-window rate limiting via Lua,
> Redis Streams for durable events, background jobs, and clean lifecycle management.

---

## Why This Project

Most Redis tutorials stop at `SET`/`GET`. This project shows what Redis actually
looks like in a production Go service:

- **Cache stampedes** handled with `singleflight` — 100 concurrent misses = 1 DB hit
- **Rate limiting** with no boundary bursts, using a Lua sliding window
- **Durable events** via Redis Streams + consumer groups (not fire-and-forget Pub/Sub)
- **Graceful shutdown** that drains HTTP, stops workers, and closes Redis in order
- **Correct k8s probes** — liveness ≠ readiness, no thundering-herd restarts
- **Deterministic tests** for time-dependent logic via an injectable clock

Every pattern here solves a real problem you'd hit at scale.

---

## Quickstart

### Prerequisites

- Go 1.23+
- Docker (for local Redis)

### Run locally

```bash
# 1. Start Redis
make redis-up

# 2. Run the service (hot reload)
make dev

# 3. Hit it
curl "http://localhost:8080/api/user?id=123"
curl -X POST "http://localhost:8080/api/user/update?id=123"
curl "http://localhost:8080/healthz"
curl "http://localhost:8080/readyz"
```

### Run the full stack

```bash
make up      # docker compose up -d --build
make logs
make down
```

---

## API

| Method | Path | Description |
|---|---|---|
| `GET`  | `/api/user?id={id}` | Fetch a user (cache-first) |
| `POST` | `/api/user/update?id={id}` | Update a user, invalidate cache, emit event |
| `GET`  | `/healthz` | Liveness probe — is the process alive? |
| `GET`  | `/readyz` | Readiness probe — can it serve traffic? |

### Example

```bash
$ curl -s "http://localhost:8080/api/user?id=123" | jq
{
  "id": "123",
  "name": "Alice",
  "email": "alice@example.com"
}
```

Response headers include rate-limit info:

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 97
X-RateLimit-Reset: 1735689600
```

On rejection:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 42
X-RateLimit-Remaining: 0
```

---

## Architecture

```
Client → Gin router
  ├─ Rate limit middleware (Lua sliding window)
  ├─ Handler
  │   ├─ Cache.GetOrLoad  →  Redis GET → miss → singleflight → DB → SET
  │   ├─ Stream event     →  XADD "user.viewed"
  │   └─ Background job   →  asynq enqueue
  └─ JSON response

Stream consumer (background)  →  XREADGROUP → handle → XACK
asynq worker (background)     →  dequeue → process
```

### Request path

1. **Rate limit middleware** runs a Lua script that atomically checks a sorted
   set — sliding window, no bursts, precise reset time.
2. **Cache.GetOrLoad** tries Redis. On miss, `singleflight` collapses concurrent
   callers into a single DB load.
3. The handler **publishes a durable event** to a Redis Stream (`XADD`) and
   **enqueues a background job** via asynq.
4. Response goes out with `X-RateLimit-*` headers.

### Background path

- A **stream consumer** reads via `XREADGROUP`, processes, and `XACK`s. On crash,
  unacked messages are reclaimed via `XAUTOCLAIM`.
- An **asynq worker** processes queued jobs with automatic retries.

---

## What's Demonstrated

| Pattern | File | Why |
|---|---|---|
| Cache-aside + singleflight | `cache.go` | Prevents cache stampedes |
| Sliding-window rate limiting | `ratelimit.lua`, `ratelimit.go` | Atomic, no boundary bursts |
| Redis Streams + consumer groups | `streams.go` | Durable, at-least-once events |
| Background jobs (asynq) | `worker.go` | Retries, scheduling, priorities |
| Distributed locks (redsync) | `locks.go` | Cross-instance coordination |
| Graceful shutdown | `main.go` | Drain HTTP → stop workers → close Redis |
| Health vs. readiness | `main.go` | Correct k8s probe semantics |
| Injectable clock | `ratelimit.go` | Deterministic time tests |
| Middleware rate limiting | `middleware.go` | Handlers stay clean |

---

## Project Layout

```
.
├── main.go              # wiring, lifecycle, HTTP server
├── cache.go             # cache-aside + singleflight
├── ratelimit.go         # sliding-window limiter
├── ratelimit.lua        # Lua script (atomic)
├── streams.go           # durable event producer + consumer
├── worker.go            # asynq background worker
├── middleware.go        # rate limit middleware
├── handlers.go          # Gin handlers
├── *_test.go            # unit tests
├── makefiles/           # split Makefiles
│   ├── common.mk
│   ├── build.mk
│   ├── test.mk
│   ├── lint.mk
│   ├── docker.mk
│   ├── redis.mk
│   ├── tools.mk
│   └── release.mk
├── .github/workflows/   # CI + release pipelines
├── .goreleaser.yml
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

---

## Development

```bash
make help           # list all targets

# Quality
make check          # tidy + fmt + vet + lint + test
make test-cover     # HTML coverage report
make bench          # benchmarks

# Dev loop
make redis-up
make dev            # hot reload
make redis-down

# Build & ship
make build          # local binary
make build-linux    # cross-compile
make docker-build
make release-snapshot
```

### Testing

```bash
go test ./... -race -v
go test -run TestRateLimiter -race -cover
go test -bench=. -benchmem -run=^$
```

Tests use **miniredis** + an **injectable clock** so they're fast, deterministic,
and don't need a real Redis.

### Docker

```bash
make docker-build
make docker-run
```

---

## Configuration

| Env Var | Default | Description |
|---|---|---|
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `HTTP_ADDR` | `:8080` | HTTP listen address |
| `GIN_MODE` | `release` | `debug` or `release` |

---

## Deployment

### Kubernetes probes

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: 8080 }
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet: { path: /readyz, port: 8080 }
  periodSeconds: 5
  failureThreshold: 2
```

**Rule of thumb:** never put external dependencies (Redis, DB) in *liveness*.
A Redis blip should remove the pod from Service endpoints (readiness), not
restart every pod in a thundering-herd cycle.

### Docker image

Published to `ghcr.io/your-org/user-service` on every tag:

```bash
docker pull ghcr.io/your-org/user-service:1.2.3
```

---

## Releases

Cut a release with:

```bash
make tag V=1.2.3
```

CI then runs GoReleaser (binaries for Linux/macOS/Windows, checksums, changelog)
and pushes a versioned Docker image.

---

## Tech Stack

- **Go 1.23** — language
- **Gin** — HTTP framework
- **Redis 7** — cache, limiter, streams, queue
- **go-redis/v9** — client
- **asynq** — background jobs
- **singleflight** — stampede protection
- **redsync** — distributed locks
- **miniredis + testify** — testing
- **Docker Compose** — local dev
- **GitHub Actions + GoReleaser** — CI/CD

---

## License

MIT — see [LICENSE](LICENSE).

---

## One-Liner

> A Go + Redis reference service showing cache-aside with singleflight,
> sliding-window rate limiting via Lua, Redis Streams for durable events,
> and production-grade lifecycle management.