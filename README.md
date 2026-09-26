# Order Backend (Go)

Backend for the **Unity Order Integration** assignment. It provides guest and email/password auth, and the `POST /api/orders` contract with server-side pricing and idempotency that stays correct under concurrent requests.

Stack: Go 1.25 · Fiber v3 · GORM (raw parameterized SQL) · PostgreSQL 16 · goose migrations · JWT HS256 · bcrypt.

The Unity client is out of scope for this repository (see [Handoff to Unity](#handoff-to-unity)).

---

## Quick start

### Option A: Postgres (default)

```bash
cp .env.example .env            # then set JWT_SECRET (≥ 32 chars)
make db-up                      # Postgres on :5433, pgAdmin on :5050
make migrate                    # goose up (needs: go install github.com/pressly/goose/v3/cmd/goose@latest)
make run                        # API on :8080
```

pgAdmin: http://localhost:5050 (server "Orders Local", password `orders`).

### Option B: no database (in-memory demo)

```bash
ORDER_REPO=memory JWT_SECRET=some-dev-secret-that-is-32-chars-long go run ./cmd/api
```

Users, products and orders are kept in memory and are lost on restart. The product seed is the same as the migration.

### Option C: API in Docker too

```bash
make db-up && make migrate && make docker-api-up
```

> No `make` on Windows? Every target is a one-line command in the [Makefile](Makefile), so you can run it directly in Git Bash.

---

## Tests

```bash
make test        # unit + handler tests, no database needed (integration tests self-skip)
make test-int    # migrates orders_test, then runs everything incl. Postgres integration tests
```

Integration tests only read `TEST_DATABASE_URL`, never `DATABASE_URL`, because their setup runs `TRUNCATE users CASCADE`. `docker compose` creates a separate `orders_test` database the first time the volume is initialised ([docker/initdb](docker/initdb/01-create-test-db.sql)).

| Suite | What it proves |
|---|---|
| [order/service_test.go](internal/order/service_test.go) | Invalid input is rejected **before any repository call** (checked with a spy). Total is computed as price × qty on the server. The same key with the same body replays the original order. The same key with a different body gives `ErrIdempotencyKeyReused`. Keys are scoped per user. **50 concurrent goroutines with one key create exactly 1 order**, including a variant that forces every goroutine past the fast path into the insert race. A replay after a price change keeps the original total. `Money` serialises as `198.00`. |
| [user/service_test.go](internal/user/service_test.go) | deviceId boundaries (15/16/128/129). Email normalisation and rejection of bad formats. Password boundaries (7/8/72/73 bytes). The repo receives a **bcrypt hash, never plaintext**. Duplicate email is rejected. Unknown email and wrong password give the same error. Guest→email link. |
| [auth/](internal/auth) | JWT round-trip. Rejects `alg=none`, RS256, wrong issuer, wrong signature, expired tokens. bcrypt salting and verification. |
| [httpapi/*_test.go](internal/httpapi) | Every error in §6.6 end-to-end through Fiber (`app.Test`), with the exact error envelope. The raw response bytes contain `"total":198.00`. The `Idempotent-Replayed` header is correct. 413 is tested over a real TCP listener. CORS exposes the idempotency headers. Rate limiting works. Fault injection is off in prod and off by default. The drop-after-commit → retry scenario works. |
| [order_integration_test.go](internal/httpapi/order_integration_test.go) | Real Postgres: **50 concurrent HTTP requests with one key → `SELECT count(*) FROM orders` = 1**. Full flow: register → login → order → replay → 422 → 404. The password is stored as bcrypt. Deleting a user makes their token return 401 (via the FK). Guest→email link keeps the same userId. |
| [config_test.go](internal/config/config_test.go) | Startup fails fast on `APP_ENV=prod` + fault injection, a short JWT secret, a missing DB URL, or invalid values. |

---

## API

Full reference with examples for every endpoint, error codes and a Unity C# sample (Thai): [Docs/api-reference.md](Docs/api-reference.md).

All routes are under `/api` except `/healthz`.

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/healthz` | – | pings the DB → `200 {"status":"ok"}` / `503 SERVICE_UNAVAILABLE` |
| POST | `/api/auth/guest` | – | `{"deviceId": "<16–128 chars>"}` → `200` |
| POST | `/api/auth/register` | – | `{"email","password"}` → `201`, rate-limited |
| POST | `/api/auth/login` | – | `{"email","password"}` → `200`, rate-limited |
| POST | `/api/auth/link` | Bearer | turns the current guest account into an email account; same `userId`, orders kept |
| GET | `/api/me` | Bearer | `{userId, accountType, email, createdAt}` |
| GET | `/api/products` | – | active products with `price` |
| **POST** | **`/api/orders`** | Bearer + `Idempotency-Key` | the assignment contract |
| GET | `/api/orders` | Bearer | the caller's latest 50 orders |

Auth responses: `{"userId","token","expiresAt","accountType":"guest"|"email"}`.

### `POST /api/orders`

```http
POST /api/orders
Authorization: Bearer <token>
Idempotency-Key: 3f2c1e9a-...          # ^[A-Za-z0-9_-]{8,128}$, a UUID works
Content-Type: application/json

{ "productId": "product-123", "quantity": 2 }
```

```http
HTTP/1.1 201 Created
Idempotent-Replayed: false

{ "id": "7f0c…", "productId": "product-123", "quantity": 2, "total": 198.00 }
```

Validation runs in this order, and all of it happens before any DB access: token → key present → key format → JSON body (strict, unknown fields rejected) → `productId` → `quantity`.

- **The client cannot send a price.** Any extra field such as `total` or `price` gives `400 INVALID_JSON` and the message names the field.
- `quantity` must be a plain JSON integer from 1 to `MAX_ORDER_QUANTITY` (99). The values `2.5`, `2.0`, `1e2`, `"2"`, `null` and a missing field are all rejected.
- Money is stored as integer cents and never goes through `float64`. `total` is written as a JSON number with two decimals.

**Idempotency**

- `(user_id, idempotency_key)` has a `UNIQUE` constraint. `INSERT … ON CONFLICT DO NOTHING` followed by a `SELECT` of the winning row is what prevents duplicates. It does not depend on Go locks or on the client disabling its button.
- Same key and same body: returns **`201` with the original order**, and `Idempotent-Replayed: true`. A client that timed out can retry without special handling.
- Same key with a different `productId`/`quantity`: returns `422 IDEMPOTENCY_KEY_REUSED` (a client bug).
- If the product is not found, the key is not consumed. The client can fix the request and retry with the same key.

### Error envelope & codes

```json
{ "error": { "code": "INVALID_QUANTITY", "message": "quantity must be a positive integer within the allowed maximum" } }
```

Clients should branch on **status + `code`**. `message` is for humans only. All codes are defined in [internal/httpapi/codes.go](internal/httpapi/codes.go).

| HTTP | codes |
|---|---|
| 400 | `INVALID_JSON`, `INVALID_DEVICE_ID`, `INVALID_EMAIL`, `WEAK_PASSWORD`, `MISSING_IDEMPOTENCY_KEY`, `INVALID_IDEMPOTENCY_KEY`, `INVALID_PRODUCT_ID`, `INVALID_QUANTITY`, `BAD_REQUEST` |
| 401 | `UNAUTHORIZED` (missing/invalid/expired token, or deleted account), `INVALID_CREDENTIALS` |
| 403 | `FORBIDDEN` (`ORDERS_REQUIRE_EMAIL_ACCOUNT=true` + guest token, or fault injection) |
| 404 | `NOT_FOUND` (route), `PRODUCT_NOT_FOUND` (missing or inactive) |
| 405 | `METHOD_NOT_ALLOWED` |
| 409 | `EMAIL_TAKEN`, `ALREADY_LINKED` |
| 413 | `PAYLOAD_TOO_LARGE` |
| 422 | `IDEMPOTENCY_KEY_REUSED` |
| 429 | `RATE_LIMITED` |
| 500 | `INTERNAL` (fixed message, no internal details) |
| 503 | `SERVICE_UNAVAILABLE` |

---

## Dev-only fault injection

Set `APP_ENV=dev` and `ENABLE_FAULT_INJECTION=true`. The server **refuses to start** if this is combined with `APP_ENV=prod`, and the router checks the setting again. Faults are triggered with the `X-Debug-Fault` header (comma-separated directives):

| Value | Effect |
|---|---|
| `delay=5s` | sleeps before the handler (max 10s), **then still processes the request** |
| `status=500` / `503` / `403` / `401` | returns that error immediately in the standard envelope; the handler does not run |
| `drop-after-commit` | runs the handler (the order is created), then returns `500`, which simulates a lost response |

---

## curl examples

```bash
BASE=http://localhost:8080

# guest
TOKEN=$(curl -s -X POST $BASE/api/auth/guest -H 'Content-Type: application/json' \
  -d '{"deviceId":"dev-device-0000000001"}' | jq -r .token)

# register / login
curl -s -X POST $BASE/api/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"player@example.com","password":"password123"}'
curl -s -X POST $BASE/api/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"player@example.com","password":"password123"}'

# create order: run twice with the same key → same id, 2nd has Idempotent-Replayed: true
KEY=$(uuidgen)
curl -i -X POST $BASE/api/orders \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":2}'

# invalid quantity → 400 INVALID_QUANTITY
curl -i -X POST $BASE/api/orders \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $(uuidgen)" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":0}'

# timeout demo (dev + ENABLE_FAULT_INJECTION=true): client gives up, server still creates the order
KEY2=$(uuidgen)
curl -i -m 2 -X POST $BASE/api/orders -H 'X-Debug-Fault: delay=5s' \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY2" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":2}'
sleep 4
# retry with the same key → 201, same order id, Idempotent-Replayed: true, still 1 row in the DB
curl -i -X POST $BASE/api/orders \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY2" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":2}'
```

Seeded products: `product-123` (99.00), `product-456` (49.50), `sword-001` (250.00), `retired-001` (inactive → 404).

---

## Configuration

| Env | Default | Notes |
|---|---|---|
| `PORT` | `8080` | |
| `APP_ENV` | `dev` | `dev` / `prod` |
| `DATABASE_URL` | required when `ORDER_REPO=postgres` | `postgres://orders:orders@localhost:5433/orders?sslmode=disable` |
| `TEST_DATABASE_URL` | – | integration tests only; point it at `orders_test` |
| `JWT_SECRET` | required, ≥ 32 chars | never commit a real value |
| `JWT_ISSUER` | `order-api` | |
| `JWT_TTL` | `168h` | |
| `BCRYPT_COST` | `12` | tests use `bcrypt.MinCost` |
| `MAX_BODY_BYTES` | `16384` | larger bodies → 413 |
| `MAX_ORDER_QUANTITY` | `99` | |
| `ORDER_REPO` | `postgres` | `memory` keeps **all** data (users too) in process |
| `ENABLE_FAULT_INJECTION` | `false` | dev only |
| `ORDERS_REQUIRE_EMAIL_ACCOUNT` | `false` | `true` → guest tokens get 403 on `POST /api/orders` |
| `AUTH_RATE_LIMIT_PER_MIN` | `10` | per IP, shared by login and register; `0` disables |
| `CORS_ALLOWED_ORIGINS` | `*` | allows `Idempotency-Key`, exposes `Idempotent-Replayed` |

---

## Project layout

```
cmd/api/main.go              wiring: config → store (postgres|memory) → services → router; graceful shutdown
internal/config              env loading + fail-fast validation
internal/auth                JWT (HS256 pinned, acct claim), bcrypt hasher
internal/user                user model, service (guest/register/login/link), repo interface, memory repo
internal/order               order model, Money, service (validation, pricing, idempotency), repo interfaces, memory repo
internal/store/postgres      GORM + raw parameterized SQL repositories
internal/httpapi             Fiber handlers, middleware, error envelope, fault injection
migrations                   goose SQL migrations (+ product seed)
```

Layering: `httpapi → service (user, order) → repository interface → postgres | memory`. Services do not import Fiber. Repositories contain no business rules.

---

## Assumptions

- A guest is identified by a client-generated `deviceId`, with no secret. Anyone who knows a deviceId can log in as that guest, which is acceptable for a demo guest flow.
- A replay returns the stored order unchanged, even if the product was later deactivated or its price changed.
- An idempotency key is scoped per user and does not expire.
- Emails are compared after trimming and lowercasing.

## Trade-offs (what I would revisit for production)

1. **Idempotency lives in the `orders` table.** There is no separate `idempotency_keys` table and no TTL. This is simple and correct for one endpoint. With many endpoints I would use a dedicated table storing request hash + status + response body with a ~24h expiry.
2. **Replay returns `201`**, not `200`/`409`. This keeps the client simple (Stripe-style), and the `Idempotent-Replayed` header shows what happened.
3. **JWT lasts 7 days with no refresh or revocation.** A deleted user's token keeps validating until it expires. Order creation still fails with 401 because of the FK check, but a production system needs short-lived access tokens plus refresh/revocation.
4. **Guest and email are separate accounts** unless the player calls `/api/auth/link`.
5. **No stock or payment.** The total is price × quantity at order time. The unit price is stored as a snapshot on the order.
6. **Register reveals whether an email exists** (`409 EMAIL_TAKEN`). Login is protected against enumeration (identical error and equalised bcrypt timing), but register is not. Production should use an email-verification flow.
7. **The rate limiter is in-memory and per instance.** Multiple replicas would need a shared store (e.g. Redis).

## Unfinished / future work

- Email verification, password reset, refresh tokens.
- Multi-item carts, stock reservation, payments, order cancellation.
- Metrics/tracing (only structured request logs exist today).
- The race detector (`go test -race`) was not run in the development environment because cgo was unavailable. Run it in CI on Linux.

---

## Handoff to Unity

- Base URL comes from config. The path is fixed: `POST /api/orders`.
- Get a token from `/api/auth/guest` or `/api/auth/login`.
- **Idempotency-Key:** generate `Guid.NewGuid().ToString()` once per *order attempt*. Retries (timeout, 5xx, network error) reuse the same key. A new order gets a new key.

| Backend | Unity UI state |
|---|---|
| 201 | Success: show `id`, `quantity`, `total` from the server |
| 400 `INVALID_*` | Validation error |
| 401 / 403 | Auth error |
| 404 `PRODUCT_NOT_FOUND` | Not found |
| 422 `IDEMPOTENCY_KEY_REUSED` | Client bug: show a general error and log it |
| 5xx | Server error; UI stays usable, Retry keeps the same key |
| timeout / connection error | Network error; Retry keeps the same key |

---

## AI usage note

**What AI was used for.** The design and plan came first ([Docs/order-backend-plan.md](Docs/order-backend-plan.md)): API contract, schema, idempotency algorithm, error codes and test list. Claude (Anthropic, via Claude Code) then generated the implementation from that plan: Go code, SQL migrations, tests, Docker/Makefile and this README. Some files were adapted from an earlier project of mine (JWT issuer, request logger, DB setup, Dockerfile).

**How it was verified.**

- `go vet ./...` is clean and `go test ./...` passes with no database.
- Integration tests were run against Postgres 16 in Docker. The 50-way concurrent test was repeated 10× and every run ended with exactly 1 row in `orders`.
- Migrations were checked with `goose up` → `reset` → `up`.
- The real binary was smoke-tested with curl, including the timeout demo: the client aborted at 1s, the server delayed 3s and committed, the retry returned `Idempotent-Replayed: true` with the original id, and the DB had exactly 1 row.
- I checked the logs for tokens and passwords (none found) and confirmed that startup is refused with `APP_ENV=prod` + fault injection.

**Bugs found during verification:**

- In Fiber v3, route handlers run in argument order. Code written as `Post(path, handler, middleware)` would have run the handler before authentication. Routes now list middleware first, and the no-bearer tests would catch a regression.
- The request logger inherited from the earlier project logged `200` for requests that actually got 404/413, because the error handler ran after the logger. The logger now renders errors first.
- `app.Test()` bypasses the body-size limit, so the 413 test runs over a real TCP listener.
