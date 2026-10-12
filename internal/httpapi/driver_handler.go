package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/integration"
)

type DriverSearcher interface {
	ActiveDrivers(context.Context, string) ([]integration.ActiveDriver, error)
}

type activeDriverResponse struct {
	ID         int64  `json:"id"`
	DriverName string `json:"driverName"`
}

// driversHandler searches drivers active in the Driver service.
//
// @Summary Search active drivers
// @Tags Movements
// @Produce json
// @Param name query string true "Driver name"
// @Success 200 {object} DataResponse{data=[]activeDriverResponse}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/drivers [get]
func driversHandler(searcher DriverSearcher) gin.HandlerFunc {
	return func(c *gin.Context) {
		if searcher == nil {
			apiError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "driver service is unavailable")
			return
		}
		name := strings.TrimSpace(c.Query("name"))
		if name == "" {
			apiError(c, http.StatusBadRequest, "INVALID_DRIVER_QUERY", "name query parameter is required")
			return
		}

		drivers, err := searcher.ActiveDrivers(c.Request.Context(), name)
		if err != nil {
			slog.Error("search active drivers failed", "error", err)
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load drivers")
			return
		}
		response := make([]activeDriverResponse, len(drivers))
		for index, driver := range drivers {
			response[index] = activeDriverResponse{ID: driver.ID, DriverName: driver.DriverName}
		}
		c.JSON(http.StatusOK, gin.H{"data": response})
	}
}
