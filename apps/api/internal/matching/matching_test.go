package matching

import "testing"

func TestEligibleChecksAvailabilityCityCategoryAndRadius(t *testing.T) {
	driver := Driver{
		City:      "Lahore",
		Status:    "available",
		Latitude:  31.5113,
		Longitude: 74.3436,
		Categories: map[string]struct{}{
			"economy": {},
		},
	}
	if !Eligible(driver, "economy", "Lahore", 31.5113, 74.3436, 20) {
		t.Fatal("nearby available Lahore driver with matching category was rejected")
	}
	if Eligible(driver, "comfort", "Lahore", 31.5113, 74.3436, 20) {
		t.Fatal("driver without the requested category was accepted")
	}
	if Eligible(driver, "economy", "Karachi", 31.5113, 74.3436, 20) {
		t.Fatal("driver from another city was accepted")
	}
	driver.Status = "offline"
	if Eligible(driver, "economy", "Lahore", 31.5113, 74.3436, 20) {
		t.Fatal("offline driver was accepted")
	}
	driver.Status = "available"
	if Eligible(driver, "economy", "Lahore", 31.5113, 74.3436, 0) {
		t.Fatal("invalid radius was accepted")
	}
	if Eligible(driver, "economy", "Lahore", 31.9, 74.9, 20) {
		t.Fatal("driver outside radius was accepted")
	}
}
