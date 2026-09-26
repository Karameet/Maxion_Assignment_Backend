# Order Backend — API Reference

เอกสารนี้อธิบาย endpoint ทั้งหมด วิธีเรียกใช้ และตัวอย่าง request / response
ตัวอย่างทุกตัวในเอกสารนี้เอามาจาก server ที่รันอยู่จริงบน Postgres (ย่อ token ให้สั้นลงเพื่อให้อ่านง่าย)

- Base URL (local): `http://localhost:8080`
- ทุก request/response เป็น JSON (UTF-8)
- ทุก endpoint ขึ้นต้นด้วย `/api` ยกเว้น `/healthz`

---

## สารบัญ

1. [เริ่มต้นใช้งานเร็ว (Flow หลัก)](#1-เริ่มต้นใช้งานเร็ว-flow-หลัก)
2. [หลักการทั่วไป](#2-หลักการทั่วไป)
3. [Endpoints](#3-endpoints)
   - [GET /healthz](#get-healthz)
   - [POST /api/auth/guest](#post-apiauthguest)
   - [POST /api/auth/register](#post-apiauthregister)
   - [POST /api/auth/login](#post-apiauthlogin)
   - [POST /api/auth/link](#post-apiauthlink)
   - [GET /api/me](#get-apime)
   - [GET /api/products](#get-apiproducts)
   - [POST /api/orders ⭐](#post-apiorders-)
   - [GET /api/orders](#get-apiorders)
4. [Error codes ทั้งหมด](#4-error-codes-ทั้งหมด)
5. [Idempotency และการ Retry](#5-idempotency-และการ-retry)
6. [Fault injection (dev เท่านั้น)](#6-fault-injection-dev-เท่านั้น)
7. [ตัวอย่างการเรียกจาก Unity (C#)](#7-ตัวอย่างการเรียกจาก-unity-c)
8. [ข้อมูลตัวอย่าง (Seed data)](#8-ข้อมูลตัวอย่าง-seed-data)

---

## 1. เริ่มต้นใช้งานเร็ว (Flow หลัก)

```
1. ขอ token      →  POST /api/auth/guest   (หรือ register / login)
2. ดูสินค้า       →  GET  /api/products
3. สร้าง order    →  POST /api/orders       + Authorization + Idempotency-Key
4. ถ้า timeout/5xx → ยิงซ้ำด้วย Idempotency-Key เดิม  → ได้ order เดิม ไม่ซ้ำ
```

```bash
BASE=http://localhost:8080

# 1) guest login → เก็บ token
TOKEN=$(curl -s -X POST $BASE/api/auth/guest \
  -H 'Content-Type: application/json' \
  -d '{"deviceId":"a1b2c3d4e5f6g7h8i9j0"}' | jq -r .token)

# 2) ดูสินค้า
curl -s $BASE/api/products

# 3) สั่งซื้อ
KEY=$(uuidgen)
curl -i -X POST $BASE/api/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' \
  -d '{"productId":"product-123","quantity":2}'
```

---

## 2. หลักการทั่วไป

### 2.1 Authentication

endpoint ที่ต้อง login ให้ส่ง header นี้:

```
Authorization: Bearer <token>
```

- token เป็น JWT (HS256) อายุ **7 วัน** (ค่าเริ่มต้น `JWT_TTL=168h`)
- ได้ token จาก `/api/auth/guest`, `/api/auth/register`, `/api/auth/login` หรือ `/api/auth/link`
- ถ้าไม่ส่ง token, token ผิด, token หมดอายุ หรือบัญชีถูกลบไปแล้ว จะได้ `401 UNAUTHORIZED`
- **ยังไม่มี refresh token** เมื่อหมดอายุให้ login ใหม่

### 2.2 Content-Type

request ที่มี body ต้องส่ง `Content-Type: application/json` ถ้าไม่ส่งจะได้ `400 INVALID_JSON`

server อ่าน JSON แบบ **strict**:
- ถ้ามี field ที่ไม่รู้จัก เช่น `total` หรือ `price` จะได้ `400 INVALID_JSON` และ message จะบอกชื่อ field นั้น
- body ต้องเป็น JSON object เดียว ห้ามมีข้อมูลต่อท้าย
- ขนาด body สูงสุด 16 KB (`MAX_BODY_BYTES`) ถ้าเกินจะได้ `413 PAYLOAD_TOO_LARGE`

### 2.3 รูปแบบ Error (ทุก endpoint ใช้แบบเดียวกัน)

```json
{
  "error": {
    "code": "INVALID_QUANTITY",
    "message": "quantity must be a positive integer within the allowed maximum"
  }
}
```

> ⚠️ **ฝั่ง client ให้แยกกรณีด้วย HTTP status + `code` เท่านั้น**
> `message` เป็นข้อความสำหรับคนอ่านและอาจเปลี่ยนได้ ห้ามเอาไปใช้เป็นเงื่อนไขในโค้ด

### 2.4 เงิน (Money)

- ราคาและยอดรวมส่งกลับเป็น **JSON number ทศนิยม 2 ตำแหน่งเสมอ** เช่น `198.00`, `49.50`
- ในระบบเก็บเป็นจำนวนเต็มหน่วยสตางค์ (cents) จึงไม่มีปัญหาปัดเศษแบบ float
- **client ส่งราคามาไม่ได้** server เป็นคนคำนวณ `total` เองเสมอ

### 2.5 CORS (สำหรับ WebGL)

- Header ที่ส่งได้: `Content-Type`, `Authorization`, `Idempotency-Key` (และ `X-Debug-Fault` เมื่อเปิด fault injection)
- Header ที่ JavaScript อ่านได้: `Idempotent-Replayed`
- Origin ที่อนุญาตตั้งได้ที่ `CORS_ALLOWED_ORIGINS` (ค่าเริ่มต้น `*`)

---

## 3. Endpoints

| Method | Path | Auth | สรุป |
|---|---|---|---|
| GET | `/healthz` | – | เช็คว่า server และ DB พร้อมหรือไม่ |
| POST | `/api/auth/guest` | – | login แบบ guest ด้วย deviceId |
| POST | `/api/auth/register` | – | สมัครด้วย email + password |
| POST | `/api/auth/login` | – | login ด้วย email + password |
| POST | `/api/auth/link` | Bearer | ผูก email/password เข้ากับบัญชี guest เดิม |
| GET | `/api/me` | Bearer | ดูข้อมูลบัญชีตัวเอง |
| GET | `/api/products` | – | รายการสินค้าที่ขายอยู่ |
| **POST** | **`/api/orders`** | Bearer + Idempotency-Key | **สร้าง order** |
| GET | `/api/orders` | Bearer | order ของตัวเอง (ล่าสุด 50 รายการ) |

---

### `GET /healthz`

เช็คว่า server ทำงานอยู่และต่อ DB ได้

**Response `200`**
```json
{ "status": "ok" }
```

**Response `503`** (ต่อ DB ไม่ได้)
```json
{ "error": { "code": "SERVICE_UNAVAILABLE", "message": "database unavailable" } }
```

---

### `POST /api/auth/guest`

login แบบไม่ต้องสมัคร ถ้าส่ง `deviceId` เดิมซ้ำจะได้ **userId เดิม**

**Request body**

| Field | Type | Required | กติกา |
|---|---|---|---|
| `deviceId` | string | ✔ | ยาว 16–128 ตัวอักษร (ดูวิธีสร้างใน Unity ที่ [หัวข้อ 7](#7-ตัวอย่างการเรียกจาก-unity-c)) ⚠️ **ห้ามส่ง `SystemInfo.deviceUniqueIdentifier` ไปตรงๆ** เพราะบน WebGL ค่านี้เป็น `"n/a"` แล้วจะได้ `400` |

```bash
curl -X POST http://localhost:8080/api/auth/guest \
  -H 'Content-Type: application/json' \
  -d '{"deviceId":"a1b2c3d4e5f6g7h8i9j0"}'
```

**Response `200`**
```json
{
  "userId": "3a7e4d46-64a0-4038-b4c0-8b09581ec554",
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2N0Ijoi...ZubPBq_b3Tpu",
  "expiresAt": "2026-10-03T08:13:07Z",
  "accountType": "guest"
}
```

**Errors**

| Status | code | เมื่อไร |
|---|---|---|
| 400 | `INVALID_DEVICE_ID` | deviceId สั้นกว่า 16 หรือยาวกว่า 128 ตัวอักษร |
| 400 | `INVALID_JSON` | JSON พัง หรือมี field อื่นปน |

```json
{ "error": { "code": "INVALID_DEVICE_ID", "message": "deviceId must be 16-128 characters" } }
```

---

### `POST /api/auth/register`

สมัครบัญชีด้วย email + password สมัครเสร็จได้ token ทันที ไม่ต้อง login ซ้ำ

**Request body**

| Field | Type | Required | กติกา |
|---|---|---|---|
| `email` | string | ✔ | รูปแบบ email ปกติ ≤ 254 ตัวอักษร server จะตัดช่องว่างหน้า/หลังและแปลงเป็นตัวพิมพ์เล็กก่อนเก็บ |
| `password` | string | ✔ | ยาว **8–72 bytes** |

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"Player@Example.com","password":"correct horse battery"}'
```

**Response `201`**
```json
{
  "userId": "44cb5953-5d5a-4b02-995a-01c73b49f4cc",
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY2N0Ijoi...ONfXacAGONfb",
  "expiresAt": "2026-10-03T08:13:08Z",
  "accountType": "email"
}
```

> email ด้านบนจะถูกเก็บเป็น `player@example.com`

**Errors**

| Status | code | เมื่อไร |
|---|---|---|
| 400 | `INVALID_EMAIL` | รูปแบบ email ไม่ถูกต้อง (รวมถึงแบบ `"Name <a@b.com>"`) |
| 400 | `WEAK_PASSWORD` | password สั้นกว่า 8 หรือยาวกว่า 72 bytes |
| 409 | `EMAIL_TAKEN` | email นี้มีคนสมัครแล้ว (ไม่สนตัวพิมพ์เล็ก/ใหญ่) |
| 429 | `RATE_LIMITED` | ยิง register + login รวมกันเกิน 10 ครั้งต่อนาทีต่อ IP |

```json
{ "error": { "code": "EMAIL_TAKEN", "message": "email is already registered" } }
```

---

### `POST /api/auth/login`

**Request body:** เหมือน register (`email`, `password`)

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"player@example.com","password":"correct horse battery"}'
```

**Response `200`:** รูปแบบเดียวกับ register (`accountType: "email"`)

**Errors**

| Status | code | เมื่อไร |
|---|---|---|
| 401 | `INVALID_CREDENTIALS` | email ไม่มีในระบบ **หรือ** password ผิด (ตั้งใจให้ได้ error เดียวกัน เพื่อไม่ให้ใครใช้ตรวจว่า email ไหนมีบัญชี) |
| 429 | `RATE_LIMITED` | ยิงถี่เกินไป |

```json
{ "error": { "code": "INVALID_CREDENTIALS", "message": "invalid email or password" } }
```

---

### `POST /api/auth/link`

ผูก email + password เข้ากับบัญชี **guest ที่ login อยู่** ผลคือ:
- `userId` ยังเป็นค่าเดิม และ order เดิมยังอยู่ครบ
- ได้ token ใหม่ที่มี `accountType: "email"` ให้ใช้ token ใหม่นี้แทนตัวเก่า
- หลังจากนี้ login ด้วย email/password ได้ตามปกติ

**Headers:** `Authorization: Bearer <guest token>`
**Request body:** `{ "email": "...", "password": "..." }` (กติกาเหมือน register)

```bash
curl -X POST http://localhost:8080/api/auth/link \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"email":"linked@example.com","password":"password123"}'
```

**Response `200`:** รูปแบบเดียวกับ register แต่ `userId` เป็นค่าเดิมของ guest

**Errors**

| Status | code | เมื่อไร |
|---|---|---|
| 401 | `UNAUTHORIZED` | ไม่มี token หรือ token ใช้ไม่ได้ |
| 400 | `INVALID_EMAIL` / `WEAK_PASSWORD` | ข้อมูลไม่ผ่านกติกา |
| 409 | `ALREADY_LINKED` | บัญชีนี้มี email อยู่แล้ว |
| 409 | `EMAIL_TAKEN` | email นี้มีบัญชีอื่นใช้อยู่แล้ว |

---

### `GET /api/me`

**Headers:** `Authorization: Bearer <token>`

```bash
curl http://localhost:8080/api/me -H "Authorization: Bearer $TOKEN"
```

**Response `200`**
```json
{
  "userId": "3a7e4d46-64a0-4038-b4c0-8b09581ec554",
  "accountType": "guest",
  "email": null,
  "createdAt": "2026-09-26T08:13:07Z"
}
```

`email` เป็น `null` สำหรับ guest ที่ยังไม่ได้ link

**Errors:** `401 UNAUTHORIZED`

---

### `GET /api/products`

รายการสินค้าที่ขายอยู่ (สินค้าที่ปิดขายแล้วจะไม่แสดง) เรียกได้โดยไม่ต้อง login

```bash
curl http://localhost:8080/api/products
```

**Response `200`**
```json
{
  "products": [
    { "id": "product-123", "name": "Health Potion", "price": 99.00 },
    { "id": "product-456", "name": "Mana Potion",   "price": 49.50 },
    { "id": "sword-001",   "name": "Iron Sword",    "price": 250.00 }
  ]
}
```

---

### `POST /api/orders` ⭐

endpoint หลักของ assignment ใช้สร้าง order โดย server เป็นคนคำนวณราคาเอง และกันไม่ให้เกิด order ซ้ำด้วย `Idempotency-Key`

**Headers**

| Header | Required | ค่า |
|---|---|---|
| `Authorization` | ✔ | `Bearer <token>` |
| `Idempotency-Key` | ✔ | 8–128 ตัวอักษร `[A-Za-z0-9_-]` ใช้ UUID ได้เลย **สร้างใหม่หนึ่งครั้งต่อการกดสั่งซื้อหนึ่งครั้ง** |
| `Content-Type` | ✔ | `application/json` |

**Request body**

| Field | Type | Required | กติกา |
|---|---|---|---|
| `productId` | string | ✔ | 1–64 ตัวอักษร `[A-Za-z0-9_-]` |
| `quantity` | integer | ✔ | จำนวนเต็ม 1–99 (`MAX_ORDER_QUANTITY`) |

ส่งได้แค่ 2 field นี้เท่านั้น ถ้าส่ง `total`, `price` หรือ field อื่นมาด้วยจะถูกปฏิเสธ

```bash
curl -i -X POST http://localhost:8080/api/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: 3f2c1e9a-7b4d-4e8a-9c1f-2a6b5d8e0f11" \
  -H 'Content-Type: application/json' \
  -d '{"productId":"product-123","quantity":2}'
```

**Response `201 Created`**
```http
HTTP/1.1 201 Created
Content-Type: application/json; charset=utf-8
Idempotent-Replayed: false

{"id":"73211725-cbc4-446d-a1c7-254e9ad53a46","productId":"product-123","quantity":2,"total":198.00}
```

| Field | Type | ความหมาย |
|---|---|---|
| `id` | string (UUID) | รหัส order |
| `productId` | string | สินค้าที่สั่ง |
| `quantity` | integer | จำนวน |
| `total` | number | ยอดรวมที่ server คำนวณ (ราคาต่อชิ้น × จำนวน) |

**Response header `Idempotent-Replayed`**
- `false`: request นี้สร้าง order ใหม่
- `true`: key นี้เคยใช้สร้าง order ไปแล้ว server เลยส่ง order เดิมกลับมา (status ยังเป็น `201` และ body เหมือนครั้งแรกทุกไบต์)

#### ถ้ายิงซ้ำด้วย key เดิมและ body เดิม

```http
HTTP/1.1 201 Created
Idempotent-Replayed: true

{"id":"73211725-cbc4-446d-a1c7-254e9ad53a46","productId":"product-123","quantity":2,"total":198.00}
```

ได้ `id` เดิม และใน DB ยังมี order นี้แค่ 1 แถว

#### ลำดับการตรวจสอบ

server ตรวจตามลำดับนี้ และทุกข้อตรวจเสร็จก่อนแตะ DB:

1. `Authorization` → `401 UNAUTHORIZED`
2. `Idempotency-Key` ต้องมี → `400 MISSING_IDEMPOTENCY_KEY`
3. `Idempotency-Key` ต้องถูกรูปแบบ → `400 INVALID_IDEMPOTENCY_KEY`
4. body ต้องเป็น JSON ที่ถูกต้องและไม่มี field แปลก → `400 INVALID_JSON`
5. `productId` → `400 INVALID_PRODUCT_ID`
6. `quantity` → `400 INVALID_QUANTITY`
7. หาสินค้าใน DB → `404 PRODUCT_NOT_FOUND`
8. ตรวจ key ซ้ำ → `422 IDEMPOTENCY_KEY_REUSED`

#### ตัวอย่างค่าที่ใช้ได้ / ใช้ไม่ได้

| Body | ผล |
|---|---|
| `{"productId":"product-123","quantity":2}` | ✅ `201` total `198.00` |
| `{"productId":"product-123","quantity":2,"total":1}` | ❌ `400 INVALID_JSON` — `unknown field "total"` |
| `{"productId":"product-123","quantity":0}` | ❌ `400 INVALID_QUANTITY` |
| `{"productId":"product-123","quantity":-1}` | ❌ `400 INVALID_QUANTITY` |
| `{"productId":"product-123","quantity":100}` | ❌ `400 INVALID_QUANTITY` (เกิน 99) |
| `{"productId":"product-123","quantity":2.5}` | ❌ `400 INVALID_QUANTITY` |
| `{"productId":"product-123","quantity":2.0}` | ❌ `400 INVALID_QUANTITY` (ต้องเป็นจำนวนเต็มล้วน) |
| `{"productId":"product-123","quantity":"2"}` | ❌ `400 INVALID_QUANTITY` (เป็น string) |
| `{"productId":"product-123"}` | ❌ `400 INVALID_QUANTITY` (ไม่มี quantity) |
| `{"productId":123,"quantity":1}` | ❌ `400 INVALID_PRODUCT_ID` |
| `{"productId":"","quantity":1}` | ❌ `400 INVALID_PRODUCT_ID` |
| `{"productId":"nope-999","quantity":1}` | ❌ `404 PRODUCT_NOT_FOUND` |
| `{"productId":"retired-001","quantity":1}` | ❌ `404 PRODUCT_NOT_FOUND` (ปิดขายแล้ว) |

#### Errors ทั้งหมดของ endpoint นี้

| Status | code | เมื่อไร | ฝั่ง client ควรทำอะไร |
|---|---|---|---|
| 400 | `MISSING_IDEMPOTENCY_KEY` | ไม่ได้ส่ง header | bug ฝั่ง client |
| 400 | `INVALID_IDEMPOTENCY_KEY` | key สั้นหรือยาวเกิน หรือมีอักขระต้องห้าม | bug ฝั่ง client |
| 400 | `INVALID_JSON` | JSON พัง, มี field อื่นปน, Content-Type ไม่ใช่ JSON | bug ฝั่ง client |
| 400 | `INVALID_PRODUCT_ID` | productId ผิดรูปแบบ | แสดง validation error |
| 400 | `INVALID_QUANTITY` | quantity ไม่ใช่จำนวนเต็ม 1–99 | แสดง validation error |
| 401 | `UNAUTHORIZED` | ไม่มี token, token ผิด/หมดอายุ หรือบัญชีถูกลบ | ให้ login ใหม่ |
| 403 | `FORBIDDEN` | เปิด `ORDERS_REQUIRE_EMAIL_ACCOUNT=true` แล้วใช้ guest token | ให้ผู้เล่นสมัครหรือ link email |
| 404 | `PRODUCT_NOT_FOUND` | ไม่มีสินค้านี้ หรือปิดขายแล้ว | แสดงว่าไม่พบสินค้า |
| 413 | `PAYLOAD_TOO_LARGE` | body ใหญ่เกิน 16 KB | bug ฝั่ง client |
| 422 | `IDEMPOTENCY_KEY_REUSED` | ใช้ key เดิมแต่ `productId` หรือ `quantity` ต่างจากครั้งแรก | bug ฝั่ง client แสดง error ทั่วไปแล้ว log ไว้ |
| 500 | `INTERNAL` | server error | แสดง error และให้ retry **ด้วย key เดิม** |

```json
{ "error": { "code": "IDEMPOTENCY_KEY_REUSED", "message": "Idempotency-Key was already used with a different request body" } }
```

---

### `GET /api/orders`

order ของผู้ใช้ที่ login อยู่ เรียงจากใหม่ไปเก่า สูงสุด 50 รายการ (เห็นเฉพาะ order ของตัวเอง)

**Headers:** `Authorization: Bearer <token>`

```bash
curl http://localhost:8080/api/orders -H "Authorization: Bearer $TOKEN"
```

**Response `200`**
```json
{
  "orders": [
    {
      "id": "73211725-cbc4-446d-a1c7-254e9ad53a46",
      "productId": "product-123",
      "quantity": 2,
      "unitPrice": 99.00,
      "total": 198.00,
      "createdAt": "2026-09-26T08:13:08Z"
    }
  ]
}
```

`unitPrice` คือราคาต่อชิ้น **ณ เวลาที่สั่ง** ถ้าราคาสินค้าเปลี่ยนภายหลัง order เก่าจะไม่เปลี่ยนตาม

**Errors:** `401 UNAUTHORIZED`

---

## 4. Error codes ทั้งหมด

ทั้งหมดนี้ประกาศไว้ใน [internal/httpapi/codes.go](../internal/httpapi/codes.go) ไฟล์เดียว ก๊อปไปทำ enum ฝั่ง Unity ได้เลย

| HTTP | code | ความหมาย |
|---|---|---|
| 400 | `INVALID_JSON` | body ไม่ใช่ JSON ที่ถูกต้อง, มี field แปลก หรือ Content-Type ผิด |
| 400 | `INVALID_DEVICE_ID` | deviceId ไม่ได้ยาว 16–128 ตัวอักษร |
| 400 | `INVALID_EMAIL` | รูปแบบ email ผิด |
| 400 | `WEAK_PASSWORD` | password ไม่ได้ยาว 8–72 bytes |
| 400 | `MISSING_IDEMPOTENCY_KEY` | ไม่ได้ส่ง `Idempotency-Key` |
| 400 | `INVALID_IDEMPOTENCY_KEY` | `Idempotency-Key` ผิดรูปแบบ |
| 400 | `INVALID_PRODUCT_ID` | productId ผิดรูปแบบ |
| 400 | `INVALID_QUANTITY` | quantity ไม่ใช่จำนวนเต็ม 1–99 |
| 400 | `BAD_REQUEST` | request ผิดแบบอื่นๆ (เช่นค่า `X-Debug-Fault` ผิด) |
| 401 | `UNAUTHORIZED` | ไม่มี token, token ใช้ไม่ได้/หมดอายุ หรือบัญชีถูกลบ |
| 401 | `INVALID_CREDENTIALS` | email หรือ password ไม่ถูกต้อง |
| 403 | `FORBIDDEN` | ไม่มีสิทธิ์ (guest เมื่อระบบบังคับใช้ email account) |
| 404 | `NOT_FOUND` | ไม่มี route นี้ |
| 404 | `PRODUCT_NOT_FOUND` | ไม่มีสินค้านี้ หรือปิดขายแล้ว |
| 405 | `METHOD_NOT_ALLOWED` | route นี้ไม่รองรับ HTTP method ที่ส่งมา |
| 409 | `EMAIL_TAKEN` | email นี้ถูกใช้แล้ว |
| 409 | `ALREADY_LINKED` | บัญชีนี้มี email แล้ว |
| 413 | `PAYLOAD_TOO_LARGE` | body ใหญ่เกินกำหนด |
| 422 | `IDEMPOTENCY_KEY_REUSED` | ใช้ key เดิมกับข้อมูล order ที่ต่างไปจากครั้งแรก |
| 429 | `RATE_LIMITED` | ยิงถี่เกินไป (register/login) |
| 500 | `INTERNAL` | server error (ไม่เปิดเผยรายละเอียดภายใน) |
| 503 | `SERVICE_UNAVAILABLE` | DB ใช้งานไม่ได้ หรือเป็นผลจาก fault injection |

---

## 5. Idempotency และการ Retry

### กติกาสำหรับ client

1. **สร้าง key ใหม่ 1 ครั้งต่อ "การกดสั่งซื้อ" 1 ครั้ง** เช่น `Guid.NewGuid().ToString()`
2. ถ้าเกิด **timeout / network error / 5xx** ให้ retry **ด้วย key เดิมและ body เดิม**
3. ถ้าผู้เล่นกดสั่ง order ใหม่ ให้สร้าง key ใหม่
4. ห้ามใช้ key เดิมกับสินค้าหรือจำนวนที่ต่างไป ไม่อย่างนั้นจะได้ `422`

### แต่ละกรณีได้ผลอะไร

| สถานการณ์ | ผล |
|---|---|
| key ใหม่ | `201`, `Idempotent-Replayed: false`, สร้าง order ใหม่ |
| key เดิม + body เดิม | `201`, `Idempotent-Replayed: true`, ได้ order เดิม (ไม่สร้างเพิ่ม) |
| key เดิม + body ต่าง | `422 IDEMPOTENCY_KEY_REUSED` |
| key เดียวกันแต่คนละ user | ไม่เกี่ยวกัน ต่างคนต่างสร้าง order ของตัวเองได้ |
| สินค้าไม่มี (`404`) | key ยังไม่ถูกใช้ แก้ request แล้วยิงด้วย key เดิมได้ |
| ส่งพร้อมกันหลาย request ด้วย key เดียวกัน | ได้ order เดียว ทุก request ได้ id เดียวกัน (ทดสอบแล้วที่ 50 request พร้อมกัน) |

### ตัวอย่าง: timeout แล้ว retry

```
Client                                   Server
  │ POST /api/orders  key=K1               │
  │──────────────────────────────────────▶│  สร้าง order #A (commit ลง DB)
  │        (client timeout, ไม่ได้ response) │
  │ POST /api/orders  key=K1 (retry)       │
  │──────────────────────────────────────▶│  เจอ K1 แล้ว → คืน order #A
  │◀──────────────────────────────────────│  201, Idempotent-Replayed: true
```

ผลคือผู้เล่นเห็น order #A และใน DB มีแค่ 1 แถว

---

## 6. Fault injection (dev เท่านั้น)

ใช้จำลอง error เพื่อทดสอบ UI ฝั่ง client จะทำงานก็ต่อเมื่อ server รันด้วย `APP_ENV=dev` **และ** `ENABLE_FAULT_INJECTION=true` ถ้าตั้ง `APP_ENV=prod` คู่กับ fault injection server จะไม่ยอม start

ส่ง header `X-Debug-Fault` มากับ request (ใส่หลายคำสั่งได้โดยคั่นด้วย `,`)

| ค่า | ผล |
|---|---|
| `delay=5s` | รอ 5 วินาทีก่อนประมวลผล **แต่ยังสร้าง order ตามปกติ** (สูงสุด 10s) ใช้ทดสอบ timeout |
| `status=500` | ตอบ `500 INTERNAL` ทันที ไม่สร้าง order |
| `status=503` | ตอบ `503 SERVICE_UNAVAILABLE` ทันที |
| `status=403` | ตอบ `403 FORBIDDEN` ทันที |
| `status=401` | ตอบ `401 UNAUTHORIZED` ทันที |
| `drop-after-commit` | สร้าง order สำเร็จแล้ว แต่ตอบ `500` (จำลองกรณี response หายระหว่างทาง) |

```bash
# จำลอง timeout: client รอแค่ 2s, server หน่วง 5s
curl -m 2 -X POST $BASE/api/orders -H 'X-Debug-Fault: delay=5s' \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' -d '{"productId":"product-123","quantity":2}'

# จำลอง server ล่ม
curl -X POST $BASE/api/orders -H 'X-Debug-Fault: status=503' ...
```

ถ้าส่งค่าที่ server ไม่รู้จัก จะได้ `400 BAD_REQUEST`:
```json
{ "error": { "code": "BAD_REQUEST", "message": "invalid X-Debug-Fault: unknown directive \"explode\"" } }
```

---

## 7. ตัวอย่างการเรียกจาก Unity (C#)

ตัวอย่างนี้เป็นแนวทางขั้นต่ำ ใช้ `UnityWebRequest` + `JsonUtility`

### 7.1 ตั้งค่าก่อนเริ่ม

| เรื่อง | ต้องทำ |
|---|---|
| **Base URL** | Editor / PC เครื่องเดียวกัน: `http://localhost:8080` · มือถือที่ต่อ Wi-Fi เดียวกัน: `http://<IP ของเครื่อง server>:8080` (เช่น `http://192.168.1.234:8080`) · Android Emulator: `http://10.0.2.2:8080` ทุกกรณีให้เก็บเป็น config ห้าม hardcode |
| **HTTP ธรรมดา (ไม่ใช่ https)** | Unity 2022.1 ขึ้นไป: Player Settings → Other Settings → **Allow downloads over HTTP** = `Always allowed` (หรือ `Development builds only`) ถ้าไม่ตั้ง จะได้ error `Insecure connection not allowed` |
| **WebGL** | server เปิด CORS ไว้แล้ว ถ้าจะใช้ `X-Debug-Fault` จาก WebGL ต้องรัน server ด้วย `ENABLE_FAULT_INJECTION=true` |
| **Firewall** | ถ้ามือถือต่อไม่ได้ ให้เปิด port 8080 ใน Windows Firewall ของเครื่อง server |

### 7.2 deviceId สำหรับ guest login

`SystemInfo.deviceUniqueIdentifier` ใช้ตรงๆ ไม่ได้ทุก platform (บน WebGL ได้ `"n/a"` → server ตอบ `400 INVALID_DEVICE_ID`) ให้สร้าง GUID ครั้งแรกแล้วเก็บไว้ใน `PlayerPrefs`:

```csharp
public static string GetDeviceId()
{
    const string prefKey = "order_api_device_id";
    var id = PlayerPrefs.GetString(prefKey, "");
    if (id.Length < 16)
    {
        var sys = SystemInfo.deviceUniqueIdentifier;
        id = (sys != SystemInfo.unsupportedIdentifier && sys.Length >= 16 && sys.Length <= 128)
            ? sys
            : Guid.NewGuid().ToString();   // 36 ตัวอักษร ผ่านกติกา 16–128
        PlayerPrefs.SetString(prefKey, id);
        PlayerPrefs.Save();
    }
    return id;
}
```

### 7.3 สร้าง order

```csharp
using System;
using System.Collections;
using System.Text;
using UnityEngine;
using UnityEngine.Networking;

[Serializable] public class CreateOrderRequest { public string productId; public int quantity; }
[Serializable] public class OrderResponse { public string id; public string productId; public int quantity; public double total; }
[Serializable] public class ApiErrorDetail { public string code; public string message; }
[Serializable] public class ApiError { public ApiErrorDetail error; }

public class OrderClient : MonoBehaviour
{
    [SerializeField] string baseUrl = "http://localhost:8080";
    [SerializeField] int timeoutSeconds = 10;

    // เรียกหนึ่งครั้งต่อการกดสั่งซื้อ แล้วเก็บ key ไว้ใช้ตอน retry
    public string NewIdempotencyKey() => Guid.NewGuid().ToString();

    public IEnumerator CreateOrder(string token, string idempotencyKey, string productId, int quantity,
                                   Action<OrderResponse, bool> onSuccess, Action<long, string> onError)
    {
        var body = JsonUtility.ToJson(new CreateOrderRequest { productId = productId, quantity = quantity });
        using var req = new UnityWebRequest($"{baseUrl}/api/orders", "POST");
        req.uploadHandler = new UploadHandlerRaw(Encoding.UTF8.GetBytes(body));
        req.downloadHandler = new DownloadHandlerBuffer();
        req.timeout = timeoutSeconds;
        req.SetRequestHeader("Content-Type", "application/json");
        req.SetRequestHeader("Authorization", "Bearer " + token);
        req.SetRequestHeader("Idempotency-Key", idempotencyKey);

        yield return req.SendWebRequest();

        if (req.result == UnityWebRequest.Result.ConnectionError)
        {
            onError(0, "NETWORK_ERROR");          // timeout / ต่อไม่ได้ → ให้ Retry ด้วย key เดิม
            yield break;
        }
        if (req.responseCode == 201)
        {
            var order = JsonUtility.FromJson<OrderResponse>(req.downloadHandler.text);
            bool replayed = req.GetResponseHeader("Idempotent-Replayed") == "true";
            onSuccess(order, replayed);
            yield break;
        }
        onError(req.responseCode, ParseErrorCode(req.downloadHandler.text));
    }

    // proxy หรือ load balancer อาจส่ง body ที่ไม่ใช่ JSON (เช่นหน้า HTML ของ 502)
    // JsonUtility จะ throw ถ้าเจอแบบนั้น เลยต้องครอบ try/catch ไว้
    static string ParseErrorCode(string body)
    {
        try { return JsonUtility.FromJson<ApiError>(body)?.error?.code ?? "UNKNOWN"; }
        catch (ArgumentException) { return "UNKNOWN"; }
    }
}
```

> ตัวอย่างนี้เป็นแนวทาง ยังไม่ได้ compile ใน Unity จริง

### 7.4 Mapping status → UI state

| ผลที่ได้ | UI |
|---|---|
| `201` | สำเร็จ แสดง `id`, `quantity`, `total` จาก server |
| `400` (`INVALID_*`) | Validation error |
| `401` / `403` | Auth error (ให้ login ใหม่ หรือบอกว่าไม่มีสิทธิ์) |
| `404 PRODUCT_NOT_FOUND` | ไม่พบสินค้า |
| `422 IDEMPOTENCY_KEY_REUSED` | error ทั่วไป (เป็น bug ฝั่ง client ให้ log ไว้) |
| `5xx` | Server error แสดงปุ่ม Retry ที่ใช้ **key เดิม** |
| timeout / connection error | Network error แสดงปุ่ม Retry ที่ใช้ **key เดิม** |

> `total` เป็นยอดเงิน ถ้าจะแสดงผลให้ format เป็นทศนิยม 2 ตำแหน่ง ห้ามเอาค่านี้ไปคำนวณต่อแล้วส่งกลับไปที่ server

---

## 8. ข้อมูลตัวอย่าง (Seed data)

ข้อมูลนี้ใส่มาจาก migration `00003_seed_products.sql` และมีชุดเดียวกันในโหมด `ORDER_REPO=memory`

| productId | name | price | active |
|---|---|---|---|
| `product-123` | Health Potion | 99.00 | ✔ |
| `product-456` | Mana Potion | 49.50 | ✔ |
| `sword-001` | Iron Sword | 250.00 | ✔ |
| `retired-001` | Old Item | 10.00 | ✘ (สั่งซื้อจะได้ `404`) |

**ตัวอย่างยอดรวม**

| Request | total |
|---|---|
| `product-123` × 2 | `198.00` |
| `product-456` × 3 | `148.50` |
| `sword-001` × 1 | `250.00` |

**ตัวอย่างข้อมูลสำหรับทดสอบ**

| ใช้กับ | ค่า |
|---|---|
| deviceId | `a1b2c3d4e5f6g7h8i9j0`, `dev-device-0000000001` |
| email / password | `player@example.com` / `password123` |
| Idempotency-Key | `3f2c1e9a-7b4d-4e8a-9c1f-2a6b5d8e0f11` (หรือ `uuidgen` / `Guid.NewGuid()`) |
