package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type testPinger struct {
	err error
}

func (p testPinger) PingContext(context.Context) error {
	return p.err
}

func TestRouterHealthReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	NewRouter(Dependencies{Database: testPinger{}}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestRouterHealthReturnsServiceUnavailableWhenDatabaseIsDown(t *testing.T) {
	gin.SetMode(gin.TestMode)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	NewRouter(Dependencies{Database: testPinger{err: errors.New("database down")}}).ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestCSVValuePreventsFormulaExecution(t *testing.T) {
	if got := csvValue("=HYPERLINK(\"https://example.test\")"); got != "'=HYPERLINK(\"https://example.test\")" {
		t.Fatalf("csvValue() = %q", got)
	}
}

func TestRouterServesSwaggerUI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	request := httptest.NewRequest(http.MethodGet, "/swagger-ui/index.html", nil)
	response := httptest.NewRecorder()

	NewRouter(Dependencies{Database: testPinger{}}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestRouterServesGeneratedSwaggerDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)

	request := httptest.NewRequest(http.MethodGet, "/swagger-ui/doc.json", nil)
	response := httptest.NewRecorder()

	NewRouter(Dependencies{Database: testPinger{}}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"/api/v1/movements"`)) {
		t.Fatal("Swagger definition does not contain the movement endpoint")
	}

	var definition map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &definition); err != nil {
		t.Fatalf("unmarshal Swagger definition: %v", err)
	}
	paths := definition["paths"].(map[string]any)
	movement := paths["/api/v1/movements"].(map[string]any)["post"].(map[string]any)
	responseSchema := movement["responses"].(map[string]any)["201"].(map[string]any)["schema"].(map[string]any)
	properties, ok := responseSchema["properties"].(map[string]any)
	if !ok {
		if allOf, hasAllOf := responseSchema["allOf"].([]any); hasAllOf {
			for _, component := range allOf {
				if componentProperties, hasProperties := component.(map[string]any)["properties"].(map[string]any); hasProperties {
					properties = componentProperties
					ok = true
					break
				}
			}
		}
	}
	if !ok {
		responseDefinition, ok := responseSchema["$ref"].(string)
		if !ok {
			t.Fatal("Swagger movement response has no object schema")
		}
		responseDefinition = strings.TrimPrefix(responseDefinition, "#/definitions/")
		properties = definition["definitions"].(map[string]any)[responseDefinition].(map[string]any)["properties"].(map[string]any)
	}
	if _, ok := properties["data"]; !ok {
		t.Fatal("Swagger movement response does not document the data envelope")
	}
}

func TestRouterRedirectsLegacySwaggerRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)

	request := httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil)
	response := httptest.NewRecorder()

	NewRouter(Dependencies{Database: testPinger{}}).ServeHTTP(response, request)

	if response.Code != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusPermanentRedirect)
	}
	if location := response.Header().Get("Location"); location != "/swagger-ui/index.html" {
		t.Fatalf("Location = %q, want %q", location, "/swagger-ui/index.html")
	}
}
