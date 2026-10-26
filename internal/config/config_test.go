package config

import "testing"

func TestSecureCookiesConfiguration(t *testing.T) {
	for _, value := range []string{"", "false", "true", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("POCKETDRIVE_SECURE_COOKIES", value)
			cfg, err := Load()
			if value == "invalid" {
				if err == nil {
					t.Fatal("invalid security setting was accepted")
				}
				return
			}
			if err != nil || cfg.SecureCookies != (value == "true") {
				t.Fatalf("Load() = %+v, %v", cfg, err)
			}
		})
	}
}
