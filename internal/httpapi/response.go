package httpapi

import (
	"github.com/gin-gonic/gin"

	"truckin-be/internal/operations"
)

// APIError is the standard API error response.
type APIError struct {
	Error struct {
		Code    string `json:"code" example:"VALIDATION_ERROR"`
		Message string `json:"message" example:"movement input is invalid"`
	} `json:"error"`
}

// DataResponse wraps one API result.
type DataResponse struct {
	Data any `json:"data"`
}

// ListResponse wraps a paginated API result.
type ListResponse struct {
	Data       any             `json:"data"`
	Pagination operations.Page `json:"pagination"`
}

func apiError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
