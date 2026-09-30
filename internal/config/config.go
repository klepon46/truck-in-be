package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	defaultRequestTimeout = 10 * time.Second
	maxConsulConfigSize   = 1 << 20
)

type Config struct {
	ServerPort                 int    `json:"server_port" mapstructure:"server_port"`
	PostgresDSN                string `json:"postgres_dsn" mapstructure:"postgres_dsn"`
	AuthJWTHS256Secret         string `json:"auth_jwt_hs256_secret" mapstructure:"auth_jwt_hs256_secret"`
	AuthPermissionsClaim       string `json:"auth_permissions_claim" mapstructure:"auth_permissions_claim"`
	AuthActorIDClaim           string `json:"auth_actor_id_claim" mapstructure:"auth_actor_id_claim"`
	AuthActorNameClaim         string `json:"auth_actor_name_claim" mapstructure:"auth_actor_name_claim"`
	UnitSyncInterval           string `json:"unit_sync_interval" mapstructure:"unit_sync_interval"`
	UnitSyncPageSize           int    `json:"unit_sync_page_size" mapstructure:"unit_sync_page_size"`
	LambungAPIBaseURL          string `json:"lambung_api_base_url" mapstructure:"lambung_api_base_url"`
	LambungServiceToken        string `json:"lambung_service_token" mapstructure:"lambung_service_token"`
	SAOSAPIBaseURL             string `json:"saos_api_base_url" mapstructure:"saos_api_base_url"`
	SAOSServiceToken           string `json:"saos_service_token" mapstructure:"saos_service_token"`
	WorkshopAPIBaseURL         string `json:"workshop_api_base_url" mapstructure:"workshop_api_base_url"`
	WorkshopServiceToken       string `json:"workshop_service_token" mapstructure:"workshop_service_token"`
	DriverAPIBaseURL           string `json:"driver_api_base_url" mapstructure:"driver_api_base_url"`
	DriverServiceToken         string `json:"driver_service_token" mapstructure:"driver_service_token"`
	HTTPClientConnectTimeoutMS int    `json:"http_client_connect_timeout_ms" mapstructure:"http_client_connect_timeout_ms"`
	HTTPClientRequestTimeoutMS int    `json:"http_client_request_timeout_ms" mapstructure:"http_client_request_timeout_ms"`
}

func Load(ctx context.Context) (Config, error) {
	address := strings.TrimRight(os.Getenv("CONSUL_HTTP_ADDR"), "/")
	key := strings.TrimPrefix(os.Getenv("CONSUL_CONFIG_KEY"), "/")
	if address == "" || key == "" {
		return Config{}, errors.New("Consul address and config key are required")
	}
	allowInsecureHTTP, err := parseInsecureHTTPOption(os.Getenv("CONSUL_ALLOW_INSECURE_HTTP"))
	if err != nil {
		return Config{}, err
	}
	consulURL, err := parseConsulURL(address, allowInsecureHTTP)
	if err != nil {
		return Config{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, consulConfigURL(consulURL, key).String(), nil)
	if err != nil {
		return Config{}, errors.New("create Consul request")
	}
	if token := os.Getenv("CONSUL_HTTP_TOKEN"); token != "" {
		request.Header.Set("X-Consul-Token", token)
	}

	client := newConsulClient()
	response, err := client.Do(request)
	if err != nil {
		return Config{}, errors.New("read configuration from Consul")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Config{}, fmt.Errorf("read configuration from Consul: status %d", response.StatusCode)
	}

	body, err := readConsulConfigBody(response.Body)
	if err != nil {
		return Config{}, errors.New("read Consul configuration body")
	}

	config, err := decodeConfig(bytes.NewReader(body))
	if err != nil {
		return Config{}, errors.New("parse Consul configuration")
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func decodeConfig(source io.Reader) (Config, error) {
	settings := viper.New()
	settings.SetConfigType("json")
	if err := settings.ReadConfig(source); err != nil {
		return Config{}, err
	}

	var config Config
	if err := settings.Unmarshal(&config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func readConsulConfigBody(source io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(source, maxConsulConfigSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxConsulConfigSize {
		return nil, errors.New("Consul configuration body exceeds 1 MiB")
	}
	return body, nil
}

func newConsulClient() *http.Client {
	return &http.Client{
		Timeout: defaultRequestTimeout,
		// Do not forward the Consul ACL token to a redirect target.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func parseInsecureHTTPOption(value string) (bool, error) {
	switch value {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("CONSUL_ALLOW_INSECURE_HTTP must be true or false")
	}
}

func parseConsulURL(address string, allowInsecureHTTP bool) (*url.URL, error) {
	consulURL, err := url.ParseRequestURI(address)
	if err != nil || consulURL.Host == "" {
		return nil, errors.New("Consul address must be an HTTPS URL")
	}
	if consulURL.Scheme != "https" && !(allowInsecureHTTP && consulURL.Scheme == "http") {
		return nil, errors.New("Consul address must be an HTTPS URL unless CONSUL_ALLOW_INSECURE_HTTP=true")
	}
	return consulURL, nil
}

func consulConfigURL(baseURL *url.URL, key string) *url.URL {
	requestURL := *baseURL
	requestURL.Path = strings.TrimRight(baseURL.Path, "/") + "/v1/kv/" + key
	requestURL.RawPath = ""
	requestURL.RawQuery = "raw"
	requestURL.Fragment = ""
	return &requestURL
}

func (c Config) Validate() error {
	if c.ServerPort < 1 || c.ServerPort > 65535 {
		return errors.New("server_port must be between 1 and 65535")
	}
	if c.PostgresDSN == "" {
		return errors.New("postgres_dsn is required")
	}
	if c.AuthJWTHS256Secret == "" {
		return errors.New("auth_jwt_hs256_secret is required")
	}
	if c.AuthPermissionsClaim == "" {
		return errors.New("auth_permissions_claim is required")
	}
	if c.AuthActorIDClaim == "" || c.AuthActorNameClaim == "" {
		return errors.New("auth actor claims are required")
	}
	return nil
}

func (c Config) ValidateRuntime() error {
	if err := c.Validate(); err != nil {
		return err
	}
	interval, err := time.ParseDuration(c.UnitSyncInterval)
	if err != nil || interval <= 0 {
		return errors.New("unit_sync_interval must be a duration")
	}
	if c.UnitSyncPageSize < 1 || c.HTTPClientConnectTimeoutMS < 1 || c.HTTPClientRequestTimeoutMS < 1 {
		return errors.New("sync page size and HTTP timeouts must be positive")
	}
	for _, value := range []string{
		c.LambungAPIBaseURL, c.LambungServiceToken, c.SAOSAPIBaseURL, c.SAOSServiceToken,
		c.WorkshopAPIBaseURL, c.WorkshopServiceToken, c.DriverAPIBaseURL, c.DriverServiceToken,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.New("integration configuration is required")
		}
	}
	return nil
}
