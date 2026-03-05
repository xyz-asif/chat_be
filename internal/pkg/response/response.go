package response

import "github.com/gofiber/fiber/v2"

// ErrorResponse represents a structured error response
type ErrorResponse struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// SuccessResponse represents a structured success response
type SuccessResponse struct {
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// SendError sends a structured error response
func SendError(c *fiber.Ctx, status int, err string, message string, details map[string]interface{}) error {
	return c.Status(status).JSON(ErrorResponse{
		Error:   err,
		Message: message,
		Details: details,
	})
}

// SendValidationError sends a validation error with field details
func SendValidationError(c *fiber.Ctx, message string, fieldErrors map[string]string) error {
	details := make(map[string]interface{})
	if fieldErrors != nil {
		details["fields"] = fieldErrors
	}
	return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{
		Error:   "validation_error",
		Message: message,
		Details: details,
	})
}

// SendSuccess sends a structured success response
func SendSuccess(c *fiber.Ctx, message string, data interface{}) error {
	return c.JSON(SuccessResponse{
		Message: message,
		Data:    data,
	})
}

// SendData sends just the data without wrapper
func SendData(c *fiber.Ctx, data interface{}) error {
	return c.JSON(data)
}

// SendCreated sends a 201 Created response
func SendCreated(c *fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusCreated).JSON(data)
}
