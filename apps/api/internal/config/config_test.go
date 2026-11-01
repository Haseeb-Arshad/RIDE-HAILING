package config

import "testing"

func TestMatchRadiusDefaultsAndValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "default", value: "", want: 20},
		{name: "configured", value: "35", want: 35},
		{name: "below minimum", value: "0", wantErr: true},
		{name: "above maximum", value: "201", wantErr: true},
		{name: "not an integer", value: "many", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PORT", "8080")
			t.Setenv("DATABASE_URL", "")
			t.Setenv("APP_ORIGIN", "http://127.0.0.1:3000")
			t.Setenv("SESSION_COOKIE_SECURE", "false")
			t.Setenv("DEMO_SEED_ENABLED", "false")
			t.Setenv("DEMO_ACCOUNT_PASSWORD", "")
			t.Setenv("REQUEST_TIMEOUT", "10s")
			t.Setenv("MATCH_RADIUS_KM", test.value)

			config, err := Load()
			if test.wantErr {
				if err == nil {
					t.Fatal("Load succeeded; want an invalid match-radius error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if config.MatchRadiusKM != test.want {
				t.Fatalf("match radius = %d, want %d", config.MatchRadiusKM, test.want)
			}
		})
	}
}
