package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/jufabeck2202/piScraper/internal/core/ports"
)

type VerifyEmailHandler struct {
	mailService ports.MailService
}

func NewVerifMailHandler(mailService ports.MailService) *VerifyEmailHandler {
	return &VerifyEmailHandler{
		mailService: mailService,
	}
}

func (hdl *VerifyEmailHandler) Get(c *fiber.Ctx) error {
	fmt.Println(c.Params("email"))
	email := c.Params("email")
	decytedEmail, err := hdl.mailService.Verify(email)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": true,
			"msg":   err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"error": false,
		"msg":   decytedEmail,
	})
}
