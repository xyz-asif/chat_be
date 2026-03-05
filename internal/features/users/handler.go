package users

import (
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/models"
	pkgErrors "github.com/xyz-asif/gotodo/pkg/errors"
	"github.com/xyz-asif/gotodo/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// MVP Launch: Get Current User - Completed
// GetMe retrieves the current user profile
func (h *Handler) GetMe(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	return response.OK(c, "User profile retrieved successfully", user)
}

// MVP Feature: User Profile Management - Completed
// UpdateProfile updates the current user profile
func (h *Handler) UpdateProfile(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var updates map[string]interface{}
	if err := c.BodyParser(&updates); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	updatedUser, err := h.service.UpdateProfile(c.Context(), user.ID.Hex(), updates)
	if err != nil {
		// Check error type and return appropriate status code
		if pkgErrors.IsValidation(err) {
			return response.ValidationFailed(c, err.Error())
		}
		if pkgErrors.IsNotFound(err) {
			return response.NotFound(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Profile updated successfully", updatedUser)
}

// FollowUser allows a user to follow another user
func (h *Handler) FollowUser(c *fiber.Ctx) error {
	targetUserID := c.Params("id")
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	if err := h.service.FollowUser(c.Context(), user.ID.Hex(), targetUserID); err != nil {
		// Check error type and return appropriate status code
		if pkgErrors.IsConflict(err) {
			return response.Conflict(c, err.Error())
		}
		if pkgErrors.IsNotFound(err) {
			return response.NotFound(c, err.Error())
		}
		if pkgErrors.IsValidation(err) {
			return response.BadRequest(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Successfully followed user", nil)
}

// UnfollowUser allows a user to unfollow another user
func (h *Handler) UnfollowUser(c *fiber.Ctx) error {
	currentUser, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	targetID := c.Params("id")
	if err := h.service.UnfollowUser(c.Context(), currentUser.ID.Hex(), targetID); err != nil {
		// Check error type and return appropriate status code
		if pkgErrors.IsNotFound(err) {
			return response.NotFound(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Unfollowed successfully", nil)
}
