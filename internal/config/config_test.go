package config

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDecodeConfigReadsConsulJSON(t *testing.T) {
	config, err := decodeConfig(bytes.NewBufferString(`{
		"server_port": 8104,
		"postgres_dsn": "postgres://example",
		"auth_jwt_hs256_secret": "test-secret",
		"auth_permissions_claim": "permissions",
		"auth_actor_id_claim": "sub",
		"auth_actor_name_claim": "name",
		"unit_sync_interval": "1m",
		"unit_sync_page_size": 100,
		"lambung_api_base_url": "https://lambung.example.internal",
		"lambung_service_token": "lambung-token",
		"saos_api_base_url": "https://saos.example.internal",
		"saos_service_token": "saos-token",
		"workshop_api_base_url": "https://workshop.example.internal",
		"workshop_service_token": "workshop-token",
		"driver_api_base_url": "https://driver.example.internal",
		"driver_service_token": "driver-token",
		"http_client_connect_timeout_ms": 1000,
		"http_client_request_timeout_ms": 5000
	}`))
	if err != nil {
		t.Fatalf("decodeConfig() error = %v", err)
	}

	if config.ServerPort != 8104 || config.PostgresDSN != "postgres://example" {
		t.Fatalf("decodeConfig() = %#v", config)
	}
	if config.HTTPClientConnectTimeoutMS != 1000 || config.HTTPClientRequestTimeoutMS != 5000 {
		t.Fatalf("decodeConfig() timeouts = %d, %d", config.HTTPClientConnectTimeoutMS, config.HTTPClientRequestTimeoutMS)
	}
}

func TestDecodeConfigRejectsInvalidJSON(t *testing.T) {
	if _, err := decodeConfig(bytes.NewBufferString(`{"server_port":`)); err == nil {
		t.Fatal("decodeConfig() accepted invalid JSON")
	}
}

func TestConfigValidateRequiresJWTSecret(t *testing.T) {
	config := Config{
		ServerPort:           8104,
		PostgresDSN:          "postgres://example",
		AuthPermissionsClaim: "permissions",
	}

	if err := config.Validate(); err == nil {
		t.Fatal("Validate() accepted missing JWT secret")
	}
}

func TestConfigValidateAcceptsRequiredValues(t *testing.T) {
	config := Config{
		ServerPort:           8104,
		PostgresDSN:          "postgres://example",
		AuthJWTHS256Secret:   "test-secret",
		AuthPermissionsClaim: "permissions",
		AuthActorIDClaim:     "sub",
		AuthActorNameClaim:   "name",
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidateRequiresActorClaims(t *testing.T) {
	config := Config{
		ServerPort:           8104,
		PostgresDSN:          "postgres://example",
		AuthJWTHS256Secret:   "test-secret",
		AuthPermissionsClaim: "permissions",
	}

	if err := config.Validate(); err == nil {
		t.Fatal("Validate() accepted missing actor claims")
	}
}

func TestConsulAddressRequiresHTTPSByDefault(t *testing.T) {
	if _, err := parseConsulURL("http://consul.internal", false); err == nil {
		t.Fatal("parseConsulURL() accepted an HTTP address without explicit opt-in")
	}
	if _, err := parseConsulURL("https://consul.internal", false); err != nil {
		t.Fatalf("parseConsulURL() error = %v", err)
	}
	if _, err := parseConsulURL("http://consul.internal", true); err != nil {
		t.Fatalf("parseConsulURL() error = %v", err)
	}
	if _, err := parseConsulURL("ftp://consul.internal", true); err == nil {
		t.Fatal("parseConsulURL() accepted an unsupported scheme")
	}
}

func TestParseInsecureHTTPOption(t *testing.T) {
	allowed, err := parseInsecureHTTPOption("true")
	if err != nil || !allowed {
		t.Fatalf("parseInsecureHTTPOption(true) = %t, %v", allowed, err)
	}
	allowed, err = parseInsecureHTTPOption("false")
	if err != nil || allowed {
		t.Fatalf("parseInsecureHTTPOption(false) = %t, %v", allowed, err)
	}
	if _, err := parseInsecureHTTPOption("enabled"); err == nil {
		t.Fatal("parseInsecureHTTPOption() accepted an invalid value")
	}
	for _, value := range []string{"1", "t", "TRUE"} {
		if _, err := parseInsecureHTTPOption(value); err == nil {
			t.Fatalf("parseInsecureHTTPOption(%q) accepted a non-exact opt-in", value)
		}
	}
}

func TestLoadRejectsHTTPConsulWithoutExplicitOptIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Errorf("Load() sent a request to HTTP Consul without explicit opt-in")
	}))
	defer server.Close()

	t.Setenv("CONSUL_HTTP_ADDR", server.URL)
	t.Setenv("CONSUL_CONFIG_KEY", "dev/be/truckin-be-config")
	t.Setenv("CONSUL_ALLOW_INSECURE_HTTP", "")

	if _, err := Load(context.Background()); err == nil {
		t.Fatal("Load() accepted HTTP Consul without explicit opt-in")
	}
}

func TestLoadAllowsHTTPConsulWithExactOptIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/kv/dev/be/truckin-be-config" || request.URL.RawQuery != "raw" {
			t.Errorf("request URL = %s", request.URL)
			return
		}
		_, _ = writer.Write([]byte(`{
			"server_port": 8104,
			"postgres_dsn": "postgres://example",
			"auth_jwt_hs256_secret": "test-secret",
			"auth_permissions_claim": "permissions",
			"auth_actor_id_claim": "sub",
			"auth_actor_name_claim": "name"
		}`))
	}))
	defer server.Close()

	t.Setenv("CONSUL_HTTP_ADDR", server.URL)
	t.Setenv("CONSUL_CONFIG_KEY", "dev/be/truckin-be-config")
	t.Setenv("CONSUL_ALLOW_INSECURE_HTTP", "true")

	config, err := Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.ServerPort != 8104 {
		t.Fatalf("Load() server port = %d", config.ServerPort)
	}
}

func TestConsulClientDoesNotFollowRedirects(t *testing.T) {
	client := newConsulClient()
	err := client.CheckRedirect(&http.Request{}, nil)
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect() error = %v, want %v", err, http.ErrUseLastResponse)
	}
}

func TestConsulConfigURLKeepsReservedKeyCharactersInThePath(t *testing.T) {
	baseURL, err := parseConsulURL("https://consul.internal", false)
	if err != nil {
		t.Fatalf("parseConsulURL() error = %v", err)
	}

	requestURL := consulConfigURL(baseURL, "dev/be/config?version=2#latest")
	parsedURL, err := url.Parse(requestURL.String())
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	if parsedURL.Path != "/v1/kv/dev/be/config?version=2#latest" {
		t.Fatalf("request path = %q", parsedURL.Path)
	}
	if parsedURL.RawQuery != "raw" || parsedURL.Fragment != "" {
		t.Fatalf("request URL query or fragment = %q, %q", parsedURL.RawQuery, parsedURL.Fragment)
	}
}

func TestReadConsulConfigBodyRejectsOversizedResponse(t *testing.T) {
	if _, err := readConsulConfigBody(bytes.NewReader(bytes.Repeat([]byte("a"), maxConsulConfigSize+1))); err == nil {
		t.Fatal("readConsulConfigBody() accepted an oversized response")
	}
}
