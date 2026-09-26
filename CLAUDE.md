# CLAUDE.md

Go backend for the Unity Order Integration assignment. The plan and contract are in `Docs/order-backend-plan.md`, and the README has the API reference.

## Stack
- Go 1.25, **Fiber v3** (`fiber.Ctx` is an interface; do not copy v2 snippets), GORM with **raw parameterized SQL** (`$1`), goose migrations (no AutoMigrate), Postgres 16 on host port **5433**, JWT HS256 (`golang-jwt/v5`), bcrypt, `log/slog` JSON.

## Commands
- `go test ./...`: no DB needed. Integration tests need `TEST_DATABASE_URL` (orders_test DB) and truncate tables.
- `make db-up && make migrate && make run`, or `ORDER_REPO=memory` to run with no DB.
- There is no `make` on the dev Windows box. Run the Makefile lines directly in Git Bash.

## Layering
`httpapi` (HTTP only) → `user` / `order` services (business rules, no Fiber import) → repository interfaces → `store/postgres` or `*/memory`.

## Invariants: do not break
- `POST /api/orders` contract: the response body is exactly `{id, productId, quantity, total}`, and `total` is `order.Money` (integer cents, marshals as `198.00`). Never use float math for money.
- The server computes the price. Strict JSON decoding (`DisallowUnknownFields`) rejects client-sent `total`/`price`.
- All validation happens before any DB access. Order: token → key present/format → JSON → productId → quantity.
- Idempotency is enforced by `UNIQUE (user_id, idempotency_key)` + `INSERT … ON CONFLICT DO NOTHING` + re-SELECT. A replay with the same body returns 201 + the original order + `Idempotent-Replayed: true`. A different body returns 422.
- **Fiber runs route handlers in argument order**: middleware (bearer, limiter) must come **before** the handler.
- Passwords are bcrypt-hashed in the service before reaching the repo. The repo only accepts hashes, and `User.PasswordHash` is `json:"-"`.
- Login failure returns the same `INVALID_CREDENTIALS` whether the email is unknown or the password is wrong, with a dummy bcrypt compare when the email is unknown.
- Never log passwords, hashes, JWTs, request bodies, the `Authorization` header or idempotency keys. `RequestLogger` logs only method/path/status/duration/ip.
- Error envelope: `{"error":{"code":"UPPER_SNAKE","message":"..."}}`. Codes live only in `internal/httpapi/codes.go`. 500 responses never leak details.
- Fault injection (`X-Debug-Fault`) is active only when `APP_ENV=dev && ENABLE_FAULT_INJECTION=true`. `config.Load` refuses prod+enabled.
- Secrets only in `.env`, which is gitignored. `.env.example` holds placeholders.
