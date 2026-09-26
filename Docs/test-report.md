# Test Report — รันใหม่ทั้งหมดบน DB ที่ล้างสะอาด

| | |
|---|---|
| วันที่รัน | 2026-09-26 (เวลา 15:35–15:37 น. +07:00) |
| เครื่อง | Windows 11, Go 1.25.0, Docker 28.2.2, Postgres 16 (`postgres:16-alpine`) |
| ผลรวม | ✅ **ผ่านทั้งหมด:** Go tests 92/92 case, e2e 52/52 check, concurrency ซ้ำ 10 รอบผ่านทุกรอบ |
| Log ดิบ | [Docs/test-run/](test-run/) |

---

## 1. ขั้นตอนที่ทำ

| # | ขั้นตอน | คำสั่ง | Log |
|---|---|---|---|
| 1 | ลบทุกอย่าง: container + volume (DB `orders` และ `orders_test` หายทั้งหมด) | `docker compose --profile api down -v` | [01-reset-and-migrate.log](test-run/01-reset-and-migrate.log) |
| 2 | เปิด Postgres + pgAdmin ใหม่ (init script สร้าง `orders_test` ให้อัตโนมัติ) | `docker compose up -d db pgadmin` | ″ |
| 3 | Migrate DB `orders` และ `orders_test` | `goose -dir migrations postgres "<url>" up` | ″ |
| 4 | `go vet` + Go tests ทุก package (รวม integration test บน `orders_test`) | `TEST_DATABASE_URL=... go test -v -count=1 ./...` | [02-go-test.log](test-run/02-go-test.log) |
| 5 | รัน concurrency tests ซ้ำ 10 รอบ | `go test -count=10 -run 'Concurrent' ...` | [03-concurrency-repeat.log](test-run/03-concurrency-repeat.log) |
| 6 | Build API container ใหม่แล้วรัน e2e ผ่าน HTTP จริง | `ENABLE_FAULT_INJECTION=true docker compose --profile api up -d --build api` แล้ว `bash scripts/e2e.sh` | [04-e2e.log](test-run/04-e2e.log) |
| 7 | เก็บ log ของ API container | `docker compose logs api` | [05-api-container.log](test-run/05-api-container.log) |
| 8 | ดูสถานะ DB หลังทดสอบ | `psql` | [06-db-state.log](test-run/06-db-state.log) |

### ยืนยันว่า DB ถูกล้างจริง

หลัง `down -v` แล้วเปิดใหม่ ก่อน migrate:
```
orders
orders_test
tables in orders before migrate: 0
```
หลัง migrate (ทั้งสอง DB อยู่ที่ version 3):
```
    t     | count
----------+-------
 users    |     0
 products |     4      ← seed จาก 00003_seed_products.sql
 orders   |     0
```

---

## 2. ผล Go tests (`go test -v -count=1 ./...`)

`go vet`: ผ่าน · exit code `0` · **92 PASS / 0 FAIL / 0 SKIP**

ไม่มี test ถูก skip แปลว่า integration test ต่อ Postgres จริงได้

| Package | Test หลัก | Subtest | เวลา | ทดสอบอะไร |
|---|---|---|---|---|
| `internal/auth` | 8 | – | 0.14s | JWT round-trip; ปฏิเสธ alg=none, RS256, issuer ผิด, signature ผิด, token หมดอายุ; bcrypt ใส่ salt แล้ว Compare ผ่าน |
| `internal/config` | 3 | 6 | 0.02s | ค่า default; ไม่ยอม start ถ้า prod + fault injection, JWT secret สั้น, ไม่มี DB URL, ค่าไม่ถูกต้อง; memory mode ไม่ต้องมี DB |
| `internal/order` | 9 | 12 | 0.02s | input ไม่ถูกต้องไม่เรียก repo เลย (ใช้ spy นับ); total = ราคา × จำนวน; replay; key reuse → 422; key แยกต่อ user; 50 goroutine พร้อมกัน → 1 order; replay หลังราคาเปลี่ยนได้ total เดิม; `Money` → `198.00` |
| `internal/user` | 8 | – | 0.02s | ขอบเขตความยาว deviceId / password; normalize email; repo ได้ hash ไม่ใช่ plaintext; email ซ้ำ; email ไม่มี กับ password ผิดได้ error เดียวกัน; link |
| `internal/httpapi` | 26 | 20 | 0.56s | ทุก error ของ endpoint ผ่าน Fiber จริง, body ตรง envelope, total `198.00`, header `Idempotent-Replayed`, 413, CORS, rate limit, fault injection **+ integration กับ Postgres 4 ตัวด้านล่าง** |

### Integration tests บน Postgres จริง (DB `orders_test`)

```
--- PASS: TestPG_ConcurrentSameKeyCreatesOneOrder (0.12s)   ← 50 HTTP request พร้อมกันด้วย key เดียว → count(*) = 1
--- PASS: TestPG_EndToEnd (0.08s)                           ← register → login → order → replay (หลังเปลี่ยนราคา) → 422 → 404
--- PASS: TestPG_DeletedUserTokenIs401 (0.06s)              ← ลบ user แล้ว token เดิมได้ 401 (จาก FK)
--- PASS: TestPG_LinkGuestToEmail (0.06s)                   ← link แล้ว userId เดิม, order ไม่หาย
```

### รัน concurrency ซ้ำ 10 รอบ

`TestPG_ConcurrentSameKeyCreatesOneOrder` (Postgres) และ `TestCreateOrder_ConcurrentSameKey` (service + memory repo) ผ่าน **ทุกรอบ** (40/40 PASS line) ดูได้ที่ [03-concurrency-repeat.log](test-run/03-concurrency-repeat.log)

---

## 3. ผล E2E ผ่าน HTTP จริง (`scripts/e2e.sh`)

API รันใน Docker (`APP_ENV=dev`, `ENABLE_FAULT_INJECTION=true`) ต่อกับ DB `orders` ที่เพิ่งล้าง

**ผลรวม: `RESULT: 52 passed, 0 failed`**

| กลุ่ม | จำนวน check | ผล |
|---|---|---|
| Health / products / route ไม่มี / method ผิด | 4 | ✅ 200, 200, 404 `NOT_FOUND`, 405 `METHOD_NOT_ALLOWED` |
| Auth | 11 | ✅ guest, register 201, email ซ้ำ 409, email ผิด 400, password สั้น 400, login ผิด 401 ×2 (ได้ message เดียวกัน), login 200, me 200/401 |
| Orders: validation และ idempotency | 18 | ✅ create 201 `198.00` → replay id เดิม → body ต่าง 422; ไม่มี token / token มั่ว 401; ไม่มี key / key ผิด 400; ส่ง `total` มา, Content-Type ผิด → `INVALID_JSON`; quantity `0`, `2.5`, `"2"`, `100` → `INVALID_QUANTITY`; productId เป็นตัวเลข → `INVALID_PRODUCT_ID`; สินค้าไม่มี / retired → 404; list 200 |
| Fault injection | 7 | ✅ ดูหัวข้อ 3.1 |
| 30 request พร้อมกันด้วย key เดียว | 3 | ✅ ได้ 201 ครบ 30, **id เดียวกันทั้งหมด**, มีแค่ 1 response ที่เป็น `Replayed=false` |
| Link guest → email | 2 | ✅ 200 → ลองซ้ำได้ 409 `ALREADY_LINKED` |
| เช็คใน Postgres | 8 | ✅ ดูหัวข้อ 3.2 |

### 3.1 Scenario timeout / response หาย

```
PASS delay=3s, client timeout 1s            want=000 got=000       ← client ตัดสายเอง
PASS retry after timeout → replayed: true                          ← retry ด้วย key เดิมได้ order เดิม
PASS drop-after-commit                      want=500 got=500       ← server สร้าง order แล้วแต่ตอบ 500
PASS retry after dropped response           want=201 got=201  {"id":"62861adc-...","productId":"product-456","quantity":3,"total":148.50}
PASS status=503 / status=403 / unknown directive → 503 / 403 / 400
```

### 3.2 ตรวจใน DB โดยตรง

```
PASS rows for key 939dc9da: 1        ← create + replay + 422
PASS rows for key 63c41495: 1        ← timeout แล้ว retry
PASS rows for key 4dc40e4e: 1        ← drop-after-commit แล้ว retry
PASS rows for key 1b6514ea: 1        ← 30 request พร้อมกัน
PASS total_cents product-123 x2: 19800
PASS total_cents product-456 x3: 14850
PASS password stored as bcrypt: $2a$12$
PASS linked account keeps guest orders: 4
```

สถานะ DB หลังจบ ([06-db-state.log](test-run/06-db-state.log)):

```
 users    |     2          ← guest (link เป็น email แล้ว) + บัญชีที่ register
 products |     4
 orders   |     4          ← ยิง POST /api/orders ไปหลายสิบครั้ง แต่มี order จริงแค่ 4

 product_id  | quantity | unit_price_cents | total_cents |   key
-------------+----------+------------------+-------------+----------
 product-123 |        2 |             9900 |       19800 | 939dc9da
 sword-001   |        1 |            25000 |       25000 | 63c41495
 product-456 |        3 |             4950 |       14850 | 4dc40e4e
 product-123 |        1 |             9900 |        9900 | 1b6514ea

-- idempotency_key ที่มีมากกว่า 1 แถว:
(0 rows)
```

---

## 4. ตรวจ log ของ API container

- [05-api-container.log](test-run/05-api-container.log) มี 100 บรรทัด เป็น JSON (`log/slog`) มีแค่ method / path / status / duration_ms / ip
- ค้นคำ `eyJ` (JWT), `password123`, `bearer ` แล้ว**ไม่เจอเลย (0 ครั้ง)**
- request ที่ใช้ fault injection มีบรรทัด `WARN "fault injection"` แยกไว้ให้เห็น

> หมายเหตุ: ใน [04-e2e.log](test-run/04-e2e.log) (log ฝั่ง test ไม่ใช่ server) ตัด response body ไว้ที่ 150 ตัวอักษร ทำให้เห็น JWT ของบัญชีทดสอบบางส่วน แต่ไม่มีส่วน signature จึงเอาไปใช้เป็น token ไม่ได้

---

## 5. ข้อจำกัดของการทดสอบรอบนี้

- **ไม่ได้รัน `go test -race`** เพราะเครื่อง Windows นี้ไม่มี cgo ควรรันใน CI บน Linux
- ยังไม่ได้ทดสอบกับ Unity client จริง ทดสอบแค่ผ่าน curl และ Go HTTP client
- ไม่ได้ทำ load test / performance test

---

## 6. วิธีรันซ้ำ

```bash
# ล้างทุกอย่างแล้วเริ่มใหม่
docker compose --profile api down -v
docker compose up -d db pgadmin
goose -dir migrations postgres "postgres://orders:orders@localhost:5433/orders?sslmode=disable" up
goose -dir migrations postgres "postgres://orders:orders@localhost:5433/orders_test?sslmode=disable" up

# Go tests (unit + handler + integration)
TEST_DATABASE_URL="postgres://orders:orders@localhost:5433/orders_test?sslmode=disable" go test -v -count=1 ./...

# E2E ผ่าน HTTP
ENABLE_FAULT_INJECTION=true docker compose --profile api up -d --build api
bash scripts/e2e.sh          # จบด้วย "RESULT: N passed, 0 failed", exit code 0 ถ้าผ่านหมด
```
