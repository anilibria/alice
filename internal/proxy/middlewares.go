package proxy

import "github.com/gofiber/fiber/v2"

func (m *Proxy) MiddlewareValidation(c *fiber.Ctx) (e error) {
	v := m.NewValidator(c)
	if e = v.ValidateRequest(); e != nil {
		return fiber.NewError(fiber.StatusBadRequest, e.Error())
	}

	// continue request processing
	e = c.Next()

	v.Destroy()
	return
}
