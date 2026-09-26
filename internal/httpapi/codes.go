package httpapi

// Error codes returned in {"error":{"code":...}}. Clients (Unity) must branch
// on HTTP status + code only; message is for humans. Keep this file as the
// single source of truth so it can be mirrored as a client-side enum.
const (
	// 400
	CodeInvalidJSON           = "INVALID_JSON"
	CodeInvalidDeviceID       = "INVALID_DEVICE_ID"
	CodeInvalidEmail          = "INVALID_EMAIL"
	CodeWeakPassword          = "WEAK_PASSWORD"
	CodeMissingIdempotencyKey = "MISSING_IDEMPOTENCY_KEY"
	CodeInvalidIdempotencyKey = "INVALID_IDEMPOTENCY_KEY"
	CodeInvalidProductID      = "INVALID_PRODUCT_ID"
	CodeInvalidQuantity       = "INVALID_QUANTITY"
	CodeBadRequest            = "BAD_REQUEST"

	// 401 / 403
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeInvalidCredentials = "INVALID_CREDENTIALS"
	CodeForbidden          = "FORBIDDEN"

	// 404 / 405
	CodeNotFound         = "NOT_FOUND"
	CodeProductNotFound  = "PRODUCT_NOT_FOUND"
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"

	// 409 / 413 / 422 / 429
	CodeEmailTaken           = "EMAIL_TAKEN"
	CodeAlreadyLinked        = "ALREADY_LINKED"
	CodePayloadTooLarge      = "PAYLOAD_TOO_LARGE"
	CodeIdempotencyKeyReused = "IDEMPOTENCY_KEY_REUSED"
	CodeRateLimited          = "RATE_LIMITED"

	// 5xx
	CodeInternal           = "INTERNAL"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

const (
	HeaderIdempotencyKey     = "Idempotency-Key"
	HeaderIdempotentReplayed = "Idempotent-Replayed"
	HeaderDebugFault         = "X-Debug-Fault"
)
