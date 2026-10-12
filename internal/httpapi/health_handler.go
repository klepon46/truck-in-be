package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Pinger interface {
	PingContext(context.Context) error
}

// health reports the service and database readiness.
//
// @Summary Health check
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /health [get]
func health(database Pinger) gin.HandlerFunc {
	return func(requestContext *gin.Context) {
		context, cancel := context.WithTimeout(requestContext.Request.Context(), time.Second)
		defer cancel()
		if err := database.PingContext(context); err != nil {
			requestContext.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		requestContext.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
