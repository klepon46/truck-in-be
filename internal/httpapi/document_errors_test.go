package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"

	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
)

func TestDocumentErrorsReachAndroidWithoutMovementWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, kind, body, code, message, reason string
		upstreamStatus, status                  int
	}{
		{"WO absent", "WO", `{"success":false,"code":404,"message":"Work order not found"}`, "WORK_ORDER_NOT_FOUND", "WO tidak ditemukan di Workshop.", "wo_not_found", 404, 404},
		{"SPP empty", "SPP", `{"success":true,"data":[]}`, "SPP_NOT_FOUND", "SPP tidak ditemukan di SAOS.", "spp_not_found", 200, 404},
		{"SPP null producer slice", "SPP", `{"success":true,"data":null}`, "SPP_NOT_FOUND", "SPP tidak ditemukan di SAOS.", "spp_not_found", 200, 404},
		{"SPP missing canonical number", "SPP", `{"success":true,"data":[{"trans_no":""}]}`, "DOCUMENT_VALIDATION_FAILED", "", "document_invalid", 200, 422},
		{"WO bad record", "WO", `{"success":true,"data":{"work_order_number":"DOC/001"}}`, "DOCUMENT_VALIDATION_FAILED", "", "document_invalid", 200, 422},
		{"SPP bad record", "SPP", `{"success":true,"data":[{"trans_no":"DOC/001"}]}`, "DOCUMENT_VALIDATION_FAILED", "", "document_invalid", 200, 422},
		{"WO unauthorized", "WO", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 401, 500},
		{"SPP unauthorized", "SPP", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 401, 500},
		{"WO service failure", "WO", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 503, 500},
		{"SPP service failure", "SPP", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 503, 500},
		{"WO route not found", "WO", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 404, 500},
		{"SPP route not found", "SPP", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 404, 500},
		{"WO malformed JSON", "WO", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 200, 500},
		{"SPP malformed JSON", "SPP", "private body", "INTERNAL_ERROR", "", "integration_unavailable", 200, 500},
	} {
		for _, action := range []string{"lookup", "IN", "OUT"} {
			t.Run(test.name+"/"+action, func(t *testing.T) {
				var logs bytes.Buffer
				original := slog.Default()
				slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
				defer slog.SetDefault(original)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(test.upstreamStatus)
					_, _ = w.Write([]byte(test.body))
				}))
				defer upstream.Close()
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				client := integration.NewClient(integration.Config{WorkshopBaseURL: upstream.URL, WorkshopToken: "private-token", SAOSBaseURL: upstream.URL, SAOSToken: "private-token", RequestTimeout: time.Second})
				service := movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client)
				secret := []byte("test-secret")
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
					"exp": time.Now().Add(time.Minute).Unix(), "permissions": []string{appPermission}, "sub": "actor", "name": "Satpam",
				}).SignedString(secret)
				if err != nil {
					t.Fatal(err)
				}
				router := NewRouter(Dependencies{Movement: service, Documents: service, JWTSecret: secret, PermissionClaim: "permissions", ActorIDClaim: "sub", ActorNameClaim: "name"})
				path := "/api/v1/movement-documents/work-orders?number=DOC%2F001"
				if test.kind == "SPP" {
					path = "/api/v1/movement-documents/spp?number=123-example"
				}
				request := httptest.NewRequest(http.MethodGet, path, nil)
				if action != "lookup" {
					body := map[string]any{"noLambung": "SAMT072", "direction": action}
					if test.kind == "WO" {
						body["workOrderNumber"], body["outDestination"] = "DOC/001", movement.OutWorkshop
					} else {
						body["sppNumber"], body["outDestination"] = "123-example", movement.OutFillingShed
					}
					if action == "IN" {
						delete(body, "outDestination")
						body["inCategory"] = movement.InWaitingForAssignment
					}
					encoded, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					request = httptest.NewRequest(http.MethodPost, "/api/v1/movements", bytes.NewReader(encoded))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Idempotency-Key", "96942009-f8c4-4dbf-b7e0-6d571765166a")
					mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
				}
				request.Header.Set("Authorization", "Bearer "+token)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				var body struct {
					Error struct{ Code, Message string } `json:"error"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if response.Code != test.status || body.Error.Code != test.code || (test.message != "" && body.Error.Message != test.message) {
					t.Fatalf("status = %d, error = %+v; want %d / %s", response.Code, body.Error, test.status, test.code)
				}
				if action == "lookup" && !strings.Contains(logs.String(), `"reason":"`+test.reason+`"`) {
					t.Fatalf("missing safe reason %s: %s", test.reason, logs.String())
				}
				for _, sensitive := range []string{"private-token", "private body", "DOC/001", "123-example", upstream.URL, token} {
					if strings.Contains(logs.String(), sensitive) || strings.Contains(response.Body.String(), sensitive) {
						t.Error("client error or logs leaked request/upstream data")
					}
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
