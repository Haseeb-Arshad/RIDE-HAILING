package matching

import (
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/pricing"
)

type Driver struct {
	City       string
	Status     string
	Categories map[string]struct{}
	Latitude   float64
	Longitude  float64
}

func Eligible(driver Driver, category, pickupCity string, pickupLatitude, pickupLongitude float64, radiusKM int) bool {
	if driver.Status != "available" || driver.City != pickupCity || pickupCity != "Lahore" || radiusKM < 1 {
		return false
	}
	if _, ok := driver.Categories[category]; !ok {
		return false
	}
	return PickupDistanceMeters(driver, pickupLatitude, pickupLongitude) <= int64(radiusKM)*1000
}

func PickupDistanceMeters(driver Driver, pickupLatitude, pickupLongitude float64) int64 {
	return pricing.DistanceMeters(driver.Latitude, driver.Longitude, pickupLatitude, pickupLongitude)
}
