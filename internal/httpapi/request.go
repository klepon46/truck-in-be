package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/operations"
)

func unitFilter(c *gin.Context) operations.UnitFilter {
	return operations.UnitFilter{Page: operations.NormalizePage(queryInt(c, "page"), queryInt(c, "pageSize")), Query: c.Query("query"), IncludeInactive: c.Query("includeInactive") == "true", Status: c.Query("status"), Destination: c.Query("destination")}
}

func transactionFilter(c *gin.Context) (operations.TransactionFilter, error) {
	filter := operations.TransactionFilter{Page: operations.NormalizePage(queryInt(c, "page"), queryInt(c, "pageSize")), Query: c.Query("query"), Direction: c.Query("direction"), Detail: c.Query("detail")}
	for key, destination := range map[string]**time.Time{"from": &filter.From, "to": &filter.To} {
		if value := c.Query(key); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return filter, err
			}
			*destination = &parsed
		}
	}
	return filter, nil
}

func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		apiError(c, http.StatusBadRequest, "INVALID_UNIT_ID", "unit id must be a positive integer")
		return 0, false
	}
	return id, true
}

func queryInt(c *gin.Context, key string) int {
	value, _ := strconv.Atoi(c.Query(key))
	return value
}
