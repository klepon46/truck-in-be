package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"truckin-be/internal/auth"
	"truckin-be/internal/movement"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

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
