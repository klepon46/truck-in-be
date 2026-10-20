package movement

import (
	"math/big"
	"regexp"
	"strings"
)

const decimalCoordinate = `[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)`

var coordinatesPattern = regexp.MustCompile(`^` + decimalCoordinate + `,` + decimalCoordinate + `$`)

func normalizeCoordinates(value string) string {
	parts := strings.Split(value, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return strings.Join(parts, ",")
}

func validCoordinates(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 100 || !coordinatesPattern.MatchString(value) {
		return false
	}
	parts := strings.SplitN(value, ",", 2)
	// Compare exact decimals so rounding cannot admit a value just outside a bound.
	latitude, latitudeOK := new(big.Rat).SetString(parts[0])
	longitude, longitudeOK := new(big.Rat).SetString(parts[1])
	return latitudeOK && longitudeOK && latitude.Abs(latitude).Cmp(big.NewRat(90, 1)) <= 0 && longitude.Abs(longitude).Cmp(big.NewRat(180, 1)) <= 0
}
