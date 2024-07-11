package proxy

import "github.com/gofiber/fiber/v2"

func (m *Proxy) HandleProxyToDst(c *fiber.Ctx) (e error) {
	if e = m.ProxyFiberRequest(c); e != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, e.Error())
	}

	return
}

// func (m *Rewriter) HandleIndex(c *fiber.Ctx) (e error) {
// 	if e = m.rewrite(c); e != nil {
// 		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
// 	}

// 	return respondPlainWithStatus(c, fiber.StatusTeapot)
// }
