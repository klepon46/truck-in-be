package config

import (
	"strings"
	"testing"
)

func TestOtherDriverSubmitValidationConfigDefaultsSafely(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       bool
	}{
		{name: "absent remains enabled", body: `{}`, want: true},
		{name: "explicitly enabled", body: `{"validate_other_driver_on_submit":true}`, want: true},
		{name: "explicit testing bypass", body: `{"validate_other_driver_on_submit":false}`, want: false},
		{name: "null remains enabled", body: `{"validate_other_driver_on_submit":null}`, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := decodeConfig(strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if config.ValidateOtherDriverOnSubmit != test.want {
				t.Fatalf("driver submit validation = %t, want %t", config.ValidateOtherDriverOnSubmit, test.want)
			}
		})
	}
}

func TestOtherDriverSubmitValidationRejectsNonBooleans(t *testing.T) {
	for _, value := range []string{`"false"`, `""`, `0`, `1`, `[]`, `{}`} {
		if _, err := decodeConfig(strings.NewReader(`{"validate_other_driver_on_submit":` + value + `}`)); err == nil {
			t.Errorf("nonboolean value %s must not silently disable validation", value)
		}
	}
}

func TestOtherDriverBypassStillRequiresPickerIntegrationConfig(t *testing.T) {
	config := Config{
		ServerPort: 8105, PostgresDSN: "postgres://example", AuthJWTHS256Secret: "test-secret",
		AuthPermissionsClaim: "permissions", AuthActorIDClaim: "sub", AuthActorNameClaim: "name",
		UnitSyncInterval: "1m", UnitSyncPageSize: 100, HTTPClientConnectTimeoutMS: 1000, HTTPClientRequestTimeoutMS: 5000,
		LambungAPIBaseURL: "https://lambung.example", LambungServiceToken: "test-token",
		SAOSAPIBaseURL: "https://saos.example", SAOSServiceToken: "test-token",
		WorkshopAPIBaseURL: "https://workshop.example", WorkshopServiceToken: "test-token",
		ValidateOtherDriverOnSubmit: false,
	}
	if config.ValidateRuntime() == nil {
		t.Fatal("bypass must not remove Driver configuration required by the active-driver picker")
	}
	config.DriverAPIBaseURL, config.DriverServiceToken = "https://driver.example", "test-token"
	if err := config.ValidateRuntime(); err != nil {
		t.Fatal(err)
	}
}
