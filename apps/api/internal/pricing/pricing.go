package pricing

import (
	"errors"
	"math"
)

const (
	DefaultMatchRadiusKM = 20
	roadDistanceFactor   = 1.3
	earthRadiusMeters    = 6_371_008.8
	averageSpeedMPerHour = 25_000
)

var (
	ErrUnsupportedCategory = errors.New("unsupported ride category")
	ErrInvalidEstimate     = errors.New("invalid estimate input")
)

type Location struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Area      string  `json:"area"`
	City      string  `json:"city"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type AvailableDriver struct {
	City      string
	Category  string
	Latitude  float64
	Longitude float64
	Available bool
}

type Quote struct {
	EstimatedFarePaisa      int64  `json:"estimated_fare_paisa"`
	EstimatedDistanceMeters int64  `json:"estimated_distance_meters"`
	PickupETAMinutes        *int   `json:"pickup_eta_minutes"`
	Currency                string `json:"currency"`
	Approximate             bool   `json:"approximate"`
}

func CalculateQuote(pickup, destination Location, category string, drivers []AvailableDriver, matchRadiusKM int) (Quote, error) {
	if !validCoordinates(pickup.Latitude, pickup.Longitude) || !validCoordinates(destination.Latitude, destination.Longitude) || pickup.ID == destination.ID || matchRadiusKM < 1 {
		return Quote{}, ErrInvalidEstimate
	}

	distanceMeters := DistanceMeters(pickup.Latitude, pickup.Longitude, destination.Latitude, destination.Longitude)
	fare, err := CalculateFare(category, distanceMeters)
	if err != nil {
		return Quote{}, err
	}

	var nearestDriverDistance int64
	foundDriver := false
	maximumDistance := int64(matchRadiusKM) * 1000
	for _, driver := range drivers {
		if !driver.Available || driver.City != "Lahore" || driver.Category != category || !validCoordinates(driver.Latitude, driver.Longitude) {
			continue
		}
		driverDistance := DistanceMeters(driver.Latitude, driver.Longitude, pickup.Latitude, pickup.Longitude)
		if driverDistance > maximumDistance {
			continue
		}
		if !foundDriver || driverDistance < nearestDriverDistance {
			nearestDriverDistance = driverDistance
			foundDriver = true
		}
	}

	var eta *int
	if foundDriver {
		minutes := int((nearestDriverDistance*60 + averageSpeedMPerHour - 1) / averageSpeedMPerHour)
		if minutes < 2 {
			minutes = 2
		}
		eta = &minutes
	}

	return Quote{
		EstimatedFarePaisa:      fare,
		EstimatedDistanceMeters: distanceMeters,
		PickupETAMinutes:        eta,
		Currency:                "PKR",
		Approximate:             true,
	}, nil
}

func CalculateFare(category string, distanceMeters int64) (int64, error) {
	if distanceMeters < 1 {
		return 0, ErrInvalidEstimate
	}

	var basePaisa, perKilometerPaisa int64
	switch category {
	case "economy":
		basePaisa, perKilometerPaisa = 15_000, 6_000
	case "comfort":
		basePaisa, perKilometerPaisa = 25_000, 9_000
	default:
		return 0, ErrUnsupportedCategory
	}

	variableFare := (distanceMeters*perKilometerPaisa + 999) / 1000
	return basePaisa + variableFare, nil
}

// DistanceMeters returns the demo's approximate road distance: Haversine
// distance multiplied by 1.3 and rounded to the nearest whole meter.
func DistanceMeters(fromLatitude, fromLongitude, toLatitude, toLongitude float64) int64 {
	fromLat := degreesToRadians(fromLatitude)
	toLat := degreesToRadians(toLatitude)
	deltaLat := toLat - fromLat
	deltaLon := degreesToRadians(toLongitude - fromLongitude)

	haversine := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(fromLat)*math.Cos(toLat)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	if haversine < 0 {
		haversine = 0
	} else if haversine > 1 {
		haversine = 1
	}
	straightLineMeters := 2 * earthRadiusMeters * math.Asin(math.Sqrt(haversine))
	roadMeters := int64(math.Round(straightLineMeters * roadDistanceFactor))
	if roadMeters < 1 {
		return 1
	}
	return roadMeters
}

func validCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsNaN(longitude) && !math.IsInf(latitude, 0) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func degreesToRadians(value float64) float64 {
	return value * math.Pi / 180
}
