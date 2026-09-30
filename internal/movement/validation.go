package movement

import (
	"errors"
	"strings"
)

type Direction string

const (
	DirectionIn  Direction = "IN"
	DirectionOut Direction = "OUT"

	InWaitingForAssignment = "WAITING_FOR_ASSIGNMENT"
	InWaitingForDelivery   = "WAITING_FOR_DELIVERY_TO_CUSTOMER"
	InServiceAndRepair     = "SERVICE_AND_REPAIR_MAINTENANCE"

	OutWorkshop    = "BENGKEL_LUAR"
	OutFillingShed = "FILLING_SHED_KUIN"
	OutCustomer    = "CUSTOMER"
	OutOther       = "OTHER"
)

var (
	ErrInvalidInput      = errors.New("invalid movement input")
	ErrInvalidTransition = errors.New("invalid movement transition")
)

type Input struct {
	NoLambung       string
	Direction       Direction
	InCategory      string
	OutDestination  string
	WorkOrderNumber string
	SPPNumber       string
	DriverID        int64
	DriverName      string
	Note            string
}

type Status struct {
	Direction  Direction
	InCategory string
}

func (i *Input) Normalize() {
	i.NoLambung = strings.ToUpper(strings.TrimSpace(i.NoLambung))
	i.InCategory = strings.ToUpper(strings.TrimSpace(i.InCategory))
	i.OutDestination = strings.ToUpper(strings.TrimSpace(i.OutDestination))
	i.WorkOrderNumber = strings.TrimSpace(i.WorkOrderNumber)
	i.SPPNumber = strings.TrimSpace(i.SPPNumber)
	i.DriverName = strings.TrimSpace(i.DriverName)
	i.Note = strings.TrimSpace(i.Note)
}

func (i Input) Validate() error {
	if i.NoLambung == "" || (i.Direction != DirectionIn && i.Direction != DirectionOut) {
		return ErrInvalidInput
	}
	if i.Direction == DirectionIn {
		if !isInCategory(i.InCategory) || i.OutDestination != "" || i.WorkOrderNumber != "" || i.SPPNumber != "" || i.DriverID != 0 || i.DriverName != "" || i.Note != "" {
			return ErrInvalidInput
		}
		return nil
	}
	if !isOutDestination(i.OutDestination) || i.InCategory != "" {
		return ErrInvalidInput
	}
	switch i.OutDestination {
	case OutWorkshop:
		if i.WorkOrderNumber == "" || i.SPPNumber != "" || i.DriverID != 0 || i.DriverName != "" {
			return ErrInvalidInput
		}
	case OutFillingShed, OutCustomer:
		if i.SPPNumber == "" || i.WorkOrderNumber != "" || i.DriverID != 0 || i.DriverName != "" {
			return ErrInvalidInput
		}
	case OutOther:
		if i.WorkOrderNumber != "" || i.SPPNumber != "" || i.DriverID == 0 || i.DriverName == "" || i.Note == "" {
			return ErrInvalidInput
		}
	}
	return nil
}

func ValidateTransition(status Status, direction Direction, inCategory, outDestination string) error {
	if direction == DirectionIn {
		if !isInCategory(inCategory) {
			return ErrInvalidTransition
		}
		return nil
	}
	if status.Direction == DirectionOut || !isOutDestination(outDestination) {
		return ErrInvalidTransition
	}
	if status.Direction == DirectionIn && status.InCategory == InWaitingForDelivery {
		if outDestination != OutCustomer {
			return ErrInvalidTransition
		}
		return nil
	}
	if outDestination == OutCustomer {
		return ErrInvalidTransition
	}
	return nil
}

func isInCategory(value string) bool {
	return value == InWaitingForAssignment || value == InWaitingForDelivery || value == InServiceAndRepair
}

func isOutDestination(value string) bool {
	return value == OutWorkshop || value == OutFillingShed || value == OutCustomer || value == OutOther
}
