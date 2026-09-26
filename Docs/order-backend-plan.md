# Order Backend Plan (Go) — สำหรับ Unity Order Integration Assignment

> เอกสารนี้ใช้เป็นแผนตั้งต้นของ **โปรเจคหลังบ้านใหม่** (แยก repo จากโปรเจคเดิม)
> ขอบเขตรอบนี้คือ **หลังบ้านอย่างเดียว** ส่วน Unity client ค่อยทำทีหลัง (ดู §12 ท้ายเอกสาร)
>
> ที่มา:
> - `Unity_Order_Integration_Follow_Up_Assignment_EN.pdf` — API contract ของ `POST /api/orders`, idempotency, error handling
> - โปรเจค `D:\Go-backend` — ใช้ stack, โครงสร้าง, และ pattern เดียวกัน (JWT, middleware, logging, error envelope, Docker)

---

## 1. เป้าหมาย

1. **Auth**
   - Guest login ด้วย `deviceId` (เหมือนโปรเจคเดิมเป๊ะ)
   - Email + password (register / login) — **ของใหม่**
   - ทั้งสองแบบได้ JWT HS256 แบบเดียวกัน (`sub=userID`, `iss`, `iat`, `exp`)
2. **Create Order** ตาม contract ในเอกสาร
   - `POST /api/orders` + `Authorization: Bearer` + `Idempotency-Key`
   - Server คำนวณ `total` เอง (client ห้ามส่งราคา)
   - Idempotency key เดียวกันต้องไม่สร้าง order ซ้ำ **แม้ส่งพร้อมกันหลาย request**
   - Error body สม่ำเสมอ ให้ Unity แยกกรณีได้
3. แยกชั้นชัดเจน: HTTP ↔ business rules ↔ persistence และ test ได้โดยไม่ต้องมี DB (fake repo)

### Non-goals (รอบนี้)
- Unity client (ทำหลังบ้านให้เสร็จก่อน)
- Payment, stock/inventory, cart หลายสินค้า, refund/cancel
- Refresh token (ใช้ JWT 7 วัน + login ใหม่ เหมือนโปรเจคเดิม)
- Email verification / reset password (ใส่ไว้ใน "Future work")

---

## 2. Stack (เหมือนโปรเจคเดิม)

| เรื่อง | ใช้ |
|---|---|
| ภาษา | Go 1.25 |
| HTTP | **Fiber v3** (`github.com/gofiber/fiber/v3`) — `fiber.Ctx` เป็น interface, **ห้ามก๊อป snippet v2** |
| DB access | **GORM** + `gorm.io/driver/postgres` แต่เขียน SQL ผ่าน `db.Raw` / `db.Exec` (parameterized `$1`, `$2`) สำหรับ query สำคัญ |
| Migrations | **goose** (`migrations/`) — ไม่ใช้ `AutoMigrate` |
| DB | Postgres 16 ผ่าน Docker Compose, host port **5433** (5432 ชนกับ Postgres ที่ลงใน Windows) + pgAdmin `:5050` |
| JWT | `github.com/golang-jwt/jwt/v5` (pin HS256) |
| Password | `golang.org/x/crypto/bcrypt` (cost 12) |
| Config | `github.com/joho/godotenv` + env vars |
| Logging | `log/slog` JSON |
| UUID | `github.com/google/uuid` |

สิ่งที่ **ก๊อปมาจากโปรเจคเดิมได้เกือบทั้งไฟล์** (เปลี่ยนแค่ module path):

| ไฟล์เดิม | ใช้ทำอะไร | ต้องแก้ |
|---|---|---|
| `internal/auth/jwt.go` + `jwt_test.go` | Issue/Parse JWT, pin HS256 | ไม่ต้องแก้ (หรือเพิ่ม claim `acct` ถ้าทำ §6.5) |
| `internal/httpapi/middleware.go` | `BearerAuth` | เปลี่ยน error code เป็น `UNAUTHORIZED` |
| `internal/httpapi/logging.go` | request log | ไม่ต้องแก้ |
| `internal/httpapi/respond.go` | error envelope | เปลี่ยนเป็น code แบบ UPPER_SNAKE (§7) |
| `internal/config/config.go` | โหลด env | เพิ่ม field ตาม §4 |
| `internal/store/postgres/db.go` | open GORM + ping | ไม่ต้องแก้ |
| `cmd/api/main.go` | wiring + graceful shutdown | เพิ่ม repo/service ใหม่ |
| `Dockerfile`, `docker-compose.yml`, `Makefile`, `pgadmin/servers.json` | dev env | เปลี่ยนชื่อ DB/user เป็น `orders` |

---

## 3. โครงสร้างโปรเจค

```
order-backend/
├── cmd/api/main.go                 # wiring: config → db → repos → services → router, graceful shutdown
├── internal/
│   ├── config/config.go
│   ├── auth/
│   │   ├── jwt.go                  # Issuer (ก๊อปจากเดิม)
│   │   └── password.go             # bcrypt Hash / Compare
│   ├── user/
│   │   ├── model.go                # User{ID, DeviceID*, Email*, PasswordHash*, ...}
│   │   ├── errors.go               # ErrInvalidDeviceID, ErrInvalidEmail, ErrWeakPassword, ErrEmailTaken, ErrInvalidCredentials
│   │   ├── repository.go           # interface
│   │   ├── service.go              # LoginGuest, Register, LoginEmail
│   │   └── service_test.go         # ใช้ fake repo
│   ├── order/
│   │   ├── model.go                # Order, Product, Money
│   │   ├── errors.go               # ErrInvalidProductID, ErrInvalidQuantity, ErrInvalidIdempotencyKey, ErrProductNotFound, ErrIdempotencyKeyReused
│   │   ├── repository.go           # ProductRepository, OrderRepository (interfaces)
│   │   ├── service.go              # CreateOrder (validation + idempotency rule + total)
│   │   ├── memory/                 # in-memory fake repo (ใช้ทั้ง test และ demo mode ได้)
│   │   │   └── repo.go
│   │   └── service_test.go
│   ├── store/postgres/
│   │   ├── db.go
│   │   ├── users.go
│   │   ├── products.go
│   │   └── orders.go
│   └── httpapi/
│       ├── router.go
│       ├── respond.go
│       ├── logging.go
│       ├── middleware.go           # BearerAuth
│       ├── faults.go               # dev-only fault injection (§9)
│       ├── auth_handler.go
│       ├── order_handler.go
│       ├── product_handler.go
│       ├── *_test.go               # handler tests ด้วย app.Test() + fake services/repos
│       └── order_integration_test.go  # ต้องมี TEST_DATABASE_URL
├── migrations/
│   ├── 00001_users.sql
│   ├── 00002_products_orders.sql
│   └── 00003_seed_products.sql
├── Docs/
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── .env.example                    # ห้ามมี secret จริง
└── README.md
```

**กฎการแบ่งชั้น (เหมือนเดิม):**
```
handler (httpapi) → service (user, order) → repository (interface) → postgres / memory
```
- Handler จัดการ HTTP เท่านั้น: อ่าน header/body, แปลง error → status + code
- Service ถือ business rule: validation, คำนวณ total, กฎ idempotency (fingerprint ต้องตรง)
- Repository ไม่มี business rule — แค่ SQL / map
- Service ต้องไม่ import `fiber`

---

## 4. Config (env)

| Env | Default | หมายเหตุ |
|---|---|---|
| `PORT` | `8080` | |
| `APP_ENV` | `dev` | `dev` / `prod` — fault injection เปิดได้เฉพาะ `dev` |
| `DATABASE_URL` | (required) | `postgres://orders:orders@localhost:5433/orders?sslmode=disable` |
| `JWT_SECRET` | (required, ≥ 32 chars) | **ห้าม commit ค่าจริง** |
| `JWT_ISSUER` | `order-api` | |
| `JWT_TTL` | `168h` | |
| `BCRYPT_COST` | `12` | test ใช้ `bcrypt.MinCost` เพื่อความเร็ว |
| `MAX_BODY_BYTES` | `16384` | request order/auth เล็กมาก |
| `MAX_ORDER_QUANTITY` | `99` | กัน overflow + กันกดเลขมั่ว |
| `ORDER_REPO` | `postgres` | `postgres` / `memory` (demo โดยไม่ต้องมี DB — assignment อนุญาต) |
| `ENABLE_FAULT_INJECTION` | `false` | ใช้ได้เมื่อ `APP_ENV=dev` เท่านั้น |
| `CORS_ALLOWED_ORIGINS` | `*` | สำคัญเฉพาะ WebGL |

`config.Load()` ต้อง fail-fast ถ้า `APP_ENV=prod` แต่ `ENABLE_FAULT_INJECTION=true`

---

## 5. Database schema (goose)

### `00001_users.sql`

```sql
-- +goose Up
CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     TEXT        UNIQUE,                 -- guest
    email         TEXT        UNIQUE,                 -- เก็บแบบ normalize แล้ว (trim + lowercase)
    password_hash TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_identity_chk CHECK (
        device_id IS NOT NULL
        OR (email IS NOT NULL AND password_hash IS NOT NULL)
    ),
    CONSTRAINT users_email_pw_chk CHECK ((email IS NULL) = (password_hash IS NULL))
);

-- +goose Down
DROP TABLE users;
```

> Guest กับ email เป็นคนละแถวกัน (ยังไม่มี "link guest → email" ในรอบแรก ดู §6.4)

### `00002_products_orders.sql`

```sql
-- +goose Up
CREATE TABLE products (
    id          TEXT        PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    name        TEXT        NOT NULL,
    price_cents BIGINT      NOT NULL CHECK (price_cents >= 0),
    active      BOOLEAN     NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id       TEXT        NOT NULL REFERENCES products(id),
    quantity         INT         NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT      NOT NULL CHECK (unit_price_cents >= 0),  -- snapshot ราคา ณ ตอนสั่ง
    total_cents      BIGINT      NOT NULL CHECK (total_cents >= 0),
    idempotency_key  TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT orders_user_idem_uniq UNIQUE (user_id, idempotency_key)
);

CREATE INDEX orders_user_created_idx ON orders (user_id, created_at DESC);

-- +goose Down
DROP TABLE orders;
DROP TABLE products;
```

**จุดสำคัญ**
- เงินเก็บเป็น **integer cents** (`BIGINT`) ห้ามใช้ float ในการคำนวณ
- `UNIQUE (user_id, idempotency_key)` คือ **ตัวกันซ้ำจริง** — ไม่ได้พึ่ง lock ใน Go, ไม่ได้พึ่งการ disable ปุ่มฝั่ง client
- Key ผูกกับ user (scope per user) → user A ใช้ key ชนกับ user B ได้โดยไม่กระทบกัน
- เก็บ `unit_price_cents` ไว้ เพื่อให้ replay คืนค่าเดิมแม้ราคาสินค้าเปลี่ยนภายหลัง

### `00003_seed_products.sql`

```sql
-- +goose Up
INSERT INTO products (id, name, price_cents) VALUES
    ('product-123', 'Health Potion', 9900),    -- 2 ชิ้น = 198.00 ตรงกับตัวอย่างในเอกสาร
    ('product-456', 'Mana Potion',   4950),
    ('sword-001',   'Iron Sword',   25000),
    ('retired-001', 'Old Item',      1000);
UPDATE products SET active = false WHERE id = 'retired-001';

-- +goose Down
DELETE FROM products WHERE id IN ('product-123','product-456','sword-001','retired-001');
```

---

## 6. API

Prefix ทั้งหมดเป็น `/api` (ยกเว้น `/healthz`)

| Method | Path | Auth | สถานะ |
|---|---|---|---|
| `GET`  | `/healthz` | – | ต้อง ping DB |
| `POST` | `/api/auth/guest` | – | **ต้องมี** |
| `POST` | `/api/auth/register` | – | **ต้องมี** |
| `POST` | `/api/auth/login` | – | **ต้องมี** |
| `GET`  | `/api/me` | Bearer | ควรมี (ไว้ทดสอบ token) |
| `GET`  | `/api/products` | – | ควรมี (ให้ Unity/curl รู้ product id) |
| `POST` | `/api/orders` | Bearer + Idempotency-Key | **ต้องมี — contract หลักของเอกสาร** |
| `GET`  | `/api/orders` | Bearer | optional (list order ของตัวเอง) |
| `POST` | `/api/auth/link` | Bearer (guest) | stretch (§6.4) |

### 6.1 `POST /api/auth/guest`

Request:
```json
{ "deviceId": "a1b2c3d4e5f6g7h8i9j0" }
```
- `deviceId` ยาว 16–128 ตัว
- Upsert (canonical SQL เดิม):
  ```sql
  INSERT INTO users (device_id) VALUES ($1)
  ON CONFLICT (device_id) DO UPDATE SET last_login_at = now()
  RETURNING id, device_id, email, created_at, last_login_at;
  ```

Response `200`:
```json
{ "userId": "uuid", "token": "jwt", "expiresAt": "2026-10-03T00:00:00Z", "accountType": "guest" }
```

### 6.2 `POST /api/auth/register`

Request:
```json
{ "email": "Player@Example.com", "password": "correct horse battery" }
```
Validation (ใน service, ก่อนแตะ DB):
- Email: `strings.TrimSpace` + `strings.ToLower`, ≤ 254 ตัว, ผ่าน `net/mail.ParseAddress` และ `addr.Address == input` (กันรูปแบบ `"Name <a@b>"`)
- Password: **8–72 bytes** (bcrypt ตัดที่ 72 bytes → ปฏิเสธถ้ายาวเกิน แทนที่จะตัดเงียบๆ)

**Hash password ก่อนเก็บเสมอ** — service เรียก `auth.HashPassword` แล้วส่งแค่ hash ให้ repo
(repo interface รับ `passwordHash string` เท่านั้น ไม่มีช่องให้ส่ง plaintext เข้าไปได้เลย)

```go
// internal/auth/password.go
type PasswordHasher struct{ cost int } // cost จาก BCRYPT_COST (prod 12, test bcrypt.MinCost)

func (h PasswordHasher) Hash(plain string) (string, error) {
    b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost) // salt สุ่มในตัว, ได้ "$2a$12$..."
    if err != nil {
        return "", fmt.Errorf("hash password: %w", err)
    }
    return string(b), nil
}

func (h PasswordHasher) Compare(hash, plain string) bool {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil // constant-time
}
```

Flow ใน `user.Service.Register`:
```
validate(email, password)            // ไม่แตะ DB
hash := hasher.Hash(password)        // plaintext หยุดอยู่ตรงนี้
repo.CreateEmailUser(email, hash)    // DB เห็นแค่ hash
```

SQL:
```sql
INSERT INTO users (email, password_hash) VALUES ($1, $2)   -- $2 = bcrypt hash
RETURNING id, email, created_at, last_login_at;
```
- Unique violation (`23505`) บน `users_email_key` → `409 EMAIL_TAKEN`

Response `201`: shape เดียวกับ guest, `accountType: "email"` (register แล้วได้ token เลย ไม่ต้อง login ซ้ำ)

### 6.3 `POST /api/auth/login`

Request: `{ "email": "...", "password": "..." }`

- หา user ด้วย email ที่ normalize แล้ว
- **ไม่พบ user หรือ password ผิด → `401 INVALID_CREDENTIALS` เหมือนกันทุกกรณี** (กัน user enumeration)
- ตรวจ password ด้วย `hasher.Compare(user.PasswordHash, input)` เท่านั้น — ห้าม hash ใหม่แล้วเทียบ string (bcrypt salt ต่างกันทุกครั้ง)
- ถ้าไม่พบ user ให้ยังเรียก `hasher.Compare` กับ dummy hash อยู่ดี (กัน timing attack แยกได้ว่า email มีจริงไหม)
- สำเร็จ → `UPDATE users SET last_login_at = now() WHERE id = $1` แล้วออก JWT

Response `200`: shape เดียวกับ guest, `accountType: "email"`

> Trade-off ที่ต้องเขียนใน README: `register` ตอบ `409 EMAIL_TAKEN` ก็เปิดเผยได้อยู่ดีว่า email มีในระบบ — ยอมรับได้สำหรับ demo, production ควรทำ email verification flow

### 6.4 (Stretch) `POST /api/auth/link`
Guest ที่ login อยู่ ผูก email/password เข้ากับแถวเดิม → `userId` ไม่เปลี่ยน, order เดิมยังอยู่ (password hash ก่อนเหมือน §6.2, `$3` = bcrypt hash)
```sql
UPDATE users SET email = $2, password_hash = $3
WHERE id = $1 AND email IS NULL
RETURNING ...;
```
0 rows → `409 ALREADY_LINKED`, unique violation → `409 EMAIL_TAKEN` — **ทำเมื่อ core เสร็จแล้วเท่านั้น**

### 6.5 (Optional) Claim `acct` ใน JWT
ถ้าอยากโชว์เคส **403** ให้ Unity: เพิ่ม custom claim `acct: "guest" | "email"` แล้ว config `ORDERS_REQUIRE_EMAIL_ACCOUNT=true` → guest สั่งของได้ `403 FORBIDDEN`
ถ้าไม่ทำ ให้ Unity จำลอง 403 ผ่าน fault injection (§9) แทน

### 6.6 `POST /api/orders` ⭐ (contract ตามเอกสาร — ห้ามเปลี่ยน)

| Item | ค่า |
|---|---|
| Method / Path | `POST /api/orders` |
| Content-Type | `application/json` |
| Auth | `Authorization: Bearer <token>` |
| Idempotency | `Idempotency-Key: <key>` |
| Success | `201` + order ที่สร้าง |

Request:
```json
{ "productId": "product-123", "quantity": 2 }
```

Response `201`:
```json
{ "id": "7f0c...uuid", "productId": "product-123", "quantity": 2, "total": 198.00 }
```
Header เพิ่ม (ไม่ผิด contract): `Idempotent-Replayed: true|false`

#### ลำดับการ validate (ทั้งหมดต้องเกิด **ก่อนแตะ DB**)

1. `BearerAuth` middleware → ไม่มี/ผิด/หมดอายุ → `401 UNAUTHORIZED` (JWT stateless ไม่ต้องใช้ DB)
2. `Idempotency-Key` header
   - ไม่มี → `400 MISSING_IDEMPOTENCY_KEY`
   - ต้องตรง `^[A-Za-z0-9_-]{8,128}$` (UUID ผ่าน) → ไม่งั้น `400 INVALID_IDEMPOTENCY_KEY`
3. Body
   - Content-Type ไม่ใช่ JSON / JSON พัง → `400 INVALID_JSON`
   - ใช้ `json.Decoder` + `DisallowUnknownFields()` + `UseNumber()` บน `c.Body()` (ไม่ใช้ `c.Bind().JSON` เพราะต้องคุมละเอียด)
   - มี field อื่นเช่น `total`, `price` → `400 INVALID_JSON` ("unknown field") — **ย้ำว่า client ส่งราคาไม่ได้**
4. `productId`: ต้องเป็น string, ตรง `^[A-Za-z0-9_-]{1,64}$` → ไม่งั้น `400 INVALID_PRODUCT_ID`
5. `quantity`: ต้องเป็น **JSON integer** 1..`MAX_ORDER_QUANTITY`
   - `2.5`, `"2"`, `0`, `-1`, `null`, หายไป, `1e2` → `400 INVALID_QUANTITY`
   - Decode เป็น `json.Number` แล้ว `strconv.ParseInt(string(n), 10, 64)` (ปฏิเสธ `2.0` / `1e2` ไปด้วย ให้ strict)

> ข้อ 4–5 อยู่ใน `order.Service.CreateOrder` (business rule) — handler แค่แปลง shape
> ข้อ 2 ตรวจ format ใน service เช่นกัน (handler แค่ดึง header)

#### Idempotency algorithm (ใน service)

```
CreateOrder(ctx, userID, key, productID, qty):
  validate(key, productID, qty)                                  // ไม่แตะ DB

  // 1) replay เร็ว: key นี้เคยใช้แล้วไหม
  if existing := orders.FindByKey(userID, key); existing != nil:
      return checkFingerprint(existing, productID, qty)          // replay หรือ 422

  // 2) ราคาจาก server เท่านั้น
  product := products.FindActive(productID)
  if product == nil: return ErrProductNotFound                   // 404, key ไม่ถูก "ใช้"

  total := product.PriceCents * qty                              // int64, qty ≤ 99 ไม่ overflow

  // 3) insert แบบ atomic — UNIQUE constraint ตัดสินผู้ชนะ
  order, created := orders.InsertIfAbsent(userID, key, productID, qty, product.PriceCents, total)
  if !created:                                                   // แพ้ race กับ request พร้อมกัน
      return checkFingerprint(order, productID, qty)

  return order, replayed=false

checkFingerprint(o, productID, qty):
  if o.ProductID != productID || o.Quantity != qty:
      return ErrIdempotencyKeyReused                             // 422
  return o, replayed=true                                        // 201 + body เดิม
```

SQL ของ `InsertIfAbsent` (Postgres repo):
```sql
INSERT INTO orders (user_id, product_id, quantity, unit_price_cents, total_cents, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, idempotency_key) DO NOTHING
RETURNING id, user_id, product_id, quantity, unit_price_cents, total_cents, idempotency_key, created_at;
```
ถ้าไม่มี row กลับมา → `SELECT ... FROM orders WHERE user_id = $1 AND idempotency_key = $2`

**ทำไมถูกต้องเมื่อมี request พร้อมกัน:** Postgres ให้ `INSERT ... ON CONFLICT` ตัวที่สองรอ transaction แรก commit แล้วค่อย `DO NOTHING` → `SELECT` ถัดไป (READ COMMITTED, statement ใหม่) เห็น row ที่ commit แล้วเสมอ → ได้ order เดียว

**ทำไม replay ตอบ `201` ไม่ใช่ `200`/`409`:** Unity ที่ timeout แล้ว retry ด้วย key เดิม ควรได้ผลเหมือนครั้งแรกทุกประการ → client ไม่ต้องมี branch พิเศษ (แนวเดียวกับ Stripe) ส่วน `Idempotent-Replayed` header มีไว้ debug/log

**ใช้ key เดิมกับ body ต่างกัน → `422 IDEMPOTENCY_KEY_REUSED`** เพราะเป็น bug ฝั่ง client ไม่ใช่ conflict ของ resource

#### Money JSON
```go
type Money int64 // cents

func (m Money) MarshalJSON() ([]byte, error) {
    neg := m < 0
    if neg { m = -m }
    s := fmt.Sprintf("%d.%02d", int64(m)/100, int64(m)%100)
    if neg { s = "-" + s }
    return []byte(s), nil // เป็น JSON number: 198.00
}
```
→ ได้ `"total": 198.00` ตรงตัวอย่างเป๊ะ และไม่มี float error

#### Error mapping ของ order

| Error (service) | HTTP | code |
|---|---|---|
| (middleware) | 401 | `UNAUTHORIZED` |
| `ErrMissingIdempotencyKey` | 400 | `MISSING_IDEMPOTENCY_KEY` |
| `ErrInvalidIdempotencyKey` | 400 | `INVALID_IDEMPOTENCY_KEY` |
| JSON พัง / unknown field | 400 | `INVALID_JSON` |
| `ErrInvalidProductID` | 400 | `INVALID_PRODUCT_ID` |
| `ErrInvalidQuantity` | 400 | `INVALID_QUANTITY` |
| `ErrProductNotFound` (ไม่มี หรือ `active=false`) | 404 | `PRODUCT_NOT_FOUND` |
| `ErrIdempotencyKeyReused` | 422 | `IDEMPOTENCY_KEY_REUSED` |
| FK violation `user_id` (user ถูกลบแต่ token ยังไม่หมดอายุ) | 401 | `UNAUTHORIZED` |
| body เกิน `MAX_BODY_BYTES` | 413 | `PAYLOAD_TOO_LARGE` |
| อื่นๆ | 500 | `INTERNAL` (message คงที่ ห้ามหลุด detail) |

---

## 7. Error envelope & codes

รูปแบบเดียวทุก endpoint (เหมือนเดิม แต่ code เป็น **UPPER_SNAKE** ตามตัวอย่างในเอกสาร):
```json
{ "error": { "code": "INVALID_QUANTITY", "message": "Quantity must be a positive integer" } }
```

| HTTP | codes |
|---|---|
| 400 | `INVALID_JSON`, `INVALID_DEVICE_ID`, `INVALID_EMAIL`, `WEAK_PASSWORD`, `MISSING_IDEMPOTENCY_KEY`, `INVALID_IDEMPOTENCY_KEY`, `INVALID_PRODUCT_ID`, `INVALID_QUANTITY` |
| 401 | `UNAUTHORIZED`, `INVALID_CREDENTIALS` |
| 403 | `FORBIDDEN` (§6.5 หรือ fault injection) |
| 404 | `NOT_FOUND` (route), `PRODUCT_NOT_FOUND` |
| 409 | `EMAIL_TAKEN`, `ALREADY_LINKED` |
| 413 | `PAYLOAD_TOO_LARGE` |
| 422 | `IDEMPOTENCY_KEY_REUSED` |
| 500 | `INTERNAL` |
| 503 | `SERVICE_UNAVAILABLE` (healthz) |

- เก็บ code เป็น `const` ใน `httpapi/codes.go` ไฟล์เดียว → ภายหลังก๊อปไปเป็น enum ฝั่ง Unity ได้ตรงๆ
- Fiber `ErrorHandler` กลาง (แบบ `router.go` เดิม) map `*fiber.Error` → code เหล่านี้ (เช่น 404 route ไม่มี, 413, 405)
- `message` เป็นข้อความสำหรับคน, **Unity ต้อง branch ด้วย `code` + status เท่านั้น**

---

## 8. Security & logging invariants

- JWT parse **pin HS256** (`jwt.WithValidMethods`), ตรวจ `iss`, `exp` required — ใช้ `jwt.go` เดิม
- **Password ต้อง hash ด้วย bcrypt ก่อนเก็บเสมอ** — DB มีแค่คอลัมน์ `password_hash`, ห้ามเก็บ/ส่ง plaintext ลง repo, ห้าม return hash ออกทาง API (`User` struct ใส่ `json:"-"` ที่ `PasswordHash`)
- SQL ทุกตัว parameterized (`$1`) — ห้ามต่อ string (นี่คือ bug หลักของโค้ด live-coding ต้นฉบับ)
- **ห้าม log**: password, password hash, JWT, request body, header `Authorization`
  - `RequestLogger` เดิม log แค่ method/path/status/duration_ms/ip → คงไว้แบบนั้น
  - `Idempotency-Key` ไม่ใช่ secret แต่ก็ไม่ต้อง log (ถ้าจะ log ตอน debug ให้ log แค่ใน service level DEBUG)
- `.env.example` ใส่ค่า placeholder เท่านั้น, `.env` อยู่ใน `.gitignore`
- CORS `AllowHeaders`: `Content-Type, Authorization, Idempotency-Key` และ `ExposeHeaders`: `Idempotent-Replayed`
- (ควรมี) Fiber `limiter` middleware บน `/api/auth/login` และ `/api/auth/register` เช่น 10 req/นาที/IP → `429 RATE_LIMITED`

**Middleware chain:** `recover` → `RequestLogger` → `cors` → (dev) `FaultInjection` → routes
`BearerAuth` ใส่ per-group (`/api/me`, `/api/orders`)

---

## 9. Dev-only fault injection (เตรียมไว้ให้ Unity demo)

เอกสารให้ demo "failure แล้ว retry" + timeout/5xx/401/403 ฝั่ง Unity → ทำ middleware ไว้เลยตั้งแต่ตอนนี้ **เปิดเฉพาะ `APP_ENV=dev` && `ENABLE_FAULT_INJECTION=true`**

Header `X-Debug-Fault`:

| ค่า | ผล |
|---|---|
| `delay=5s` | sleep ก่อน handler (ให้ Unity timeout) — **แต่ยังประมวลผล order ต่อ** |
| `status=500` / `503` / `403` | ตอบ error ทันทีด้วย envelope มาตรฐาน ไม่แตะ handler |
| `drop-after-commit` | สร้าง order สำเร็จแล้วค่อยตอบ 500 (จำลอง "server ทำแล้วแต่ response หาย") |

Scenario สำคัญที่จะโชว์: `delay=5s` + Unity timeout 3s → order ถูกสร้างจริงแล้ว → Unity retry ด้วย key เดิม → ได้ `201` + `Idempotent-Replayed: true` + **order id เดิม** → DB มี 1 แถว

---

## 10. Testing

`go test ./...` ต้องผ่านโดย **ไม่มี DB** — integration test skip เองถ้าไม่มี `TEST_DATABASE_URL`

### 10.1 Unit — `order/service_test.go` (fake repo)
- [ ] Invalid input (`quantity` = 0, -1, > max; `productId` ว่าง/ยาว/มีอักขระแปลก; key สั้น/มีช่องว่าง) → error ถูกตัว และ **spy repo นับ call = 0**
- [ ] Valid → total = price × qty, ส่ง userID/productID/qty/key ถูกต้องเข้า repo
- [ ] Product ไม่มี / inactive → `ErrProductNotFound`, ไม่มี insert
- [ ] Key เดิม + body เดิม → order เดิม, `replayed=true`, ไม่ insert ซ้ำ
- [ ] Key เดิม + body ต่าง → `ErrIdempotencyKeyReused`
- [ ] Key เดียวกันคนละ user → สร้างได้ทั้งคู่
- [ ] **Concurrent**: 50 goroutines, key เดียวกัน → สร้าง 1 order, ทุกตัวได้ order id เดียวกัน (fake repo ต้องใช้ `sync.Mutex` จำลอง unique constraint)
- [ ] Replay หลังราคาสินค้าเปลี่ยน → total เดิม

### 10.2 Unit — `user/service_test.go`
- [ ] deviceId 15/16/128/129 ตัว
- [ ] Email normalize (`  A@B.com ` → `a@b.com`), email ผิดรูป
- [ ] Password 7 / 8 / 72 / 73 bytes
- [ ] Register ซ้ำ → `ErrEmailTaken`
- [ ] Login: email ไม่มี กับ password ผิด → ได้ `ErrInvalidCredentials` ตัวเดียวกัน
- [ ] Register แล้ว fake repo ได้รับ **hash ไม่ใช่ plaintext** (`hash != password`, ขึ้นต้น `$2a$`, `Compare(hash, password) == true`)
- [ ] Hash password เดียวกันสองครั้งได้ค่าต่างกัน (salt) แต่ `Compare` ผ่านทั้งคู่
- [ ] ใช้ `bcrypt.MinCost` ใน test

### 10.3 Unit — `auth/jwt_test.go` (ก๊อปของเดิม)
- [ ] alg=none, RS256, issuer ผิด, หมดอายุ → reject

### 10.4 Handler — `httpapi/*_test.go` (`app.Test()` + fake repos, ไม่มี DB)
- [ ] ไม่มี Bearer → 401 `UNAUTHORIZED`
- [ ] ไม่มี `Idempotency-Key` → 400 `MISSING_IDEMPOTENCY_KEY`
- [ ] `{"productId":"product-123","quantity":2.5}` → 400 `INVALID_QUANTITY`
- [ ] `{"productId":"product-123","quantity":2,"total":1}` → 400 `INVALID_JSON`
- [ ] Valid → 201, body `total` เป็น `198.00` (เช็ค raw bytes), header `Idempotent-Replayed: false`
- [ ] ยิงซ้ำ → 201, id เดิม, `Idempotent-Replayed: true`
- [ ] ทุก error body ตรง envelope (`error.code`, `error.message`)
- [ ] Fault injection ปิดอยู่เมื่อ `APP_ENV=prod`

### 10.5 Integration — Postgres (`TEST_DATABASE_URL`)
- [ ] Concurrent 50 goroutines ยิง HTTP key เดียวกัน → `SELECT count(*) FROM orders` = 1
- [ ] Register → login → create order → replay → end-to-end
- ⚠️ Integration test `TRUNCATE users CASCADE` ตอน setup → **ใช้ DB แยก** (`orders_test`) อย่าชี้ไป dev DB

---

## 11. Phases (checklist)

### Phase 0 — Scaffold
- [ ] `go mod init order-backend`, ติดตั้ง fiber v3, gorm, driver/postgres, jwt/v5, godotenv, uuid, x/crypto
- [ ] ก๊อป `config`, `auth/jwt.go`, `httpapi/{logging,middleware,respond}.go`, `store/postgres/db.go`, `main.go` จากโปรเจคเดิม แล้วแก้ module path
- [ ] `docker-compose.yml` (db `5433:5432`, pgadmin `5050`, api หลัง `profiles: ["api"]`), `Dockerfile`, `Makefile`, `.env.example`
- [ ] `GET /healthz` ทำงาน

### Phase 1 — DB
- [ ] Migrations 00001–00003, `make migrate` ผ่าน, ดูใน pgAdmin ได้

### Phase 2 — Auth
- [ ] `user` service + repo interface + fake + tests (§10.2)
- [ ] `auth/password.go` (bcrypt `Hash` / `Compare`) — hash ก่อนเก็บทุกครั้ง
- [ ] Postgres `UserRepo` (guest upsert, create email, find by email, touch last_login)
- [ ] Handlers `/api/auth/guest`, `/api/auth/register`, `/api/auth/login`, `/api/me`
- [ ] Rate limit บน login/register

### Phase 3 — Order domain (ไม่มี DB)
- [ ] `order` model, errors, `Money`, repository interfaces
- [ ] `order/memory` fake repo (thread-safe)
- [ ] `order.Service.CreateOrder` ตาม §6.6
- [ ] Tests §10.1 ผ่านทั้งหมด

### Phase 4 — Order persistence
- [ ] Postgres `ProductRepo.FindActive`, `OrderRepo.FindByKey`, `OrderRepo.InsertIfAbsent`
- [ ] `ORDER_REPO=memory|postgres` wiring ใน `main.go`
- [ ] Integration tests §10.5

### Phase 5 — HTTP
- [ ] `POST /api/orders` handler + error mapping §6.6
- [ ] `GET /api/products`, (optional) `GET /api/orders`
- [ ] Central `ErrorHandler` + `codes.go`
- [ ] CORS headers (`Idempotency-Key`, expose `Idempotent-Replayed`)
- [ ] Handler tests §10.4

### Phase 6 — Dev tooling & docs
- [ ] Fault injection middleware §9 + test
- [ ] README: setup, วิธีรันหลังบ้าน (Docker / `go run`), วิธีรัน test, curl ตัวอย่าง, assumptions, trade-offs, unfinished work
- [ ] **AI usage note** (เอกสารบังคับ): ใช้ AI ส่วนไหน และ verify ยังไง (test ไหนพิสูจน์อะไร)
- [ ] `CLAUDE.md` ของ repo ใหม่ (สรุป invariants §8 + stack)

### Phase 7 — Unity (ภายหลัง) → ดู §12

---

## 12. Handoff ให้ฝั่ง Unity (ยังไม่ทำตอนนี้ — จดไว้กันลืม)

สิ่งที่หลังบ้านรับประกันให้ Unity:
- Base URL มาจาก config, path ตายตัว `POST /api/orders`
- Token ได้จาก `/api/auth/guest` หรือ `/api/auth/login` → Unity ทำ `ITokenProvider` แยก
- Key: Unity สร้าง `Guid.NewGuid().ToString()` ต่อ "order attempt" หนึ่งครั้ง, retry ใช้ key เดิม, order ใหม่ค่อยสร้างใหม่
- Mapping status → UI state ที่เอกสารกำหนด:

| Backend | Unity state |
|---|---|
| 201 | Success (แสดง `id`, `quantity`, `total` จาก server) |
| 400 `INVALID_*` | Validation error (ปกติ Unity ควรจับได้ก่อนส่ง) |
| 401 / 403 | Auth error |
| 404 `PRODUCT_NOT_FOUND` | Not found |
| 422 `IDEMPOTENCY_KEY_REUSED` | Bug ฝั่ง client → แสดงเป็น general error + log |
| 5xx | Server error, UI กลับมาใช้งานได้ |
| timeout / connection error | Network error, ปุ่ม Retry ใช้ key เดิม |

---

## 13. ตัวอย่าง curl (ใส่ใน README)

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

# create order (ยิงสองรอบด้วย key เดิม → ได้ id เดิม)
KEY=$(uuidgen)
curl -i -X POST $BASE/api/orders \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":2}'

# invalid quantity
curl -i -X POST $BASE/api/orders \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $(uuidgen)" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":0}'

# จำลอง timeout (dev เท่านั้น)
curl -i -m 2 -X POST $BASE/api/orders -H 'X-Debug-Fault: delay=5s' \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":2}'
```

---

## 14. Trade-offs ที่ต้องพร้อมอธิบาย (เอกสารขอ "one trade-off you would revisit for production")

1. **Idempotency เก็บในตาราง `orders` เลย** (ไม่มีตาราง `idempotency_keys` แยก, ไม่มี TTL) — ง่ายและถูกต้องสำหรับ endpoint เดียว; production ที่มีหลาย endpoint ควรแยกตาราง เก็บ response + status + request hash + หมดอายุ 24 ชม.
2. **Replay คืน `201`** แทน `200` — ง่ายสำหรับ client แต่ต่างจากบาง API
3. **JWT 7 วันไม่มี refresh/revoke** — ถ้า user ถูกลบ token ยังใช้ได้จนหมดอายุ (เราจับได้แค่ตอน FK fail)
4. **Guest กับ email แยกบัญชี** — ผู้เล่น guest ที่ register ทีหลังจะเสีย order เดิม จนกว่าจะทำ `/api/auth/link`
5. **ไม่มี stock/payment** — total คำนวณจากราคาปัจจุบันเท่านั้น
6. **Register บอกว่า email ซ้ำ** (enumeration) — production ควรใช้ email verification
