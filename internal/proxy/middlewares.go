package proxy

import (
	"bytes"

	"github.com/gofiber/fiber/v2"
)

func (*Proxy) MiddlewareValidation(c *fiber.Ctx) (e error) {
	v := AcquireValidator(c, c.Request().Header.ContentType())
	defer ReleaseValidator(v)

	if e = v.ValidateRequest(); e != nil {
		return fiber.NewError(fiber.StatusBadRequest, e.Error())
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
