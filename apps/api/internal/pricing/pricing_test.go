package pricing

import (
	"errors"
	"testing"
)

func TestCalculateQuoteUsesDocumentedDistanceAndFareRounding(t *testing.T) {
	pickup := Location{ID: "pickup", City: "Lahore", Latitude: 0, Longitude: 0}
	destination := Location{ID: "destination", City: "Lahore", Latitude: 0, Longitude: 0.01}

	economy, err := CalculateQuote(pickup, destination, "economy", nil, DefaultMatchRadiusKM)
	if err != nil {
		t.Fatalf("calculate economy quote: %v", err)
	}
	if economy.EstimatedDistanceMeters != 1446 || economy.EstimatedFarePaisa != 23_676 {
		t.Fatalf("economy quote = distance %d fare %d, want distance 1446 and fare 23676", economy.EstimatedDistanceMeters, economy.EstimatedFarePaisa)
	}
	if economy.FuelPricePaisaPerLiter != 37_791 || economy.FuelCostPaisa != 4_554 || economy.BaseFarePaisa != 15_000 || economy.DistanceCostPaisa != 4_122 {
		t.Fatalf("economy fare breakdown = fuel price %d, fuel %d, base %d, distance %d", economy.FuelPricePaisaPerLiter, economy.FuelCostPaisa, economy.BaseFarePaisa, economy.DistanceCostPaisa)
	}
	if economy.PickupETAMinutes != nil || economy.Currency != "PKR" || !economy.Approximate {
		t.Fatalf("unexpected no-driver quote fields: %+v", economy)
	}

	comfort, err := CalculateQuote(pickup, destination, "comfort", nil, DefaultMatchRadiusKM)
	if err != nil {
		t.Fatalf("calculate comfort quote: %v", err)
	}
	if comfort.EstimatedFarePaisa != 35_121 {
		t.Fatalf("comfort fare = %d, want 35121", comfort.EstimatedFarePaisa)
	}
}

func TestCalculateQuoteUsesNearestEligibleAvailableDriver(t *testing.T) {
	pickup := Location{ID: "pickup", City: "Lahore", Latitude: 0, Longitude: 0}
	destination := Location{ID: "destination", City: "Lahore", Latitude: 0, Longitude: 0.01}
	drivers := []AvailableDriver{
		{City: "Lahore", Category: "economy", Latitude: 0, Longitude: 0.02, Available: true},
		{City: "Lahore", Category: "economy", Latitude: 0, Longitude: 0.002, Available: true},
		{City: "Lahore", Category: "comfort", Latitude: 0, Longitude: 0.001, Available: true},
		{City: "Karachi", Category: "economy", Latitude: 0, Longitude: 0.001, Available: true},
		{City: "Lahore", Category: "economy", Latitude: 0, Longitude: 0.001, Available: false},
	}

	quote, err := CalculateQuote(pickup, destination, "economy", drivers, DefaultMatchRadiusKM)
	if err != nil {
		t.Fatalf("calculate quote: %v", err)
	}
	if quote.PickupETAMinutes == nil || *quote.PickupETAMinutes != 2 {
		t.Fatalf("pickup ETA = %v, want the two-minute minimum for the nearest eligible driver", quote.PickupETAMinutes)
	}
}

func TestCalculateQuoteReturnsNullETAWithoutNearbyMatchingDriver(t *testing.T) {
	pickup := Location{ID: "pickup", City: "Lahore", Latitude: 31.5, Longitude: 74.3}
	destination := Location{ID: "destination", City: "Lahore", Latitude: 31.6, Longitude: 74.4}
	drivers := []AvailableDriver{
		{City: "Lahore", Category: "economy", Latitude: 31.9, Longitude: 74.9, Available: true},
		{City: "Lahore", Category: "comfort", Latitude: 31.5, Longitude: 74.3, Available: true},
		{City: "Karachi", Category: "economy", Latitude: 31.5, Longitude: 74.3, Available: true},
	}

	quote, err := CalculateQuote(pickup, destination, "economy", drivers, 1)
	if err != nil {
		t.Fatalf("calculate quote: %v", err)
	}
	if quote.PickupETAMinutes != nil {
		t.Fatalf("pickup ETA = %d, want null when no eligible driver is within radius", *quote.PickupETAMinutes)
	}
}

func TestCalculateFareRoundsVariableFareUpToWholePaisa(t *testing.T) {
	for _, test := range []struct {
		category string
		want     int64
	}{
		{category: "economy", want: 15_007},
		{category: "comfort", want: 25_008},
	} {
		got, err := CalculateFare(test.category, 1)
		if err != nil {
			t.Fatalf("calculate %s fare: %v", test.category, err)
		}
		if got != test.want {
			t.Errorf("%s fare = %d, want %d", test.category, got, test.want)
		}
	}
}

func TestFuelPriceChangesFareAndPreservesBreakdown(t *testing.T) {
	pickup := Location{ID: "pickup", City: "Lahore", Latitude: 31.47, Longitude: 74.40}
	destination := Location{ID: "destination", City: "Lahore", Latitude: 31.51, Longitude: 74.34}
	rateCard, err := DefaultRateCard("economy")
	if err != nil {
		t.Fatalf("default rate card: %v", err)
	}
	baseQuote, err := CalculateQuoteWithRateCard(pickup, destination, "economy", nil, DefaultMatchRadiusKM, rateCard)
	if err != nil {
		t.Fatalf("quote at baseline fuel price: %v", err)
	}
	rateCard.FuelPricePaisaPerLiter += 10_000
	rateCard.Version++
	higherFuelQuote, err := CalculateQuoteWithRateCard(pickup, destination, "economy", nil, DefaultMatchRadiusKM, rateCard)
	if err != nil {
		t.Fatalf("quote at higher fuel price: %v", err)
	}
	if higherFuelQuote.EstimatedFarePaisa <= baseQuote.EstimatedFarePaisa {
		t.Fatalf("higher fuel price did not increase fare: baseline=%d higher=%d", baseQuote.EstimatedFarePaisa, higherFuelQuote.EstimatedFarePaisa)
	}
	if higherFuelQuote.EstimatedFarePaisa != higherFuelQuote.FuelCostPaisa+higherFuelQuote.BaseFarePaisa+higherFuelQuote.DistanceCostPaisa {
		t.Fatalf("fare breakdown does not sum to total: %+v", higherFuelQuote)
	}
}

func TestLahoreServiceAreaIncludesCityEdgesAndRejectsOutsidePoints(t *testing.T) {
	if !IsInLahoreServiceArea(31.2, 73.95) || !IsInLahoreServiceArea(31.75, 74.7) {
		t.Fatal("Lahore service bounds excluded an edge coordinate")
	}
	if IsInLahoreServiceArea(31.9, 74.3) || IsInLahoreServiceArea(31.5, 75.1) {
		t.Fatal("Lahore service bounds accepted a point outside the city area")
	}
}

func TestCalculateQuoteRejectsInvalidInputs(t *testing.T) {
	pickup := Location{ID: "same", City: "Lahore", Latitude: 31.5, Longitude: 74.3}
	destination := Location{ID: "destination", City: "Lahore", Latitude: 31.6, Longitude: 74.4}

	if _, err := CalculateQuote(pickup, destination, "premium", nil, DefaultMatchRadiusKM); !errors.Is(err, ErrUnsupportedCategory) {
		t.Fatalf("unsupported category error = %v, want ErrUnsupportedCategory", err)
	}
	if _, err := CalculateQuote(pickup, pickup, "economy", nil, DefaultMatchRadiusKM); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("identical locations error = %v, want ErrInvalidEstimate", err)
	}
	invalidCoordinates := destination
	invalidCoordinates.Latitude = 91
	if _, err := CalculateQuote(pickup, invalidCoordinates, "economy", nil, DefaultMatchRadiusKM); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("invalid coordinate error = %v, want ErrInvalidEstimate", err)
	}
	if _, err := CalculateQuote(pickup, destination, "economy", nil, 0); !errors.Is(err, ErrInvalidEstimate) {
		t.Fatalf("invalid radius error = %v, want ErrInvalidEstimate", err)
	}
}
