package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/movement"
)

type DocumentLookup interface {
	LookupWorkshop(context.Context, string) (movement.LookupDocument, error)
	LookupSPP(context.Context, string) (movement.LookupDocument, error)
}

// workshopLookupHandler looks up the unit and driver details for a scanned work order.
//
// @Summary Look up a scanned work order
// @Tags Movements
// @Produce json
// @Param number query string true "Work order number"
// @Success 200 {object} DataResponse{data=movement.LookupDocument}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 404 {object} APIError
// @Failure 422 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/movement-documents/work-orders [get]
func workshopLookupHandler(lookup DocumentLookup) gin.HandlerFunc {
	return documentLookupHandler(lookup, func(ctx context.Context, number string) (movement.LookupDocument, error) {
		return lookup.LookupWorkshop(ctx, number)
	})
}

// sppLookupHandler looks up the unit, driver, and customer details for a scanned SPP.
//
// @Summary Look up a scanned SPP
// @Tags Movements
// @Produce json
// @Param number query string true "SPP number"
// @Success 200 {object} DataResponse{data=movement.LookupDocument}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 404 {object} APIError
// @Failure 422 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/movement-documents/spp [get]
func sppLookupHandler(lookup DocumentLookup) gin.HandlerFunc {
	return documentLookupHandler(lookup, func(ctx context.Context, number string) (movement.LookupDocument, error) {
		return lookup.LookupSPP(ctx, number)
	})
}

func documentLookupHandler(lookup DocumentLookup, find func(context.Context, string) (movement.LookupDocument, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if lookup == nil {
			apiError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "movement document service is unavailable")
			return
		}
		number := strings.TrimSpace(c.Query("number"))
		if number == "" {
			apiError(c, http.StatusBadRequest, "INVALID_DOCUMENT_QUERY", "document number is required")
			return
		}
		result, err := find(c.Request.Context(), number)
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"data": result})
			return
		}
		switch {
		case errors.Is(err, movement.ErrInvalidInput):
			apiError(c, http.StatusBadRequest, "INVALID_DOCUMENT_QUERY", "document number is invalid")
		case errors.Is(err, movement.ErrUnitNotFound):
			apiError(c, http.StatusNotFound, "UNIT_NOT_FOUND", "document unit was not found")
		case errors.Is(err, movement.ErrExternalDocument):
			apiError(c, http.StatusUnprocessableEntity, "DOCUMENT_VALIDATION_FAILED", "movement document is invalid")
		default:
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not look up movement document")
		}
	}
}
