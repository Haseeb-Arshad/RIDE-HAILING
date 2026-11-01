package drivers

import (
	"errors"
	"testing"
)

func TestParseAvailability(t *testing.T) {
	for _, test := range []struct {
		value string
		want  Availability
	}{
		{value: "offline", want: Offline},
		{value: "available", want: Available},
	} {
		got, err := ParseAvailability(test.value)
		if err != nil || got != test.want {
			t.Errorf("ParseAvailability(%q) = %q, %v; want %q", test.value, got, err, test.want)
		}
	}
	for _, value := range []string{"", "busy", "Available", "online"} {
		if _, err := ParseAvailability(value); !errors.Is(err, ErrInvalidAvailability) {
			t.Errorf("ParseAvailability(%q) error = %v, want ErrInvalidAvailability", value, err)
		}
	}
}
