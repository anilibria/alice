package proxy

import (
	"bytes"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
)

func (m *Proxy) MiddlewareValidation(c *fiber.Ctx) (e error) {
	v := AcquireValidator(c, c.Request().Header.ContentType())
	defer ReleaseValidator(v)

	if e = v.ValidateRequest(); e != nil {
		return fiber.NewError(fiber.StatusBadRequest, e.Error())
	}

	// set ALICE cache status
	c.Response().Header.Set("X-Alice-Cache", "MISS")

	// hijack all query=random_release queries
	if v.IsQueryEqual([]byte("random_release")) {
		if m.randomizer != nil {
			if release := m.randomizer.Randomize(); release != "" {
				if e = utils.RespondWithRandomRelease(release, c); e == nil {
					c.Response().Header.Set("X-Alice-Cache", "HIT")
					return respondPlainWithStatus(c, fiber.StatusOK)
				}
				rlog(c).Error().Msg("could not respond on random release query - " + e.Error())
			}
		}
	}

	// continue request processing
	e = c.Next()
	return
}

func (m *Proxy) MiddlewareInternalApi(c *fiber.Ctx) (_ error) {
	isecret := c.Context().Request.Header.Peek("x-api-secret")

	if len(isecret) == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "secret key is empty or invalid")
	}

	if !bytes.Equal(m.config.apiSecret, isecret) {
		return fiber.NewError(fiber.StatusUnauthorized, "secret key is empty or invalid")
	}

	return c.Next()
}
