package httpapi

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/operations"
)

// unitsExportHandler exports matching units as CSV.
//
// @Summary Export units
// @Tags Operations
// @Produce text/csv
// @Param query query string false "Search no_lambung or no_polisi"
// @Param status query string false "Current direction" Enums(IN, OUT, NULL)
// @Param destination query string false "Current OUT destination" Enums(BENGKEL_LUAR, FILLING_SHED_KUIN, CUSTOMER, OTHER)
// @Param includeInactive query bool false "Include inactive units"
// @Success 200 {file} file
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/units/export [get]
func unitsExportHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter := unitFilter(c)
		filter.Page = operations.NormalizePage(1, 100)
		units, _, err := service.ListUnits(c.Request.Context(), filter)
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not export units")
			return
		}
		c.Header("Content-Disposition", `attachment; filename="units.csv"`)
		c.Header("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(c.Writer)
		_ = writer.Write([]string{"no_lambung", "no_polisi", "status", "destination", "driver", "last_updated_at"})
		for _, unit := range units {
			_ = writer.Write([]string{csvValue(unit.NoLambung), csvValue(unit.NoPolisi), csvValue(unit.Status), csvValue(value(unit.OutDestination)), csvValue(value(unit.DriverName)), csvValue(timeValue(unit.LastUpdatedAt))})
		}
		writer.Flush()
	}
}

// transactionsExportHandler exports the filtered audit trail as CSV.
//
// @Summary Export transactions
// @Tags Operations
// @Produce text/csv
// @Param query query string false "Search no_lambung or no_polisi"
// @Param direction query string false "Movement direction" Enums(IN, OUT)
// @Param detail query string false "IN category or OUT destination"
// @Param from query string false "Inclusive RFC3339 UTC timestamp"
// @Param to query string false "Exclusive RFC3339 UTC timestamp"
// @Success 200 {file} file
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/transactions/export [get]
func transactionsExportHandler(service *operations.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter, err := transactionFilter(c)
		if err != nil {
			apiError(c, http.StatusBadRequest, "INVALID_FILTER", "date filters must be RFC3339 timestamps")
			return
		}
		filter.Page = operations.NormalizePage(1, 100)
		transactions, _, err := service.ListTransactions(c.Request.Context(), filter)
		if err != nil {
			apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not export transactions")
			return
		}
		c.Header("Content-Disposition", `attachment; filename="transactions.csv"`)
		c.Header("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(c.Writer)
		_ = writer.Write([]string{"transaction_id", "occurred_at", "no_lambung", "direction", "detail", "driver", "work_order", "spp", "customer", "actor", "note"})
		for _, transaction := range transactions {
			detail := value(transaction.OutDestination)
			if transaction.InCategory != nil {
				detail = *transaction.InCategory
			}
			_ = writer.Write([]string{strconv.FormatInt(transaction.ID, 10), transaction.OccurredAt.UTC().Format(time.RFC3339), csvValue(transaction.NoLambung), csvValue(transaction.Direction), csvValue(detail), csvValue(value(transaction.DriverName)), csvValue(value(transaction.WorkOrderNumber)), csvValue(value(transaction.SPPNumber)), csvValue(value(transaction.CustomerName)), csvValue(transaction.ActorName), csvValue(value(transaction.Note))})
		}
		writer.Flush()
	}
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}

func timeValue(pointer *time.Time) string {
	if pointer == nil {
		return ""
	}
	return pointer.UTC().Format(time.RFC3339)
}

func csvValue(value string) string {
	if len(value) > 0 && strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}
