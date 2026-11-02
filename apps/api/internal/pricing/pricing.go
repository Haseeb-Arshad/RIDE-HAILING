package pricing

import (
	"errors"
	"math"
	"time"
)

const (
	DefaultMatchRadiusKM  = 20
	roadDistanceFactorBPS = 13_000
	earthRadiusMeters     = 6_371_008.8
	averageSpeedMPerHour  = 25_000
	LahoreMinLatitude     = 31.20
	LahoreMaxLatitude     = 31.75
	LahoreMinLongitude    = 73.95
	LahoreMaxLongitude    = 74.70
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
	EstimatedFarePaisa      int64     `json:"estimated_fare_paisa"`
	EstimatedDistanceMeters int64     `json:"estimated_distance_meters"`
	PickupETAMinutes        *int      `json:"pickup_eta_minutes"`
	FuelPricePaisaPerLiter  int64     `json:"fuel_price_paisa_per_liter"`
	FuelPriceEffectiveDate  time.Time `json:"fuel_price_effective_date"`
	FuelCostPaisa           int64     `json:"fuel_cost_paisa"`
	BaseFarePaisa           int64     `json:"base_fare_paisa"`
	DistanceCostPaisa       int64     `json:"distance_cost_paisa"`
	PricingVersion          int       `json:"pricing_version"`
	Currency                string    `json:"currency"`
	Approximate             bool      `json:"approximate"`
}

type RateCard struct {
	Category                     string
	FuelPricePaisaPerLiter       int64
	FuelPriceEffectiveDate       time.Time
	FuelEfficiencyMetersPerLiter int64
	BaseFarePaisa                int64
	NonFuelCostPaisaPerKM        int64
	RoadDistanceFactorBPS        int64
	Version                      int
}

func CalculateQuote(pickup, destination Location, category string, drivers []AvailableDriver, matchRadiusKM int) (Quote, error) {
	rateCard, err := DefaultRateCard(category)
	if err != nil {
		return Quote{}, err
	}
	return CalculateQuoteWithRateCard(pickup, destination, category, drivers, matchRadiusKM, rateCard)
}

func CalculateQuoteWithRateCard(pickup, destination Location, category string, drivers []AvailableDriver, matchRadiusKM int, rateCard RateCard) (Quote, error) {
	if !validCoordinates(pickup.Latitude, pickup.Longitude) || !validCoordinates(destination.Latitude, destination.Longitude) || (pickup.ID != "" && pickup.ID == destination.ID) || matchRadiusKM < 1 {
		return Quote{}, ErrInvalidEstimate
	}
	if !validRateCard(rateCard) || rateCard.Category != category {
		if category != "economy" && category != "comfort" {
			return Quote{}, ErrUnsupportedCategory
		}
		return Quote{}, ErrInvalidEstimate
	}

	distanceMeters := distanceMetersWithFactor(pickup.Latitude, pickup.Longitude, destination.Latitude, destination.Longitude, rateCard.RoadDistanceFactorBPS)
	if distanceMeters <= 1 {
		return Quote{}, ErrInvalidEstimate
	}
	fare, fuelCost, distanceCost, err := CalculateFareWithRateCard(distanceMeters, rateCard)
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
		FuelPricePaisaPerLiter:  rateCard.FuelPricePaisaPerLiter,
		FuelPriceEffectiveDate:  rateCard.FuelPriceEffectiveDate,
		FuelCostPaisa:           fuelCost,
		BaseFarePaisa:           rateCard.BaseFarePaisa,
		DistanceCostPaisa:       distanceCost,
		PricingVersion:          rateCard.Version,
		Currency:                "PKR",
		Approximate:             true,
	}, nil
}

func CalculateFare(category string, distanceMeters int64) (int64, error) {
	rateCard, err := DefaultRateCard(category)
	if err != nil {
		return 0, err
	}
	fare, _, _, err := CalculateFareWithRateCard(distanceMeters, rateCard)
	return fare, err
}

func CalculateFareWithRateCard(distanceMeters int64, rateCard RateCard) (farePaisa, fuelCostPaisa, distanceCostPaisa int64, err error) {
	if distanceMeters < 1 || !validRateCard(rateCard) {
		return 0, 0, 0, ErrInvalidEstimate
	}
	fuelCostPaisa = (distanceMeters*rateCard.FuelPricePaisaPerLiter + rateCard.FuelEfficiencyMetersPerLiter - 1) / rateCard.FuelEfficiencyMetersPerLiter
	distanceCostPaisa = (distanceMeters*rateCard.NonFuelCostPaisaPerKM + 999) / 1000
	farePaisa = rateCard.BaseFarePaisa + fuelCostPaisa + distanceCostPaisa
	return farePaisa, fuelCostPaisa, distanceCostPaisa, nil
}

// DistanceMeters returns the demo's approximate road distance: Haversine
// distance multiplied by 1.3 and rounded to the nearest whole meter.
func DistanceMeters(fromLatitude, fromLongitude, toLatitude, toLongitude float64) int64 {
	return distanceMetersWithFactor(fromLatitude, fromLongitude, toLatitude, toLongitude, roadDistanceFactorBPS)
}

func distanceMetersWithFactor(fromLatitude, fromLongitude, toLatitude, toLongitude float64, roadFactorBasisPoints int64) int64 {
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
	roadMeters := int64(math.Round(straightLineMeters * float64(roadFactorBasisPoints) / 10_000))
	if roadMeters < 1 {
		return 1
	}
	return roadMeters
}

func DefaultRateCard(category string) (RateCard, error) {
	switch category {
	case "economy":
		return RateCard{Category: category, FuelPricePaisaPerLiter: 37_791, FuelPriceEffectiveDate: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC), FuelEfficiencyMetersPerLiter: 12_000, BaseFarePaisa: 15_000, NonFuelCostPaisaPerKM: 2_850, RoadDistanceFactorBPS: roadDistanceFactorBPS, Version: 1}, nil
	case "comfort":
		return RateCard{Category: category, FuelPricePaisaPerLiter: 37_791, FuelPriceEffectiveDate: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC), FuelEfficiencyMetersPerLiter: 9_000, BaseFarePaisa: 25_000, NonFuelCostPaisaPerKM: 2_800, RoadDistanceFactorBPS: roadDistanceFactorBPS, Version: 1}, nil
	default:
		return RateCard{}, ErrUnsupportedCategory
	}
}

func validRateCard(rateCard RateCard) bool {
	return rateCard.FuelPricePaisaPerLiter > 0 &&
		!rateCard.FuelPriceEffectiveDate.IsZero() &&
		rateCard.FuelEfficiencyMetersPerLiter > 0 &&
		rateCard.BaseFarePaisa >= 0 &&
		rateCard.NonFuelCostPaisaPerKM >= 0 &&
		rateCard.RoadDistanceFactorBPS >= 10_000 &&
		rateCard.RoadDistanceFactorBPS <= 30_000 &&
		rateCard.Version > 0
}

func validCoordinates(latitude, longitude float64) bool {
	return !math.IsNaN(latitude) && !math.IsNaN(longitude) && !math.IsInf(latitude, 0) && !math.IsInf(longitude, 0) && latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180
}

func IsInLahoreServiceArea(latitude, longitude float64) bool {
	return validCoordinates(latitude, longitude) &&
		latitude >= LahoreMinLatitude && latitude <= LahoreMaxLatitude &&
		longitude >= LahoreMinLongitude && longitude <= LahoreMaxLongitude
}

func degreesToRadians(value float64) float64 {
	return value * math.Pi / 180
}
