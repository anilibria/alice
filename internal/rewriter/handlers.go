package rewriter

import "github.com/gofiber/fiber/v2"

func (*Rewriter) HandleDummy(c *fiber.Ctx) error {
	return respondPlainWithStatus(c, fiber.StatusNoContent)
}

func (*Rewriter) HandleUnavailable(*fiber.Ctx) error {
	return fiber.NewError(fiber.StatusServiceUnavailable, "not inited yet")
}

func (m *Rewriter) HandleIndex(c *fiber.Ctx) (e error) {
	if e = m.rewrite(c); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	}

	return respondPlainWithStatus(c, fiber.StatusNoContent)
}
