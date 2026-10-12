package httpapi

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/operations"
)

// dashboardHandler returns current active-unit totals.
//
// @Summary Get operational dashboard totals
// @Tags Operations
// @Produce json
// @Success 200 {object} DataResponse{data=operations.Dashboard}
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/dashboard [get]
func dashboardHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			apiError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "operations service is unavailable")
			return
		}
		result, err := service.Dashboard(c.Request.Context())
		if err != nil {
			slog.Error("load dashboard failed", "error", err)
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load dashboard")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": result})
	}
}

// unitsHandler lists units and their current movement status.
//
// @Summary List units
// @Tags Operations
// @Produce json
// @Param query query string false "Search no_lambung or no_polisi"
// @Param status query string false "Current direction" Enums(IN, OUT, NULL)
// @Param destination query string false "Current OUT destination" Enums(BENGKEL_LUAR, FILLING_SHED_KUIN, CUSTOMER, OTHER)
// @Param includeInactive query bool false "Include inactive units"
// @Param page query int false "Page number" default(1) minimum(1)
// @Param pageSize query int false "Page size" default(25) minimum(1) maximum(100)
// @Success 200 {object} ListResponse{data=[]operations.Unit}
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/units [get]
func unitsHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := unitFilter(c)
		units, page, err := service.ListUnits(c.Request.Context(), filter)
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load units")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": units, "pagination": page})
	}
}

// unitHandler returns one unit and its current status.
//
// @Summary Get unit detail
// @Tags Operations
// @Produce json
// @Param id path int true "Unit Lambung ID" minimum(1)
// @Success 200 {object} DataResponse{data=operations.Unit}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 404 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/units/{id} [get]
func unitHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		unit, err := service.Unit(c.Request.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			apiError(c, http.StatusNotFound, "UNIT_NOT_FOUND", "unit was not found")
			return
		}
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load unit")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": unit})
	}
}

// unitTransactionsHandler lists the transaction timeline for one unit.
//
// @Summary List unit transactions
// @Tags Operations
// @Produce json
// @Param id path int true "Unit Lambung ID" minimum(1)
// @Param page query int false "Page number" default(1) minimum(1)
// @Param pageSize query int false "Page size" default(25) minimum(1) maximum(100)
// @Success 200 {object} ListResponse{data=[]operations.Transaction}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/units/{id}/transactions [get]
func unitTransactionsHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		filter, err := transactionFilter(c)
		if err != nil {
			apiError(c, http.StatusBadRequest, "INVALID_FILTER", "date filters must be RFC3339 timestamps")
			return
		}
		filter.UnitID = &id
		transactions, page, err := service.ListTransactions(c.Request.Context(), filter)
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load transactions")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": transactions, "pagination": page})
	}
}

// transactionsHandler lists the immutable transaction audit trail.
//
// @Summary List transactions
// @Tags Operations
// @Produce json
// @Param query query string false "Search no_lambung or no_polisi"
// @Param direction query string false "Movement direction" Enums(IN, OUT)
// @Param detail query string false "IN category or OUT destination"
// @Param from query string false "Inclusive RFC3339 UTC timestamp"
// @Param to query string false "Exclusive RFC3339 UTC timestamp"
// @Param page query int false "Page number" default(1) minimum(1)
// @Param pageSize query int false "Page size" default(25) minimum(1) maximum(100)
// @Success 200 {object} ListResponse{data=[]operations.Transaction}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/transactions [get]
func transactionsHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter, err := transactionFilter(c)
		if err != nil {
			apiError(c, http.StatusBadRequest, "INVALID_FILTER", "date filters must be RFC3339 timestamps")
			return
		}
		transactions, page, err := service.ListTransactions(c.Request.Context(), filter)
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not load transactions")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": transactions, "pagination": page})
	}
}
