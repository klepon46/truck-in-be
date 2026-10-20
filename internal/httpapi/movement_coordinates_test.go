package httpapi

import (
	"bytes"
	"crypto/sha256"
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

	"truckin-be/internal/movement"
)

func TestMovementCoordinatesHTTPBindingAndReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		value      any
		omit       bool
		want       *string
		wantStatus int
	}{
		{name: "legacy absent", omit: true, wantStatus: 201},
		{name: "null", value: nil, wantStatus: 201},
		{name: "empty", value: "", wantStatus: 201},
		{name: "blank", value: "  ", wantStatus: 201},
		{name: "normalized pair", value: " -3.319437 , 114.590889 ", want: coordinatePointer("-3.319437,114.590889"), wantStatus: 201},
		{name: "zero is not missing", value: "0,0", want: coordinatePointer("0,0"), wantStatus: 201},
		{name: "changed coordinate conflicts", value: "-3.319438,114.590889", want: coordinatePointer("-3.319437,114.590889"), wantStatus: 409},
		{name: "out of range", value: "91,114.5", wantStatus: 422},
		{name: "nonfinite", value: "NaN,0", wantStatus: 422},
		{name: "incomplete pair", value: "-3.3,", wantStatus: 422},
		{name: "numeric JSON", value: 3.3, wantStatus: 400},
		{name: "array JSON", value: []string{"-3.3", "114.5"}, wantStatus: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			originalLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(originalLogger)
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if test.wantStatus == 201 || test.wantStatus == 409 {
				input := movement.Input{NoLambung: "SAMT119", Direction: movement.DirectionOut, OutDestination: movement.OutWorkshop, WorkOrderNumber: "WO-1"}
				var storedCoordinates any
				if test.want != nil {
					input.Coordinates = *test.want
					storedCoordinates = *test.want
				}
				encoded, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(encoded)
				mock.ExpectQuery("SELECT m.transaction_id").WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
					WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "request_hash", "coordinates"}).AddRow(19, hash[:], storedCoordinates))
			}
			secret := []byte("test-secret")
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"exp": time.Now().Add(time.Minute).Unix(), "permissions": []string{appPermission}, "actorId": "satpam-id", "actorName": "Satpam",
			}).SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"noLambung": "SAMT119", "direction": "OUT", "outDestination": "BENGKEL_LUAR", "workOrderNumber": "WO-1"}
			if !test.omit {
				body["coordinates"] = test.value
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/movements", bytes.NewReader(encoded))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Idempotency-Key", "96942009-f8c4-4dbf-b7e0-6d571765166a")
			response := httptest.NewRecorder()
			NewRouter(Dependencies{
				Movement:  movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, nil),
				JWTSecret: secret, PermissionClaim: "permissions", ActorIDClaim: "actorId", ActorNameClaim: "actorName",
			}).ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == 201 {
				var result struct {
					Data movement.Transaction `json:"data"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if (result.Data.Coordinates == nil) != (test.want == nil) || (test.want != nil && *result.Data.Coordinates != *test.want) {
					t.Fatal("replay did not return the immutable coordinate snapshot")
				}
				if test.want == nil && !strings.Contains(response.Body.String(), `"coordinates":null`) {
					t.Fatal("missing coordinates must be explicitly returned as null")
				}
			}
			if strings.Contains(logs.String(), "coordinates") || strings.Contains(logs.String(), "-3.319437") || strings.Contains(logs.String(), "91,114.5") {
				t.Fatal("movement logs must not expose location data")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func coordinatePointer(value string) *string { return &value }
