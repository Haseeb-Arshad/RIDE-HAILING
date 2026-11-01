package drivers

import "errors"

type Availability string

const (
	Offline   Availability = "offline"
	Available Availability = "available"
	Busy      Availability = "busy"
)

var ErrInvalidAvailability = errors.New("availability must be offline or available")

func ParseAvailability(value string) (Availability, error) {
	status := Availability(value)
	if status != Offline && status != Available {
		return "", ErrInvalidAvailability
	}
	return status, nil
}
