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
	"reflect"
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
		name             string
		upstreamStatus   int
		response         string
		querySnapshot    bool
		snapshotPolice   string
		wantStatus       int
		wantCode         string
		wantMessage      string
		wantLog          string
		databaseFailure  bool
		direction        string
		category         string
		activeSPP        any
		activeCustomer   any
		used             bool
		completed        bool
		wantDestinations []string
	}{
		{name: "actual fixture without movement writes", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", wantStatus: 200},
		{name: "OUT lookup still works for returning IN", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", direction: "OUT", used: true, wantStatus: 200, wantDestinations: []string{}},
		{name: "Gantungan matching active SPP", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", direction: "IN", category: movement.InWaitingForDelivery, activeSPP: "SPG-BJM-2612481", activeCustomer: "C400", used: true, wantStatus: 200, wantDestinations: []string{movement.OutCustomer}},
		{name: "Service resumes both destinations", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "MASTER PLATE", direction: "IN", category: movement.InServiceAndRepair, activeSPP: "SPG-BJM-2612481", activeCustomer: "C400", used: true, wantStatus: 200, wantDestinations: []string{movement.OutCustomer, movement.OutFillingShed}},
		{name: "Service cannot change customer", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "MASTER PLATE", direction: "IN", category: movement.InServiceAndRepair, activeSPP: "SPG-BJM-2612481", activeCustomer: "OTHER", used: true, wantStatus: 200, wantDestinations: []string{}},
		{name: "Service cannot revive terminal SPP", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "MASTER PLATE", direction: "IN", category: movement.InServiceAndRepair, activeSPP: "SPG-BJM-2612481", activeCustomer: "C400", used: true, completed: true, wantStatus: 200, wantDestinations: []string{}},
		{name: "Gantungan different active SPP", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", direction: "IN", category: movement.InWaitingForDelivery, activeSPP: "OTHER-SPP", activeCustomer: "C400", wantStatus: 200, wantDestinations: []string{}},
		{name: "Gantungan customer mismatch is not a lookup error", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", direction: "IN", category: movement.InWaitingForDelivery, activeSPP: "SPG-BJM-2612481", activeCustomer: "OTHER-CUSTOMER", wantStatus: 200, wantDestinations: []string{}},
		{name: "completed SPP is not offered", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 8729 CB", direction: "IN", category: movement.InWaitingForAssignment, completed: true, wantStatus: 200, wantDestinations: []string{}},
		{name: "unit not found", response: sppLookupFixture, querySnapshot: true, wantStatus: 404, wantCode: "UNIT_NOT_FOUND"},
		{name: "different SAOS police ignored", response: sppLookupFixture, querySnapshot: true, snapshotPolice: "DA 0000 XX", wantStatus: 200},
		{name: "database failure is not empty eligibility", response: sppLookupFixture, querySnapshot: true, databaseFailure: true, wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "ambiguous response is not absence", response: strings.Replace(sppLookupFixture, `"data":[{`, `"data":[{"trans_no":"OTHER-SPP"},{`, 1), wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED", wantLog: "document_invalid"},
		{name: "empty array", response: `{"success":true,"data":[]}`, wantStatus: 404, wantCode: "SPP_NOT_FOUND", wantLog: "spp_not_found"},
		{name: "unsuccessful document", response: `{"success":false,"data":null}`, wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED"},
		{name: "missing driver", response: strings.ReplaceAll(sppLookupFixture, "JOYO LUMBANRAJA ", " "), wantStatus: 422, wantCode: "DOCUMENT_VALIDATION_FAILED"},
		{name: "upstream unauthorized", upstreamStatus: 401, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR", wantLog: "integration returned status 401"},
		{name: "upstream forbidden", upstreamStatus: 403, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "upstream service error", upstreamStatus: 503, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "upstream internal error", upstreamStatus: 500, response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "malformed JSON", response: "sensitive upstream response", wantStatus: 500, wantCode: "INTERNAL_ERROR"},
		{name: "invalid envelope", response: `{"success":true}`, wantStatus: 500, wantCode: "INTERNAL_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			originalLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(originalLogger)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("id") != "123" || r.URL.Query().Get("owner") != "example" || r.URL.Query().Has("search") {
					t.Error("SAOS must receive separate ID and owner, never the removed search parameter")
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
				rows := sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category", "active_spp", "active_customer_id", "filling_shed_used", "spp_completed"})
				var currentID any
				if test.direction != "" {
					currentID = int64(20)
				}
				if test.snapshotPolice != "" {
					rows.AddRow(72, "SAMT072", test.snapshotPolice, currentID, test.direction, test.category, test.activeSPP, test.activeCustomer, test.used, test.completed)
				}
				expectation := mock.ExpectQuery(regexp.QuoteMeta(`SELECT u.lambung_unit_id, u.no_lambung, u.no_polisi, u.current_transaction_id,`)).
					WithArgs("SAMT072", "SPG-BJM-2612481")
				if test.databaseFailure {
					expectation.WillReturnError(errors.New("test database unavailable"))
				} else {
					expectation.WillReturnRows(rows)
				}
			}
			client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, SAOSToken: "secret-saos-service-token", RequestTimeout: time.Second})
			secret := []byte("test-secret")
			router := NewRouter(Dependencies{
				Documents: movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client),
				JWTSecret: secret, PermissionClaim: "permissions",
			})
			request := httptest.NewRequest(http.MethodGet, "/api/v1/movement-documents/spp?number="+url.QueryEscape("  123-example  "), nil)
			request.Header.Set("Authorization", "Bearer "+testToken(t, secret, appPermission))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			var body struct {
				Data  movement.SPPLookupDocument `json:"data"`
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if test.wantStatus == 200 {
				want := movement.LookupDocument{NoLambung: "SAMT072", NoPolisi: test.snapshotPolice, DriverName: "JOYO LUMBANRAJA", SPPNumber: "123-example", CustomerID: "C400", CustomerName: "PT KAPUAS MAJU JAYA"}
				wantDestinations := test.wantDestinations
				if wantDestinations == nil {
					wantDestinations = []string{movement.OutFillingShed}
				}
				if body.Data.LookupDocument != want || !reflect.DeepEqual(body.Data.AllowedOutDestinations, wantDestinations) {
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
		NoLambung: "SAMT072", Direction: movement.DirectionOut, OutDestination: movement.OutFillingShed, SPPNumber: "123-example",
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

// No format assumptions about the unused SAOS field are permitted.
func sppPoliceResponses() map[string]string {
	responses := map[string]string{"omitted": strings.ReplaceAll(sppLookupFixture, `"unit_police_no":"DA 8729 CB",`, "")}
	for name, value := range map[string]string{
		"annotated": `"DA 8957 JI (SAFT 08)"`, "arbitrary": `"unstructured / unknown vehicle notes"`,
		"different plate": `"DA 9999 XX"`, "null": `null`, "blank": `""`, "whitespace": `"   "`,
		"non-string": `{"ignored":true}`,
	} {
		responses[name] = strings.ReplaceAll(sppLookupFixture, `"DA 8729 CB"`, value)
	}
	return responses
}

func TestSPPLookupIgnoresSAOSPolice(t *testing.T) {
	for name, body := range sppPoliceResponses() {
		t.Run(name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer upstream.Close()
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT072", "SPG-BJM-2612481").
				WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category", "active_spp", "active_customer_id", "filling_shed_used", "spp_completed"}).
					AddRow(72, "SAMT072", "MASTER PLATE", nil, "", "", nil, nil, false, false))
			client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, RequestTimeout: time.Second})
			result, err := movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client).LookupSPP(context.Background(), "123-example")
			if err != nil || result.NoPolisi != "MASTER PLATE" || result.NoLambung != "SAMT072" || !reflect.DeepEqual(result.AllowedOutDestinations, []string{movement.OutFillingShed}) {
				t.Fatalf("lookup must ignore SAOS police and return master unit: %+v, %v", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSPPSubmitSnapshotsMasterPoliceInBothDirections(t *testing.T) {
	for name, body := range sppPoliceResponses() {
		for _, direction := range []movement.Direction{movement.DirectionIn, movement.DirectionOut} {
			t.Run(name+"/"+string(direction), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(body))
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
				var currentID, previousDirection any
				category, destination := "", movement.OutFillingShed
				if direction == movement.DirectionIn {
					currentID, previousDirection = int64(19), "OUT"
					category, destination = movement.InWaitingForAssignment, ""
				}
				mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT072").
					WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
						AddRow(72, "SAMT072", "MASTER PLATE", currentID, previousDirection, nil))
				mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
				mock.ExpectExec(`SELECT pg_advisory_xact_lock\(8105, hashtext\(\$1\)\)`).WithArgs("SPG-BJM-2612481").WillReturnResult(sqlmock.NewResult(0, 1))
				if direction == movement.DirectionIn {
					mock.ExpectQuery("SELECT out_destination, work_order_number, spp_number").WithArgs(int64(19)).
						WillReturnRows(sqlmock.NewRows([]string{"out_destination", "work_order_number", "spp_number"}).AddRow(movement.OutFillingShed, nil, "SPG-BJM-2612481"))
				} else {
					mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT072", "SPG-BJM-2612481").
						WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category", "active_spp", "active_customer_id", "filling_shed_used", "spp_completed"}).
							AddRow(72, "SAMT072", "MASTER PLATE", nil, "", "", nil, nil, false, false))
				}
				mock.ExpectQuery("INSERT INTO movement_transactions").WithArgs(
					"96942009-f8c4-4dbf-b7e0-6d571765166a", sqlmock.AnyArg(), int64(72), "SAMT072", "MASTER PLATE",
					direction, category, destination, int64(0), "JOYO LUMBANRAJA", "", "SPG-BJM-2612481", "C400", "PT KAPUAS MAJU JAYA", "", "satpam-id", "Satpam", "-3,114",
				).WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "no_lambung_snapshot", "no_polisi_snapshot", "direction", "spp_number", "coordinates", "occurred_at"}).
					AddRow(20, "SAMT072", "MASTER PLATE", direction, "SPG-BJM-2612481", "-3,114", time.Now()))
				if direction == movement.DirectionOut {
					mock.ExpectQuery("INSERT INTO movement_cycles").WithArgs(int64(72), int64(20)).WillReturnRows(sqlmock.NewRows([]string{"cycle_id"}).AddRow(91))
				} else {
					mock.ExpectQuery("SELECT c.cycle_id").WithArgs(int64(19)).WillReturnRows(sqlmock.NewRows([]string{"cycle_id", "departure_transaction_id", "departure_work_order_number", "departure_spp_number"}).AddRow(91, 19, nil, "SPG-BJM-2612481"))
					mock.ExpectExec("UPDATE movement_cycles").WithArgs(int64(20), int64(91)).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("INSERT INTO movement_status_durations").WithArgs(int64(19)).WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectExec("UPDATE unit_snapshot SET current_transaction_id").WithArgs(int64(20), int64(72)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
				client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, RequestTimeout: time.Second})
				service := movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client)
				input := movement.Input{NoLambung: "SAMT072", Direction: direction, SPPNumber: "123-example", Coordinates: "-3,114"}
				if direction == movement.DirectionIn {
					input.InCategory = movement.InWaitingForAssignment
				} else {
					input.OutDestination = movement.OutFillingShed
				}
				result, err := service.Record(context.Background(), "96942009-f8c4-4dbf-b7e0-6d571765166a", input, "satpam-id", "Satpam")
				if err != nil || result.NoPolisi != "MASTER PLATE" || result.SPPNumber == nil || *result.SPPNumber != "SPG-BJM-2612481" || result.Coordinates == nil || *result.Coordinates != "-3,114" || result.CycleID == nil {
					t.Fatalf("SPP submit must snapshot master police and preserve canonical audit/cycle/coordinates: %+v, %v", result, err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
