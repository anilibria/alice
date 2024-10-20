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

	// set ALICE cache status
	c.Response().Header.Set("X-Alice-Cache", "MISS")

	// hijack all query=random_release queries
	if v.IsQueryEqual([]byte("random_release")) {
		return m.middlewareRandomReleaseRequest(c)
	}

	// hijack all query=random queries
	if v.IsQueryEqual([]byte("release")) && v.ArgsLen() == 2 {
		return m.middlewareReleaseRequest(c, v)
	}

	// continue request processing
	return c.Next()
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

///
///
///

func (m *Proxy) middlewareRandomReleaseRequest(c *fiber.Ctx) (e error) {
	if m.randomizer == nil {
		return c.Next() // bypass randomizer module
	}

	country := m.countryByRemoteIP(c)

	var release string
	if release, e = m.randomizer.Randomize(country); e != nil {
		rlog(c).Error().Msg("could not respond on random release query - " + e.Error())
		return c.Next() // bypass randomizer module
	} else if release == "" {
		return c.Next() // bypass randomizer module
	}

	if e = utils.RespondWithRandomRelease(release, c); e != nil {
		rlog(c).Error().Msg("could not respond on random release query - " + e.Error())
		return c.Next() // bypass randomizer module
	}

	c.Response().Header.Set("X-Alice-Cache", "HIT")
	return respondPlainWithStatus(c, fiber.StatusOK)
}

func (m *Proxy) middlewareReleaseRequest(c *fiber.Ctx, v *Validator) (e error) {
	if m.randomizer == nil {
		return c.Next() // bypass randomizer module
	}

	var ok bool
	var ident []byte
	if ident, ok = v.Arg([]byte("code")); !ok {
		if ident, ok = v.Arg([]byte("id")); !ok {
			return c.Next() // bypass to origin
		}
	}

	var release []byte
	if release, ok, e = m.randomizer.RawRelease(ident); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	} else if !ok {
		rlog(c).Warn().Msg("BUG: empty raw data fetched from Releases.Release.Raw")
		return c.Next() // bypass to origin
	}

	if e = utils.RespondWithRawJSON(release, c); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	}

	fmt.Println("returned cached value")

	c.Response().Header.Set("X-Alice-Cache", "HIT")
	return respondPlainWithStatus(c, fiber.StatusOK)
}
