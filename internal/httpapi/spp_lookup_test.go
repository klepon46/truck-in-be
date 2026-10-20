package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
)

// Supplied SAOS fixture; extra fields are deliberately kept to test compatibility.
const sppLookupFixture = `{"success":true,"message":"Success","code":200,"data":[{
	"trans_no":"SPG-BJM-2612481","unit_police_no":"DA 8729 CB",
	"driver_name":"JOYO LUMBANRAJA ","unit_capacity":"10000","unit_name":"SAMT072",
	"customer_code":"C400","customer_name":"PT KAPUAS MAJU JAYA","qty":10000,"is_marked":false
}]}`

func TestSPPLookupIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name           string
		upstreamStatus int
		response       string
		querySnapshot  bool
		snapshotPolice string
		wantStatus     int
		wantCode       string
		wantMessage    string
		wantLog        string
	}{
		{name: "actual fixture without movement writes", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", wantStatus: 200},
		{name: "unit not found", response: sppLookupFixture, querySnapshot: true, wantStatus: 404, wantCode: "UNIT_NOT_FOUND"},
		{name: "police mismatch", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 0000 XX", wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED", wantMessage: "document vehicle or police number does not match Unit Lambung; contact operations to check master data", wantLog: "unit_police_mismatch"},
		{name: "no exact match", response: strings.ReplaceAll(sppLookupFixture, "SPG-BJM-2612481", "OTHER-SPP"), wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED"},
		{name: "empty array", response: `{"success":true,"data":[]}`, wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED"},
		{name: "unsuccessful document", response: `{"success":false,"data":null}`, wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED"},
		{name: "missing driver", response: strings.ReplaceAll(sppLookupFixture, "JOYO LUMBANRAJA ", " "), wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED"},
		{name: "upstream unauthorized", upstreamStatus: 401, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR", wantLog: "integration returned status 401"},
		{name: "upstream forbidden", upstreamStatus: 403, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "upstream service error", upstreamStatus: 503, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "upstream internal error", upstreamStatus: 500, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "malformed JSON", response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "invalid envelope", response: `{"success":true,"data":null}`, wantStatus: 500, wantCode: "INTERNAL_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			originalLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(originalLogger)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("search") != "SPG-BJM-2612481" {
					t.Error("SAOS must receive the complete trimmed number")
				}
				if test.upstreamStatus != 0 {
					w.WriteHeader(test.upstreamStatus)
				}
				_, _ = w.Write([]byte(test.response))
			}))
			defer upstream.Close()
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if test.querySnapshot {
				rows := sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi"})
				if test.snapshotPolice != "" {
					rows.AddRow(72, "SAMT072", test.snapshotPolice)
				}
				mock.ExpectQuery(regexp.QuoteMeta(`SELECT lambung_unit_id, no_lambung, no_polisi FROM unit_snapshot WHERE no_lambung = $1 AND is_active = TRUE`)).
					WithArgs("SAMT072").WillReturnRows(rows)
			}
			client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, SAOSToken: "secret-saos-service-token", RequestTimeout: time.Second})
			secret := []byte("test-secret")
			router := NewRouter(Dependencies{
				Documents: movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client),
				JWTSecret: secret, PermissionClaim: "permissions",
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/movement-documents/spp?number="+url.QueryEscape("  SPG-BJM-2612481  "), nil)
			request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			var body struct {
				Data  movement.LookupDocument `json:"data"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if test.wantStatus == 200 {
				want := movement.LookupDocument{NoLambung: "SAMT072", NoPolisi: "DA 8729 CB", DriverName: "JOYO LUMBANRAJA", SPPNumber: "SPG-BJM-2612481", CustomerID: "C400", CustomerName: "PT KAPUAS MAJU JAYA"}
				if body.Data != want {
					t.Fatal("lookup did not return the normalized confirmed fixture")
				}
			} else if body.Error.Code != test.wantCode || (test.wantMessage != "" && body.Error.Message != test.wantMessage) {
				t.Fatalf("error = %+v", body.Error)
			}
			if test.wantStatus != 200 && logs.Len() == 0 {
				t.Error("lookup failure must produce safe server diagnostics")
			}
			if test.wantLog != "" && !strings.Contains(logs.String(), test.wantLog) {
				t.Errorf("logs must identify failure reason %q", test.wantLog)
			}
			for _, sensitive := range []string{"secret-saos-service-token", "sensitive upstream response", "JOYO LUMBANRAJA", "PT KAPUAS MAJU JAYA", request.Header.Get("Authorization")} {
				if strings.Contains(logs.String(), sensitive) || (test.wantStatus != 200 && strings.Contains(response.Body.String(), sensitive)) {
					t.Error("diagnostics or client error leaked sensitive data")
				}
			}
			// No movement INSERT, status UPDATE, cycle, or duration SQL is allowed.
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSPPSubmitPreservesUpstreamFailureWithoutMovementWrites(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT m.transaction_id").WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
		WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
	client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, RequestTimeout: time.Second})
	service := movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client)
	_, err = service.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", movement.Input{
		NoLambung: "SAMT072", Direction: movement.DirectionOut, OutDestination: movement.OutFillingShed, SPPNumber: "SPG-BJM-2612481",
	}, "satpam-id", "Satpam")
	if !errors.Is(err, movement.ErrIntegrationUnavailable) || errors.Is(err, movement.ErrExternalDocument) {
		t.Fatalf("Record() must preserve the infrastructure failure: %v", err)
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	movementError(c, err)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("submit status = %d, want 500", response.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSPPSubmitRevalidatesPoliceInBothDirections(t *testing.T) {
	for _, direction := range []movement.Direction{movement.DirectionIn, movement.DirectionOut} {
		t.Run(string(direction), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(sppLookupFixture))
			}))
			defer upstream.Close()
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("SELECT m.transaction_id").WithArgs("96942009-f8c4-4dbf-b7e0-6d571765166a").
				WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT072").
				WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
					AddRow(72, "SAMT072", "DA 0000 XX", nil, nil, nil))
			mock.ExpectRollback()
			client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, RequestTimeout: time.Second})
			service := movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client)
			input := movement.Input{NoLambung: "SAMT072", Direction: direction, SPPNumber: "SPG-BJM-2612481"}
			if direction == movement.DirectionIn {
				input.InCategory = movement.InWaitingForAssignment
			} else {
				input.OutDestination = movement.OutFillingShed
			}
			_, err = service.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", input, "satpam-id", "Satpam")
			if !errors.Is(err, movement.ErrDocumentUnitMismatch) {
				t.Fatalf("Record() bypassed SPP police revalidation: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
