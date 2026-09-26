package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"
)

var errNotJSON = errors.New("Content-Type must be application/json")

// decodeStrict parses the request body as exactly one JSON object, rejecting
// a non-JSON Content-Type, unknown fields (so a client cannot smuggle in
// "total" or "price"), and trailing data. Numbers decode as json.Number.
func decodeStrict(c fiber.Ctx, dst any) error {
	ct := strings.ToLower(strings.TrimSpace(c.Get(fiber.HeaderContentType)))
	if !strings.HasPrefix(ct, fiber.MIMEApplicationJSON) {
		return errNotJSON
	}
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON object")
	}
	return nil
}

// invalidJSON writes 400 INVALID_JSON. Unknown-field errors name the field
// (it came from the client, so echoing it back is safe and helps debugging).
func invalidJSON(c fiber.Ctx, err error) error {
	msg := "request body must be a single valid JSON object"
	switch {
	case errors.Is(err, errNotJSON):
		msg = err.Error()
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		msg = "unknown field " + strings.TrimPrefix(err.Error(), "json: unknown field ")
	}
	return WriteError(c, fiber.StatusBadRequest, CodeInvalidJSON, msg)
}
