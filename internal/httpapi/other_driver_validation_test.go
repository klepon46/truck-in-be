package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"

	"truckin-be/internal/integration"
	"truckin-be/internal/movement"
)

func TestOtherSubmitDriverBypassKeepsPickerAndMovementRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, currentDirection, currentCategory string
		direction                               movement.Direction
		bypass, unknownUnit, invalidDriverID    bool
		returnSource                            string
		status                                  int
	}{
		{name: "default validation rejects full-name 404 despite client flag", direction: movement.DirectionOut, status: 422},
		{name: "bypass OUT preserves cycle and snapshot", direction: movement.DirectionOut, bypass: true, status: 201},
		{name: "bypass returning IN preserves cycle and duration", direction: movement.DirectionIn, currentDirection: "OUT", bypass: true, returnSource: movement.OutOther, status: 201},
		{name: "bypass does not allow OUT after OUT", direction: movement.DirectionOut, currentDirection: "OUT", bypass: true, status: 422},
		{name: "bypass does not allow OTHER from Gantungan", direction: movement.DirectionOut, currentDirection: "IN", currentCategory: movement.InWaitingForDelivery, bypass: true, status: 422},
		{name: "bypass does not allow OTHER IN after WO departure", direction: movement.DirectionIn, currentDirection: "OUT", returnSource: movement.OutWorkshop, bypass: true, status: 422},
		{name: "bypass does not allow an unknown unit", direction: movement.DirectionOut, bypass: true, unknownUnit: true, status: 404},
		{name: "bypass still requires positive driver ID", direction: movement.DirectionOut, bypass: true, invalidDriverID: true, status: 422},
	} {
		t.Run(test.name, func(t *testing.T) {
			var pickerCalls, submitChecks atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/internal/v1/active-drivers" || r.Header.Get("Authorization") != "Bearer test-driver-token" {
					t.Error("Driver integration path or service token changed")
				}
				switch r.URL.Query().Get("name") {
				case "Driver":
					pickerCalls.Add(1)
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":42,"driverName":"Driver Full Name","resignDate":null}]}`))
				case "Driver Full Name":
					submitChecks.Add(1)
					w.WriteHeader(http.StatusNotFound)
				default:
					t.Error("unexpected Driver search")
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer upstream.Close()
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if !test.invalidDriverID {
				mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
			}
			if test.bypass && !test.invalidDriverID {
				mock.ExpectBegin()
				rows := sqlmock.NewRows([]string{"lambung_unit_id", "no_lambung", "no_polisi", "current_transaction_id", "direction", "in_category"})
				var currentID, currentDirection, currentCategory any
				if test.currentDirection != "" {
					currentID, currentDirection = int64(19), test.currentDirection
				}
				if test.currentCategory != "" {
					currentCategory = test.currentCategory
				}
				if !test.unknownUnit {
					rows.AddRow(25, "SAMT025", "DA 0000 XX", currentID, currentDirection, currentCategory)
				}
				mock.ExpectQuery("SELECT u.lambung_unit_id").WithArgs("SAMT025").WillReturnRows(rows)
				if !test.unknownUnit {
					mock.ExpectQuery("SELECT m.transaction_id").WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}))
				}
				if test.direction == movement.DirectionIn {
					var wo any
					if test.returnSource == movement.OutWorkshop {
						wo = "WO-001"
					}
					mock.ExpectQuery("SELECT out_destination, work_order_number, spp_number").WithArgs(int64(19)).
						WillReturnRows(sqlmock.NewRows([]string{"out_destination", "work_order_number", "spp_number"}).AddRow(test.returnSource, wo, nil))
				}
				if test.status == 201 {
					category, destination := "", movement.OutOther
					var categoryValue, destinationValue any = nil, movement.OutOther
					if test.direction == movement.DirectionIn {
						category, destination = movement.InWaitingForAssignment, ""
						categoryValue, destinationValue = category, nil
					}
					mock.ExpectQuery("INSERT INTO movement_transactions").WithArgs(
						"96942009-f8c4-4dbf-b7e0-6d571765166a", sqlmock.AnyArg(), int64(25), "SAMT025", "DA 0000 XX",
						test.direction, category, destination, int64(42), "Driver Full Name", "", "", "", "", "Test note", "actor-1", "Satpam", "-3.3,114.5",
					).WillReturnRows(sqlmock.NewRows([]string{
						"transaction_id", "lambung_unit_id", "no_lambung_snapshot", "no_polisi_snapshot", "direction", "in_category", "out_destination",
						"driver_id", "driver_name_snapshot", "note", "coordinates", "occurred_at",
					}).AddRow(20, 25, "SAMT025", "DA 0000 XX", test.direction, categoryValue, destinationValue, 42, "Driver Full Name", "Test note", "-3.3,114.5", time.Now()))
					if test.direction == movement.DirectionOut {
						mock.ExpectQuery("INSERT INTO movement_cycles").WithArgs(int64(25), int64(20)).
							WillReturnRows(sqlmock.NewRows([]string{"cycle_id"}).AddRow(91))
					} else {
						mock.ExpectQuery("SELECT c.cycle_id").WithArgs(int64(19)).
							WillReturnRows(sqlmock.NewRows([]string{"cycle_id", "departure_transaction_id", "departure_work_order_number", "departure_spp_number"}).AddRow(91, 19, nil, nil))
						mock.ExpectExec("UPDATE movement_cycles").WithArgs(int64(20), int64(91)).WillReturnResult(sqlmock.NewResult(0, 1))
						mock.ExpectExec("INSERT INTO movement_status_durations").WithArgs(int64(19)).WillReturnResult(sqlmock.NewResult(0, 1))
					}
					mock.ExpectExec("UPDATE unit_snapshot SET current_transaction_id").WithArgs(int64(20), int64(25)).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				} else {
					mock.ExpectRollback()
				}
			}
			client := integration.NewClient(integration.Config{DriverBaseURL: upstream.URL, DriverToken: "test-driver-token", RequestTimeout: time.Second})
			var options []movement.ServiceOption
			if test.bypass {
				options = append(options, movement.WithOtherDriverValidation(false))
			}
			secret := []byte("test-secret")
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"exp": time.Now().Add(time.Minute).Unix(), "permissions": []string{appPermission}, "actorId": "actor-1", "actorName": "Satpam",
			}).SignedString(secret)
			if err != nil {
				t.Fatal(err)
			}
			router := NewRouter(Dependencies{
				Movement: movement.NewService(sqlx.NewDb(db, "sqlmock"), nil, client, options...), Drivers: client,
				JWTSecret: secret, PermissionClaim: "permissions", ActorIDClaim: "actorId", ActorNameClaim: "actorName",
			})
			search := httptest.NewRequest(http.MethodGet, "/api/v1/drivers?name=Driver", nil)
			search.Header.Set("Authorization", "Bearer "+token)
			searchResult := httptest.NewRecorder()
			router.ServeHTTP(searchResult, search)
			if searchResult.Code != 200 || pickerCalls.Load() != 1 {
				t.Fatal("the active-driver picker must remain operational")
			}
			body := map[string]any{
				"noLambung": "SAMT025", "direction": test.direction, "driverId": 42,
				"driverName": " Driver Full Name ", "note": " Test note ", "coordinates": " -3.3 , 114.5 ",
				"validate_other_driver_on_submit": false, // A client field must not control the server switch.
			}
			if test.direction == movement.DirectionIn {
				body["inCategory"] = movement.InWaitingForAssignment
			} else {
				body["outDestination"] = movement.OutOther
			}
			if test.invalidDriverID {
				body["driverId"] = -42
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/movements", bytes.NewReader(encoded))
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "96942009-f8c4-4dbf-b7e0-6d571765166a")
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			if result.Code != test.status {
				t.Fatalf("submit status = %d, want %d: %s", result.Code, test.status, result.Body.String())
			}
			if (test.bypass && submitChecks.Load() != 0) || (!test.bypass && submitChecks.Load() != 1) {
				t.Fatal("submit Driver call count did not follow the server configuration")
			}
			if test.status == 201 {
				var response struct {
					Data movement.Transaction `json:"data"`
				}
				if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Data.DriverID == nil || *response.Data.DriverID != 42 || response.Data.DriverName == nil || *response.Data.DriverName != "Driver Full Name" ||
					response.Data.Coordinates == nil || *response.Data.Coordinates != "-3.3,114.5" || response.Data.CycleID == nil || *response.Data.CycleID != 91 {
					t.Fatal("OTHER submit did not preserve snapshots, coordinates, and cycle")
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
