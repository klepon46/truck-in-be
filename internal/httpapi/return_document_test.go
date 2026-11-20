package httpapi

import (
	"bytes"
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
	"github.com/lib/pq"

	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
)

func TestReturningINHTTPAcceptsChangedNumberButNotVehicle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []string{"WO", "SPP"} {
		for _, wrongUnit := range []bool{false, true} {
			name := source + "/same-unit"
			if wrongUnit {
				name = source + "/wrong-unit"
			}
			t.Run(name, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body := sppLookupFixture
					if source == "WO" {
						if r.URL.Query().Get("wo-number") != "WO-B" {
							t.Error("lookup must validate the actual incoming WO-B")
						}
						body = `{"success":true,"data":{"work_order_number":"WO-B","vehicle_no":"SAMT072","police_no":"MASTER PLATE","driver_name":"Driver"}}`
					} else if r.URL.Query().Get("id") != "123" || r.URL.Query().Get("owner") != "example" {
						t.Error("lookup must resolve the actual incoming SPP reference")
					}
					if wrongUnit {
						body = strings.ReplaceAll(body, "SAMT072", "SAMT999")
					}
					_, _ = w.Write([]byte(body))
				}))
				defer upstream.Close()
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
				var originalWO, originalSPP, incomingWO, incomingSPP any
				destination := movement.OutWorkshop
				body := map[string]any{"noLambung": "SAMT072", "direction": "IN", "inCategory": movement.InWaitingForAssignment, "coordinates": "-3,114"}
				if source == "WO" {
					originalWO, incomingWO = "WO-A", "WO-B"
					body["workOrderNumber"] = "WO-B"
				} else {
					destination = movement.OutFillingShed
					originalSPP, incomingSPP = "SPP-A", "SPG-BJM-2612481"
					body["sppNumber"] = "123-example"
				}
				if !wrongUnit {
					mock.ExpectBegin()
					mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT072").
						WillReturnRows(sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"}).
							AddRow(72, "SAMT072", "MASTER PLATE", 19, "OUT", nil))
					mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
					mock.ExpectQuery("SELECT out_destination, work_order_number, spp_number").WithArgs(int64(19), int64(72)).
						WillReturnRows(sqlmock.NewRows([]string{"out_destination", "work_order_number", "spp_number"}).AddRow(destination, originalWO, originalSPP))
					if source == "SPP" {
						mock.ExpectExec(`SELECT pg_advisory_xact_lock\(8105, spp_key\)`).
							WithArgs(pq.Array([]string{"SPG-BJM-2612481", "SPP-A"})).WillReturnResult(sqlmock.NewResult(0, 1))
					}
					mock.ExpectQuery("INSERT INTO movement_transactions").
						WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "direction", "work_order_number", "spp_number", "coordinates"}).
							AddRow(20, "IN", incomingWO, incomingSPP, "-3,114"))
					mock.ExpectQuery("SELECT c.cycle_id").WithArgs(int64(19)).
						WillReturnRows(sqlmock.NewRows([]string{"cycle_id", "departure_transaction_id", "departure_work_order_number", "departure_spp_number"}).
							AddRow(91, 19, originalWO, originalSPP))
					mock.ExpectExec("UPDATE movement_cycles").WithArgs(int64(20), int64(91)).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("INSERT INTO movement_status_durations").WithArgs(int64(19)).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE unit_snapshot SET current_transaction_id").WithArgs(int64(20), int64(72)).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				}
				secret := []byte("test-secret")
				token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
					"exp": time.Now().Add(time.Minute).Unix(), "permissions": []string{appPermission}, "actorId": "actor", "actorName": "Satpam",
				}).SignedString(secret)
				if err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(body)
				request := httptest.NewRequest(http.MethodPost, "/api/v1/movements", bytes.NewReader(encoded))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Idempotency-Key", "96942009-f8c4-4dbf-b7e0-6d571765166a")
				response := httptest.NewRecorder()
				client := integration.NewClient(integration.Config{SAOSBaseURL: upstream.URL, WorkshopBaseURL: upstream.URL, RequestTimeout: time.Second})
				NewRouter(Dependencies{
					Movement: movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client), JWTSecret: secret,
					PermissionClaim: "permissions", ActorIDClaim: "actorId", ActorNameClaim: "actorName",
				}).ServeHTTP(response, request)
				want := http.StatusCreated
				if wrongUnit {
					want = http.StatusUnprocessableEntity
				}
				if response.Code != want {
					t.Fatalf("status %d, want %d: %s", response.Code, want, response.Body.String())
				}
				if !wrongUnit {
					var result struct {
						Data movement.Transaction `json:"data"`
					}
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Data.CycleID == nil || *result.Data.CycleID != 91 || result.Data.Coordinates == nil || *result.Data.Coordinates != "-3,114" ||
						(source == "WO" && (result.Data.WorkOrderNumber == nil || *result.Data.WorkOrderNumber != incomingWO || result.Data.DepartureWorkOrderNumber == nil || *result.Data.DepartureWorkOrderNumber != originalWO)) ||
						(source == "SPP" && (result.Data.SPPNumber == nil || *result.Data.SPPNumber != incomingSPP || result.Data.DepartureSPPNumber == nil || *result.Data.DepartureSPPNumber != originalSPP)) {
						t.Fatal("response must distinguish scanned IN and original OUT documents")
					}
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
