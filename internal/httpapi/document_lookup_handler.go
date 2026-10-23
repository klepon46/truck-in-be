package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/movement"
)

type DocumentLookup interface {
	LookupWorkshop(context.Context, string) (movement.LookupDocument, error)
	LookupSPP(context.Context, string) (movement.SPPLookupDocument, error)
}

// workshopLookupHandler looks up the unit and driver details for a scanned work order.
//
// @Summary Look up a scanned work order
// @Description Returns 404 WORK_ORDER_NOT_FOUND for confirmed Workshop absence, or 404 UNIT_NOT_FOUND for an unresolved document unit. Invalid document data remains 422; integration failures return 500.
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
	return documentLookupHandler(lookup, "WO", func(ctx context.Context, number string) (any, error) {
		return lookup.LookupWorkshop(ctx, number)
	})
}

// sppLookupHandler looks up the unit, driver, and customer details for a scanned SPP.
//
// @Summary Look up a scanned SPP
// @Description Returns document data and backend-derived allowedOutDestinations. An empty array prevents a new SPP OUT but does not invalidate a returning-IN lookup. Submit always revalidates current eligibility.
// @Description Accepts an ID-owner reference (123-example), splitting on the first hyphen and encoding SAOS id and owner separately. Echoes the reference as sppNumber for submit, but eligibility uses the canonical SAOS trans_no. Malformed references return 400 INVALID_DOCUMENT_QUERY.
// @Description Returns 404 SPP_NOT_FOUND for an empty/null successful SAOS result, or 404 UNIT_NOT_FOUND for an unresolved document unit. Conflicting document projections return 422; missing/malformed data and integration failures return 500.
// @Tags Movements
// @Produce json
// @Param number query string true "SPP reference in {id}-{owner} format, e.g. 123-example; owner may contain hyphens"
// @Success 200 {object} DataResponse{data=movement.SPPLookupDocument}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 404 {object} APIError
// @Failure 422 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/movement-documents/spp [get]
func sppLookupHandler(lookup DocumentLookup) gin.HandlerFunc {
	return documentLookupHandler(lookup, "SPP", func(ctx context.Context, number string) (any, error) {
		return lookup.LookupSPP(ctx, number)
	})
}

func documentLookupHandler(lookup DocumentLookup, documentType string, find func(context.Context, string) (any, error)) gin.HandlerFunc {
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
		// Only log allowlisted reasons; never log upstream bodies or personal data.
		reason := "internal_error"
		switch {
		case errors.Is(err, movement.ErrInvalidInput):
			reason = "invalid_document_query"
		case errors.Is(err, movement.ErrIntegrationUnavailable):
			reason = "integration_unavailable"
		case errors.Is(err, movement.ErrWorkOrderNotFound):
			reason = "wo_not_found"
		case errors.Is(err, movement.ErrSPPNotFound):
			reason = "spp_not_found"
		case errors.Is(err, movement.ErrDocumentUnitMismatch):
			reason = "unit_police_mismatch"
		case errors.Is(err, movement.ErrUnitNotFound):
			reason = "unit_not_found"
		case errors.Is(err, movement.ErrExternalDocument):
			reason = "document_invalid"
		}
		slog.ErrorContext(c.Request.Context(), "movement document lookup failed", "document_type", documentType, "reason", reason)
		switch {
		case errors.Is(err, movement.ErrInvalidInput):
			apiError(c, http.StatusBadRequest, "INVALID_DOCUMENT_QUERY", "document number is invalid")
		case errors.Is(err, movement.ErrWorkOrderNotFound):
			apiError(c, http.StatusNotFound, "WORK_ORDER_NOT_FOUND", "WO tidak ditemukan di Workshop.")
		case errors.Is(err, movement.ErrSPPNotFound):
			apiError(c, http.StatusNotFound, "SPP_NOT_FOUND", "SPP tidak ditemukan di SAOS.")
		case errors.Is(err, movement.ErrUnitNotFound):
			apiError(c, http.StatusNotFound, "UNIT_NOT_FOUND", "document unit was not found")
		case errors.Is(err, movement.ErrDocumentUnitMismatch):
			apiError(c, http.StatusUnprocessableEntity, "DOCUMENT_VALIDATION_FAILED", "document vehicle or police number does not match Unit Lambung; contact operations to check master data")
		case errors.Is(err, movement.ErrSPPUnitMissing):
			apiError(c, http.StatusUnprocessableEntity, "DOCUMENT_VALIDATION_FAILED", "Kode Unit Lambung tidak tersedia pada data SPP dari SAOS. Hubungi operasional untuk memeriksa data unit.")
		case errors.Is(err, movement.ErrExternalDocument):
			apiError(c, http.StatusUnprocessableEntity, "DOCUMENT_VALIDATION_FAILED", "movement document is invalid")
		default:
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not look up movement document")
		}
	}
}
