package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
	"truckin-be/internal/operations"
)

type testPinger struct {
	err error
}

func (p testPinger) PingContext(context.Context) error {
	return p.err
}

type testDriverSearcher struct {
	drivers []integration.ActiveDriver
	err     error
	query   string
	calls   int
}

type testDocumentLookup struct {
	workOrder movement.LookupDocument
	spp       movement.LookupDocument
	err       error
}

type testUnitPicker struct {
	units  []operations.Unit
	err    error
	filter operations.UnitFilter
}

func (p *testUnitPicker) ListUnits(_ context.Context, filter operations.UnitFilter) ([]operations.Unit, operations.Page, error) {
	p.filter = filter
	return p.units, filter.Page, p.err
}

func (l testDocumentLookup) LookupWorkshop(_ context.Context, number string) (movement.LookupDocument, error) {
	if number != "WO-001" {
		return movement.LookupDocument{}, errors.New("unexpected work order")
	}
	return l.workOrder, l.err
}

func (l testDocumentLookup) LookupSPP(_ context.Context, number string) (movement.LookupDocument, error) {
	if number != "SPP-001" {
		return movement.LookupDocument{}, errors.New("unexpected SPP")
	}
	return l.spp, l.err
}

func (s *testDriverSearcher) ActiveDrivers(_ context.Context, query string) ([]integration.ActiveDriver, error) {
	s.calls++
	s.query = query
	return s.drivers, s.err
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

func TestRouterAllowsConfiguredOriginPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/dashboard", nil)
	request.Header.Set("Origin", "http://172.16.17.17:3022")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "authorization")
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:           testPinger{},
		CORSAllowedOrigins: []string{"http://172.16.17.17:3022"},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if origin := response.Header().Get("Access-Control-Allow-Origin"); origin != "http://172.16.17.17:3022" {
		t.Fatalf("Access-Control-Allow-Origin = %q", origin)
	}
	if vary := response.Header().Get("Vary"); vary != "Origin" {
		t.Fatalf("Vary = %q", vary)
	}
	if methods := response.Header().Get("Access-Control-Allow-Methods"); methods != "GET, POST, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q", methods)
	}
}

func TestRouterRejectsUnconfiguredOriginPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodOptions, "/api/v1/dashboard", nil)
	request.Header.Set("Origin", "http://untrusted.example")
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:           testPinger{},
		CORSAllowedOrigins: []string{"http://172.16.17.17:3022"},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if origin := response.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", origin)
	}
}

func TestRouterListsActiveDriversForAppUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret")
	searcher := &testDriverSearcher{drivers: []integration.ActiveDriver{{ID: 42, DriverName: "Driver Name"}}}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/drivers?name=Driver%20Name", nil)
	request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:        testPinger{},
		Drivers:         searcher,
		JWTSecret:       secret,
		PermissionClaim: "permissions",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if searcher.query != "Driver Name" {
		t.Fatalf("driver query = %q", searcher.query)
	}
	var body struct {
		Data []integration.ActiveDriver `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0] != (integration.ActiveDriver{ID: 42, DriverName: "Driver Name"}) {
		t.Fatalf("response data = %#v", body.Data)
	}
}

func TestRouterLooksUpWorkshopDocumentForAppUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/movement-documents/work-orders?number=WO-001", nil)
	request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:        testPinger{},
		Documents:       testDocumentLookup{workOrder: movement.LookupDocument{NoLambung: "A122", NoPolisi: "DA 122 A", DriverName: "Driver", WorkOrderNumber: "WO-001"}},
		JWTSecret:       secret,
		PermissionClaim: "permissions",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Data struct {
			NoLambung       string `json:"noLambung"`
			NoPolisi        string `json:"noPolisi"`
			DriverName      string `json:"driverName"`
			WorkOrderNumber string `json:"workOrderNumber"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Data.NoLambung != "A122" || body.Data.NoPolisi != "DA 122 A" || body.Data.DriverName != "Driver" || body.Data.WorkOrderNumber != "WO-001" {
		t.Fatalf("response data = %#v", body.Data)
	}
}

func TestRouterLooksUpSPPDocumentWithCustomerForAppUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/movement-documents/spp?number=SPP-001", nil)
	request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:        testPinger{},
		Documents:       testDocumentLookup{spp: movement.LookupDocument{NoLambung: "A122", NoPolisi: "DA 122 A", DriverName: "Driver", SPPNumber: "SPP-001", CustomerID: "CUST-1", CustomerName: "Customer One"}},
		JWTSecret:       secret,
		PermissionClaim: "permissions",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Data struct {
			SPPNumber    string `json:"sppNumber"`
			CustomerID   string `json:"customerId"`
			CustomerName string `json:"customerName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Data.SPPNumber != "SPP-001" || body.Data.CustomerID != "CUST-1" || body.Data.CustomerName != "Customer One" {
		t.Fatalf("response data = %#v", body.Data)
	}
}

func TestRouterListsUnitsForAppPicker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/unit-picker?query=A122&includeInactive=true", nil)
	request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
	response := httptest.NewRecorder()

	driverName := "Driver Name"
	picker := &testUnitPicker{units: []operations.Unit{{ID: 7, NoLambung: "A122", NoPolisi: "DA 122 A", IsActive: true, DriverName: &driverName}}}
	NewRouter(Dependencies{
		Database:        testPinger{},
		UnitPicker:      picker,
		JWTSecret:       secret,
		PermissionClaim: "permissions",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var body struct {
		Data []struct {
			NoLambung string `json:"noLambung"`
			NoPolisi  string `json:"noPolisi"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].NoLambung != "A122" || body.Data[0].NoPolisi != "DA 122 A" {
		t.Fatalf("response data = %#v", body.Data)
	}
	if picker.filter.IncludeInactive {
		t.Fatal("unit picker allowed includeInactive=true")
	}
	if bytes.Contains(response.Body.Bytes(), []byte(`"driverName"`)) {
		t.Fatalf("unit picker leaked operations data: %s", response.Body.String())
	}
}

func TestRouterRejectsDriverSearchForWebUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/drivers?name=Driver", nil)
	request.Header.Set("Authorization", "Bearer "+testToken(t, secret, webPermission))
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:        testPinger{},
		Drivers:         &testDriverSearcher{},
		JWTSecret:       secret,
		PermissionClaim: "permissions",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestRouterRequiresDriverSearchName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("test-secret")
	searcher := &testDriverSearcher{}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/drivers", nil)
	request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
	response := httptest.NewRecorder()

	NewRouter(Dependencies{
		Database:        testPinger{},
		Drivers:         searcher,
		JWTSecret:       secret,
		PermissionClaim: "permissions",
	}).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if searcher.calls != 0 {
		t.Fatalf("driver search calls = %d, want 0", searcher.calls)
	}
}

func testToken(t *testing.T, secret []byte, permission string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp":         time.Now().Add(time.Minute).Unix(),
		"permissions": []string{permission},
	})
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
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
	if !bytes.Contains(response.Body.Bytes(), []byte(`"/api/v1/drivers"`)) {
		t.Fatal("Swagger definition does not contain the driver search endpoint")
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"/api/v1/movement-documents/work-orders"`)) {
		t.Fatal("Swagger definition does not contain the work order lookup endpoint")
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"/api/v1/movement-documents/spp"`)) {
		t.Fatal("Swagger definition does not contain the SPP lookup endpoint")
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"/api/v1/unit-picker"`)) {
		t.Fatal("Swagger definition does not contain the unit picker endpoint")
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
	if !bytes.Contains(response.Body.Bytes(), []byte(`"cycleId"`)) {
		t.Fatal("Swagger movement response does not document cycleId")
	}
	transactionDefinition, ok := definition["definitions"].(map[string]any)["operations.Transaction"].(map[string]any)
	if !ok {
		t.Fatal("Swagger does not define operations.Transaction")
	}
	transactionProperties := transactionDefinition["properties"].(map[string]any)
	if _, ok := transactionProperties["durationSeconds"]; !ok {
		t.Fatal("Swagger transaction response does not document durationSeconds")
	}
	if _, ok := transactionProperties["statusCompletedAt"]; !ok {
		t.Fatal("Swagger transaction response does not document statusCompletedAt")
	}
	for _, name := range []string{"movement.Transaction", "operations.Transaction", "httpapi.movementRequest"} {
		properties := definition["definitions"].(map[string]any)[name].(map[string]any)["properties"].(map[string]any)
		coordinates, ok := properties["coordinates"].(map[string]any)
		if !ok || coordinates["type"] != "string" {
			t.Fatalf("Swagger %s must document coordinates as a string", name)
		}
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

func TestLogMovementFailureUsesSafeContext(t *testing.T) {
	var output bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(originalLogger)

	logMovementFailure(movement.Input{
		NoLambung:      "SAMT119",
		Direction:      movement.DirectionOut,
		OutDestination: movement.OutWorkshop,
	}, errors.New("ensure Unit Lambung unit: call integration"))

	entry := output.String()
	for _, expected := range []string{"record movement failed", "SAMT119", "OUT", "BENGKEL_LUAR", "call integration"} {
		if !strings.Contains(entry, expected) {
			t.Fatalf("log entry %q does not contain %q", entry, expected)
		}
	}
	if strings.Contains(strings.ToLower(entry), "idempotency") {
		t.Fatalf("log entry contains idempotency material: %q", entry)
	}
}
