package httpapi

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// maxFaultDelay stays below the server WriteTimeout so the delayed response
// can still be written if the client waits for it.
const maxFaultDelay = 10 * time.Second

var faultStatusCodes = map[int]string{
	fiber.StatusUnauthorized:        CodeUnauthorized,
	fiber.StatusForbidden:           CodeForbidden,
	fiber.StatusInternalServerError: CodeInternal,
	fiber.StatusServiceUnavailable:  CodeServiceUnavailable,
}

type faultSpec struct {
	delay           time.Duration
	status          int
	dropAfterCommit bool
}

// FaultInjection is a DEV-ONLY middleware driven by the X-Debug-Fault header,
// so a client can rehearse timeouts and error handling against a real server.
// Directives are comma-separated:
//
//	delay=5s            sleep before the handler, then process normally
//	status=500|503|403|401   answer immediately with that error, skip the handler
//	drop-after-commit   run the handler (order is created), then answer 500
//
// The router mounts it only when APP_ENV=dev && ENABLE_FAULT_INJECTION=true.
func FaultInjection() fiber.Handler {
	return func(c fiber.Ctx) error {
		raw := c.Get(HeaderDebugFault)
		if raw == "" {
			return c.Next()
		}
		spec, err := parseFault(raw)
		if err != nil {
			return WriteError(c, fiber.StatusBadRequest, CodeBadRequest, "invalid "+HeaderDebugFault+": "+err.Error())
		}
		slog.Warn("fault injection", "path", c.Path(), "fault", raw)

		if spec.delay > 0 {
			time.Sleep(spec.delay)
		}
		if spec.status != 0 {
			return WriteError(c, spec.status, faultStatusCodes[spec.status], "injected fault")
		}
		if err := c.Next(); err != nil {
			return err
		}
		if spec.dropAfterCommit && c.Response().StatusCode() < 300 {
			// The work is committed; only the response is "lost".
			c.Response().Header.Del(HeaderIdempotentReplayed)
			return WriteError(c, fiber.StatusInternalServerError, CodeInternal, "injected fault after commit")
		}
		return nil
	}
}

func parseFault(raw string) (faultSpec, error) {
	var spec faultSpec
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		name, value, _ := strings.Cut(part, "=")
		switch name {
		case "delay":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 || d > maxFaultDelay {
				return spec, fmt.Errorf("delay must be a duration in (0, %s]", maxFaultDelay)
			}
			spec.delay = d
		case "status":
			n, err := strconv.Atoi(value)
			if _, ok := faultStatusCodes[n]; err != nil || !ok {
				return spec, fmt.Errorf("status must be one of 401, 403, 500, 503")
			}
			spec.status = n
		case "drop-after-commit":
			spec.dropAfterCommit = true
		default:
			return spec, fmt.Errorf("unknown directive %q", name)
		}
	}
	return spec, nil
}
