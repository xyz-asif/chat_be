package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// MVP Launch: Rate Limiting Configuration - Completed

// RateLimiterConfig returns a configured rate limiter middleware
func RateLimiterConfig() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        100, // Default: 100 requests
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			// Use user ID if authenticated, otherwise IP
			user := c.Locals("user")
			if user != nil {
				return "user:" + c.Locals("userID").(string)
			}
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":   "rate_limit_exceeded",
				"message": "Too many requests. Please try again later.",
			})
		},
	})
}

// StrictRateLimiter for write operations (likes, comments, follows, etc.)
func StrictRateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        20, // 20 requests per minute for writes
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			user := c.Locals("user")
			if user != nil {
				return "user:" + c.Locals("userID").(string)
			}
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":   "rate_limit_exceeded",
				"message": "Too many write operations. Please slow down.",
			})
		},
	})
}

// GenerousRateLimiter for read operations
func GenerousRateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        300, // 300 requests per minute for reads
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			user := c.Locals("user")
			if user != nil {
				return "user:" + c.Locals("userID").(string)
			}
			return c.IP()
		},
	})
}
