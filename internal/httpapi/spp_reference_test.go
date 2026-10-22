package httpapi

import (
	"encoding/json"
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

func TestMalformedSPPReferenceHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("malformed references must not call SAOS") }))
	defer upstream.Close()
	for _, action := range []string{"lookup", "IN", "OUT"} {
		t.Run(action, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, RequestTimeout: time.Second})
			service := movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client)
			secret := []byte("test-secret")
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"permissions": []string{appPermission}, "sub": "actor", "name": "Satpam", "exp": time.Now().Add(time.Minute).Unix()}).SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(Dependencies{Movement: service, Documents: service, JWTSecret: secret, PermissionClaim: "permissions", ActorIDClaim: "sub", ActorNameClaim: "name"})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/movement-documents/spp?number=123-", nil)
			wantStatus, wantCode := 400, "INVALID_DOCUMENT_QUERY"
			if action != "lookup" {
				body := `{"noLambung":"SAMT072","direction":"IN","inCategory":"WAITING_FOR_ASSIGNMENT","sppNumber":"123-"}`
				if action == "OUT" {
					body = `{"noLambung":"SAMT072","direction":"OUT","outDestination":"FILLING_SHED_KUIN","sppNumber":"123-"}`
				}
				request = httptest.NewRequest(http.MethodPost, "/api/v1/movements", strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", "96942009-f8c4-4dbf-b7e0-6d571765166a")
				mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
				wantStatus, wantCode = 422, "VALIDATION_ERROR"
			}
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != wantStatus || body.Error.Code != wantCode {
				t.Fatalf("invalid reference status/code = %d/%s, want %d/%s", response.Code, body.Error.Code, wantStatus, wantCode)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
