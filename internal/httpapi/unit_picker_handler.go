package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/operations"
)

type UnitPicker interface {
	ListUnits(context.Context, operations.UnitFilter) ([]operations.Unit, operations.Page, error)
}

// unitPickerHandler lists active units for an Android OTHER movement.
//
// @Summary List units for the movement picker
// @Tags Movements
// @Produce json
// @Param query query string false "Search no_lambung or no_polisi"
// @Param page query int false "Page number" default(1) minimum(1)
// @Param pageSize query int false "Page size" default(25) minimum(1) maximum(100)
// @Success 200 {object} ListResponse{data=[]operations.Unit}
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/unit-picker [get]
func unitPickerHandler(picker UnitPicker) gin.HandlerFunc {
	return func(c *gin.Context) {
		if picker == nil {
			apiError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "unit picker service is unavailable")
			return
		}
		units, page, err := picker.ListUnits(c.Request.Context(), unitFilter(c))
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load unit picker")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": units, "pagination": page})
	}
}
