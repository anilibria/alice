package proxy

import (
	"bytes"
	"fmt"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
)

func (m *Proxy) MiddlewareValidation(c *fiber.Ctx) (e error) {
	v := AcquireValidator(c, c.Request().Header.ContentType())
	defer ReleaseValidator(v)

	if e = v.ValidateRequest(); e != nil {
		return fiber.NewError(fiber.StatusBadRequest, e.Error())
	}

	if v.IsQueryEqual([]byte("random_release")) {
		fmt.Println("this is random_release")
		if m.randomizer != nil && m.randomizer.IsReady() {
			if release := m.randomizer.Randomize(); release != "" {
				fmt.Fprintln(c, release)
				fmt.Println("all good - ", release)
				if e = utils.RespondWithRandomRelease(fiber.StatusOK, release, c); e == nil {
					return respondPlainWithStatus(c, fiber.StatusOK)
				}
				rlog(c).Error().Msg("could not respond on random release query - " + e.Error())
			}
			fmt.Println("randomizer empty result")
		}
		fmt.Println("randomizer falls")
	}

	// {"status":true,"data":{"code":"nande-koko-ni-sensei-ga"},"error":null}

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
