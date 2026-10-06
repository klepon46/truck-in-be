package httpapi

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "truckin-be/docs"
	"truckin-be/internal/auth"
	"truckin-be/internal/movement"
	"truckin-be/internal/operations"
)

const (
	appPermission = "unitlog:app:default:view"
	webPermission = "unitlog:web:default:view"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type Pinger interface {
	PingContext(context.Context) error
}

type Dependencies struct {
	Database           Pinger
	Movement           *movement.Service
	Operations         *operations.Service
	JWTSecret          []byte
	PermissionClaim    string
	ActorIDClaim       string
	ActorNameClaim     string
	CORSAllowedOrigins []string
}

func NewRouter(dependencies Dependencies) *gin.Engine {
	router := gin.New()
	router.SetTrustedProxies(nil)
	router.Use(gin.Recovery())
	router.Use(cors(dependencies.CORSAllowedOrigins))
	router.GET("/health", health(dependencies.Database))
	router.GET("/swagger-ui/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	router.GET("/swagger/*any", func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, "/swagger-ui"+c.Param("any"))
	})

	api := router.Group("/api/v1")
	api.Use(auth.Authenticate(dependencies.JWTSecret))
	api.POST("/movements", auth.RequirePermission(dependencies.PermissionClaim, appPermission), movementHandler(dependencies))

	operationsAPI := api.Group("/")
	operationsAPI.Use(auth.RequirePermission(dependencies.PermissionClaim, webPermission))
	operationsAPI.GET("/dashboard", dashboardHandler(dependencies.Operations))
	operationsAPI.GET("/units", unitsHandler(dependencies.Operations))
	operationsAPI.GET("/units/export", unitsExportHandler(dependencies.Operations))
	operationsAPI.GET("/units/:id", unitHandler(dependencies.Operations))
	operationsAPI.GET("/units/:id/transactions", unitTransactionsHandler(dependencies.Operations))
	operationsAPI.GET("/transactions", transactionsHandler(dependencies.Operations))
	operationsAPI.GET("/transactions/export", transactionsExportHandler(dependencies.Operations))
	return router
}

func cors(origins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		if _, found := allowed[origin]; !found {
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Next()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Idempotency-Key")
		c.Header("Vary", "Origin")
		if c.Request.Method == http.MethodOptions {
			if c.GetHeader("Access-Control-Request-Method") != http.MethodGet && c.GetHeader("Access-Control-Request-Method") != http.MethodPost {
				c.AbortWithStatus(http.StatusMethodNotAllowed)
				return
			}
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Next()
	}
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

type movementRequest struct {
	// Required for all movements.
	NoLambung string `json:"noLambung" validate:"required" example:"SAMT223"`
	// Required for all movements. IN requires inCategory; OUT requires outDestination and its matching fields.
	Direction movement.Direction `json:"direction" validate:"required" enums:"IN,OUT" example:"OUT"`
	// Required only for IN. Must not be supplied for OUT.
	InCategory string `json:"inCategory" enums:"WAITING_FOR_ASSIGNMENT,WAITING_FOR_DELIVERY_TO_CUSTOMER,SERVICE_AND_REPAIR_MAINTENANCE"`
	// Required only for OUT. Must not be supplied for IN.
	OutDestination string `json:"outDestination" enums:"BENGKEL_LUAR,FILLING_SHED_KUIN,CUSTOMER,OTHER"`
	// Required only when outDestination is BENGKEL_LUAR.
	WorkOrderNumber string `json:"workOrderNumber" example:"WO-001"`
	// Required only when outDestination is FILLING_SHED_KUIN or CUSTOMER.
	SPPNumber string `json:"sppNumber" example:"SPP-001"`
	// Required only when outDestination is OTHER.
	DriverID int64 `json:"driverId" example:"42"`
	// Required only when outDestination is OTHER.
	DriverName string `json:"driverName" example:"Driver Name"`
	// Required only when outDestination is OTHER; optional for other OUT destinations.
	Note string `json:"note" example:"Operational note"`
}

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

// movementHandler records an immutable truck movement.
//
// @Summary Record a unit movement
// @Description Records an IN or OUT transaction. Retries must reuse the same Idempotency-Key and request body. IN requires inCategory only. OUT requires BENGKEL_LUAR with workOrderNumber, FILLING_SHED_KUIN or CUSTOMER with sppNumber, or OTHER with driverId, driverName, and note.
// @Tags Movements
// @Accept json
// @Produce json
// @Param Idempotency-Key header string true "UUID generated once per movement"
// @Param request body movementRequest true "Movement request"
// @Success 201 {object} DataResponse{data=movement.Transaction}
// @Failure 400 {object} APIError
// @Failure 401 {object} APIError
// @Failure 403 {object} APIError
// @Failure 404 {object} APIError
// @Failure 409 {object} APIError
// @Failure 422 {object} APIError
// @Failure 500 {object} APIError
// @Security BearerAuth
// @Router /api/v1/movements [post]
func movementHandler(dependencies Dependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		if dependencies.Movement == nil {
			apiError(c, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "movement service is unavailable")
			return
		}
		key := c.GetHeader("Idempotency-Key")
		if !idempotencyKeyPattern.MatchString(key) {
			apiError(c, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must be a UUID")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		var request movementRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			apiError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body is invalid")
			return
		}
		actorID, validID := auth.ClaimString(c, dependencies.ActorIDClaim)
		actorName, validName := auth.ClaimString(c, dependencies.ActorNameClaim)
		if !validID || !validName {
			apiError(c, http.StatusForbidden, "INVALID_ACTOR", "required actor claims are missing")
			return
		}
		result, err := dependencies.Movement.Record(c.Request.Context(), key, movement.Input{
			NoLambung: request.NoLambung, Direction: request.Direction, InCategory: request.InCategory,
			OutDestination: request.OutDestination, WorkOrderNumber: request.WorkOrderNumber,
			SPPNumber: request.SPPNumber, DriverID: request.DriverID, DriverName: request.DriverName, Note: request.Note,
		}, actorID, actorName)
		if err != nil {
			logMovementFailure(movement.Input{
				NoLambung:      request.NoLambung,
				Direction:      request.Direction,
				InCategory:     request.InCategory,
				OutDestination: request.OutDestination,
			}, err)
			movementError(c, err)
			return
		}
		c.JSON(http.StatusCreated, gin.H{"data": result})
	}
}

func logMovementFailure(input movement.Input, err error) {
	slog.Error("record movement failed",
		"error", err,
		"no_lambung", input.NoLambung,
		"direction", input.Direction,
		"in_category", input.InCategory,
		"out_destination", input.OutDestination,
	)
}

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

func movementError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, movement.ErrInvalidInput):
		apiError(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "movement input is invalid")
	case errors.Is(err, movement.ErrUnitNotFound):
		apiError(c, http.StatusNotFound, "UNIT_NOT_FOUND", "unit was not found")
	case errors.Is(err, movement.ErrInvalidTransition):
		apiError(c, http.StatusUnprocessableEntity, "INVALID_TRANSITION", "movement transition is not allowed")
	case errors.Is(err, movement.ErrIdempotencyConflict):
		apiError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "Idempotency-Key was reused with a different request")
	case errors.Is(err, movement.ErrWorkOrderUsed):
		apiError(c, http.StatusConflict, "WORK_ORDER_USED", "work order has already been used")
	case errors.Is(err, movement.ErrSPPUsed):
		apiError(c, http.StatusConflict, "SPP_USED", "SPP has already been used")
	case errors.Is(err, movement.ErrSPPMismatch), errors.Is(err, movement.ErrCustomerMismatch), errors.Is(err, movement.ErrExternalDocument):
		apiError(c, http.StatusUnprocessableEntity, "DOCUMENT_VALIDATION_FAILED", "movement document is invalid")
	default:
		apiError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "could not record movement")
	}
}

func apiError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
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
