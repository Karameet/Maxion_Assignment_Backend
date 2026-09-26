#!/usr/bin/env bash
# End-to-end scenario against a running API + Postgres.
# Requires: API with APP_ENV=dev and ENABLE_FAULT_INJECTION=true, curl, python, docker compose (for DB checks).
# Usage: BASE=http://localhost:8080 bash scripts/e2e.sh
set -u
B=${BASE:-http://localhost:8080}
TS=$(date +%s)
PASS=0; FAIL=0

uuid() { python -c "import uuid;print(uuid.uuid4())"; }
jf()   { python -c "import sys,json;print(json.load(sys.stdin)$1)"; }

# req <label> <expected-status> <curl args...>
req() {
  local label=$1 exp=$2; shift 2
  local out code body mark
  out=$(curl -s -w '\n%{http_code}' "$@"); code=${out##*$'\n'}; body=${out%$'\n'*}
  if [ "$code" = "$exp" ]; then mark=PASS; PASS=$((PASS+1)); else mark=FAIL; FAIL=$((FAIL+1)); fi
  printf '%-4s %-38s want=%s got=%s  %s\n' "$mark" "$label" "$exp" "$code" "$(echo "$body" | tr -d '\r' | tail -1 | cut -c1-150)"
}
check() { # check <label> <want> <got>
  if [ "$2" = "$3" ]; then echo "PASS $1: $3"; PASS=$((PASS+1)); else echo "FAIL $1: want $2 got $3"; FAIL=$((FAIL+1)); fi
}
J=(-H 'Content-Type: application/json')

echo "== Health / products"
req "healthz" 200 "$B/healthz"
req "products" 200 "$B/api/products"
req "unknown route" 404 "$B/api/nope"
req "wrong method" 405 -X PUT "$B/api/orders"

echo "== Auth"
TOKEN=$(curl -s -X POST "$B/api/auth/guest" "${J[@]}" -d "{\"deviceId\":\"e2e-device-$TS\"}" | jf "['token']")
req "guest (same device again)" 200 -X POST "$B/api/auth/guest" "${J[@]}" -d "{\"deviceId\":\"e2e-device-$TS\"}"
req "guest short deviceId" 400 -X POST "$B/api/auth/guest" "${J[@]}" -d '{"deviceId":"short"}'
EMAIL="player$TS@example.com"
req "register" 201 -X POST "$B/api/auth/register" "${J[@]}" -d "{\"email\":\"$EMAIL\",\"password\":\"password123\"}"
req "register duplicate (upper case)" 409 -X POST "$B/api/auth/register" "${J[@]}" -d "{\"email\":\"PLAYER$TS@example.com\",\"password\":\"password123\"}"
req "register invalid email" 400 -X POST "$B/api/auth/register" "${J[@]}" -d '{"email":"not-an-email","password":"password123"}'
req "register weak password" 400 -X POST "$B/api/auth/register" "${J[@]}" -d '{"email":"weak@example.com","password":"1234567"}'
req "login wrong password" 401 -X POST "$B/api/auth/login" "${J[@]}" -d "{\"email\":\"$EMAIL\",\"password\":\"wrongpass\"}"
req "login unknown email" 401 -X POST "$B/api/auth/login" "${J[@]}" -d '{"email":"nobody@example.com","password":"password123"}'
req "login ok" 200 -X POST "$B/api/auth/login" "${J[@]}" -d "{\"email\":\"$EMAIL\",\"password\":\"password123\"}"
ETOKEN=$(curl -s -X POST "$B/api/auth/login" "${J[@]}" -d "{\"email\":\"$EMAIL\",\"password\":\"password123\"}" | jf "['token']")
req "me (email token)" 200 "$B/api/me" -H "Authorization: Bearer $ETOKEN"
req "me (no token)" 401 "$B/api/me"

echo "== Orders"
A=(-H "Authorization: Bearer $TOKEN")
KEY=$(uuid)
req "create" 201 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $KEY" "${J[@]}" -d '{"productId":"product-123","quantity":2}'
req "replay same key" 201 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $KEY" "${J[@]}" -d '{"productId":"product-123","quantity":2}'
req "same key, different body" 422 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $KEY" "${J[@]}" -d '{"productId":"product-123","quantity":3}'
req "no token" 401 -X POST "$B/api/orders" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":2}'
req "garbage token" 401 -X POST "$B/api/orders" -H "Authorization: Bearer abc" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":2}'
req "missing key" 400 -X POST "$B/api/orders" "${A[@]}" "${J[@]}" -d '{"productId":"product-123","quantity":2}'
req "bad key" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: bad key" "${J[@]}" -d '{"productId":"product-123","quantity":2}'
req "client sends total" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":2,"total":1}'
req "wrong content-type" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" -H 'Content-Type: text/plain' -d '{"productId":"product-123","quantity":2}'
req "quantity 0" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":0}'
req "quantity 2.5" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":2.5}'
req "quantity \"2\"" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":"2"}'
req "quantity 100 (> max)" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":100}'
req "productId number" 400 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":123,"quantity":1}'
req "unknown product" 404 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"nope-999","quantity":1}'
req "retired product" 404 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"retired-001","quantity":1}'
req "list orders" 200 "$B/api/orders" "${A[@]}"

echo "== Fault injection"
KEY2=$(uuid)
req "delay=3s, client timeout 1s" 000 -m 1 -X POST "$B/api/orders" -H 'X-Debug-Fault: delay=3s' "${A[@]}" -H "Idempotency-Key: $KEY2" "${J[@]}" -d '{"productId":"sword-001","quantity":1}'
sleep 3
H=$(curl -s -D - -o /dev/null -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $KEY2" "${J[@]}" -d '{"productId":"sword-001","quantity":1}' | tr -d '\r')
check "retry after timeout → replayed" "true" "$(echo "$H" | grep -i '^Idempotent-Replayed' | awk '{print $2}')"
KEY3=$(uuid)
req "drop-after-commit" 500 -X POST "$B/api/orders" -H 'X-Debug-Fault: drop-after-commit' "${A[@]}" -H "Idempotency-Key: $KEY3" "${J[@]}" -d '{"productId":"product-456","quantity":3}'
req "retry after dropped response" 201 -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $KEY3" "${J[@]}" -d '{"productId":"product-456","quantity":3}'
req "status=503" 503 -X POST "$B/api/orders" -H 'X-Debug-Fault: status=503' "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":1}'
req "status=403" 403 -X POST "$B/api/orders" -H 'X-Debug-Fault: status=403' "${A[@]}" -H "Idempotency-Key: $(uuid)" "${J[@]}" -d '{"productId":"product-123","quantity":1}'
req "unknown fault directive" 400 "$B/healthz" -H 'X-Debug-Fault: explode'

echo "== 30 parallel requests, same key"
KEY4=$(uuid); TMP=$(mktemp -d)
for i in $(seq 1 30); do
  curl -s -o "$TMP/b_$i" -D "$TMP/h_$i" -X POST "$B/api/orders" "${A[@]}" -H "Idempotency-Key: $KEY4" "${J[@]}" -d '{"productId":"product-123","quantity":1}' &
done; wait
check "all 30 got 201" 30 "$(grep -l '201 Created' "$TMP"/h_* | wc -l | tr -d ' ')"
check "distinct order ids" 1 "$(cat "$TMP"/b_* | python -c "import sys,re;print(len(set(re.findall(r'\"id\":\"([^\"]+)\"',sys.stdin.read()))))")"
check "responses with Replayed=false" 1 "$(grep -il 'Idempotent-Replayed: false' "$TMP"/h_* | wc -l | tr -d ' ')"
rm -rf "$TMP"

echo "== Link guest -> email"
req "link" 200 -X POST "$B/api/auth/link" "${A[@]}" "${J[@]}" -d "{\"email\":\"linked$TS@example.com\",\"password\":\"password123\"}"
req "link again" 409 -X POST "$B/api/auth/link" "${A[@]}" "${J[@]}" -d "{\"email\":\"x$TS@example.com\",\"password\":\"password123\"}"

echo "== Verify in Postgres"
Q() { docker compose exec -T db psql -U orders -d orders -tAc "$1" | tr -d '\r'; }
for k in "$KEY" "$KEY2" "$KEY3" "$KEY4"; do check "rows for key ${k:0:8}" 1 "$(Q "SELECT count(*) FROM orders WHERE idempotency_key='$k'")"; done
check "total_cents product-123 x2" 19800 "$(Q "SELECT total_cents FROM orders WHERE idempotency_key='$KEY'")"
check "total_cents product-456 x3" 14850 "$(Q "SELECT total_cents FROM orders WHERE idempotency_key='$KEY3'")"
check "password stored as bcrypt" '$2a$12$' "$(Q "SELECT left(password_hash,7) FROM users WHERE email='$EMAIL'")"
check "linked account keeps guest orders" 4 "$(Q "SELECT count(*) FROM orders o JOIN users u ON u.id=o.user_id WHERE u.email='linked$TS@example.com'")"

echo
echo "RESULT: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
