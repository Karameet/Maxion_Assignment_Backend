package httpapi

import "github.com/gofiber/fiber/v3"

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(ErrorBody{
		Error: ErrorDetail{Code: code, Message: message},
	})
}

func Unauthorized(c fiber.Ctx, msg string) error {
	return WriteError(c, fiber.StatusUnauthorized, CodeUnauthorized, msg)
}

// Internal never leaks error details to the client.
func Internal(c fiber.Ctx) error {
	return WriteError(c, fiber.StatusInternalServerError, CodeInternal, "internal server error")
}

// errorHandler maps framework-level errors (unknown route, body too large,
// wrong method, panics recovered as errors) onto the standard envelope.
func errorHandler(c fiber.Ctx, err error) error {
	fe, ok := err.(*fiber.Error)
	if !ok {
		return Internal(c)
	}
	switch fe.Code {
	case fiber.StatusNotFound:
		return WriteError(c, fe.Code, CodeNotFound, "route not found")
	case fiber.StatusMethodNotAllowed:
		return WriteError(c, fe.Code, CodeMethodNotAllowed, "method not allowed")
	case fiber.StatusRequestEntityTooLarge:
		return WriteError(c, fe.Code, CodePayloadTooLarge, "request body too large")
	case fiber.StatusUnauthorized:
		return WriteError(c, fe.Code, CodeUnauthorized, fe.Message)
	case fiber.StatusTooManyRequests:
		return WriteError(c, fe.Code, CodeRateLimited, "too many requests")
	case fiber.StatusBadRequest:
		return WriteError(c, fe.Code, CodeBadRequest, fe.Message)
	}
	if fe.Code >= 400 && fe.Code < 500 {
		return WriteError(c, fe.Code, CodeBadRequest, fe.Message)
	}
	return Internal(c)
}
