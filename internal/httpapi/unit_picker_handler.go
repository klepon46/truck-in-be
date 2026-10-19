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

type unitPickerResponse struct {
	NoLambung string `json:"noLambung"`
	NoPolisi  string `json:"noPolisi"`
}

// unitPickerHandler lists active units for an Android OTHER movement.
//
// @Summary List units for the movement picker
// @Tags Movements
// @Produce json
// @Param query query string false "Search no_lambung or no_polisi"
// @Param page query int false "Page number" default(1) minimum(1)
// @Param pageSize query int false "Page size" default(25) minimum(1) maximum(100)
// @Success 200 {object} ListResponse{data=[]unitPickerResponse}
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
		filter := unitFilter(c)
		filter.IncludeInactive = false
		units, page, err := picker.ListUnits(c.Request.Context(), filter)
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load unit picker")
			return
		}
		response := make([]unitPickerResponse, len(units))
		for index, unit := range units {
			response[index] = unitPickerResponse{NoLambung: unit.NoLambung, NoPolisi: unit.NoPolisi}
		}
		c.JSON(http.StatusOK, gin.H{"data": response, "pagination": page})
	}
}
