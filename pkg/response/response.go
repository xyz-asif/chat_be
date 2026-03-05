// ================== internal/pkg/response/response.go ==================
package response

import (
	"github.com/gofiber/fiber/v2"
)

// Standard API response structure
type APIResponse struct {
	Success    bool        `json:"success" example:"true"`
	StatusCode int         `json:"statusCode" example:"200"`
	Message    string      `json:"message" example:"Operation successful"`
	Data       interface{} `json:"data,omitempty"` // omitted if nil
}

// FailureResponse struct for Swagger docs (to show success: false)
// Note: statusCode will match the actual HTTP status code returned
type FailureResponse struct {
	Success    bool   `json:"success" example:"false"`
	StatusCode int    `json:"statusCode" example:"400"`
	Message    string `json:"message" example:"Error description"`
}

// Success sends a successful response
func Success(c *fiber.Ctx, statusCode int, message string, data interface{}) error {
	return c.Status(statusCode).JSON(APIResponse{
		Success:    true,
		StatusCode: statusCode,
		Message:    message,
		Data:       data,
	})
}

// Error sends an error response
func Error(c *fiber.Ctx, statusCode int, message string, data interface{}) error {
	return c.Status(statusCode).JSON(APIResponse{
		Success:    false,
		StatusCode: statusCode,
		Message:    message,
		Data:       data,
	})
}

// Common helpers — use these everywhere!
func OK(c *fiber.Ctx, message string, data interface{}) error {
	return Success(c, fiber.StatusOK, message, data)
}

func Created(c *fiber.Ctx, message string, data interface{}) error {
	return Success(c, fiber.StatusCreated, message, data)
}

func BadRequest(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusBadRequest, message, nil)
}

func Unauthorized(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusUnauthorized, message, nil)
}

func Forbidden(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusForbidden, message, nil)
}

func NotFound(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusNotFound, message, nil)
}

func ValidationFailed(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusUnprocessableEntity, message, nil)
}

func InternalError(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusInternalServerError, message, nil)
}

func Conflict(c *fiber.Ctx, message string) error {
	return Error(c, fiber.StatusConflict, message, nil)
}

// Special helpers
func NoContent(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}
