package service

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

func rlog(c *fiber.Ctx) *zerolog.Logger {
	return c.Locals("logger").(*zerolog.Logger)
}
