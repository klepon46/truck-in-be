package movement

import (
	"regexp"
	"strconv"
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
	latitude, latitudeError := strconv.ParseFloat(parts[0], 64)
	longitude, longitudeError := strconv.ParseFloat(parts[1], 64)
	return latitudeError == nil && longitudeError == nil && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}
