package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUnitsReadsCursorPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/lambung/v1/internal/unit-snapshots" {
			t.Errorf("request path = %q", request.URL.Path)
			return
		}
		if request.URL.Query().Get("cursor") != "cursor-41" || request.URL.Query().Get("limit") != "100" {
			t.Errorf("request query = %q", request.URL.RawQuery)
			return
		}
		if request.Header.Get("Authorization") != "Bearer service-token" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			return
		}
		_, _ = writer.Write([]byte(`{
			"data": {
				"items": [{"id": 42, "noLambung": "SAMT007", "noPolisi": "DA 8515 CX"}],
				"nextCursor": "cursor-42",
				"hasMore": true
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{
		LambungBaseURL: server.URL,
		LambungToken:   "service-token",
		ConnectTimeout: time.Second,
		RequestTimeout: time.Second,
	})

	page, err := client.Units(context.Background(), "cursor-41", 100)
	if err != nil {
		t.Fatalf("Units() error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 42 || page.NextCursor == nil || *page.NextCursor != "cursor-42" || !page.HasMore {
		t.Fatalf("Units() = %#v", page)
	}
}

func TestUnitsRejectsMissingNextCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"data":{"items":[],"nextCursor":null,"hasMore":true}}`))
	}))
	defer server.Close()

	client := NewClient(Config{
		LambungBaseURL: server.URL,
		ConnectTimeout: time.Second,
		RequestTimeout: time.Second,
	})

	if _, err := client.Units(context.Background(), "", 100); err == nil {
		t.Fatal("Units() accepted hasMore without nextCursor")
	}
}

func TestUnitsRejectsIncompleteCursorPages(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "missing data", response: `{}`},
		{name: "missing items", response: `{"data":{"nextCursor":null,"hasMore":false}}`},
		{name: "missing hasMore", response: `{"data":{"items":[],"nextCursor":null}}`},
		{name: "missing nextCursor", response: `{"data":{"items":[],"hasMore":false}}`},
		{name: "final page has cursor", response: `{"data":{"items":[],"nextCursor":"cursor-42","hasMore":false}}`},
		{name: "empty non-terminal page", response: `{"data":{"items":[],"nextCursor":"cursor-42","hasMore":true}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.response))
			}))
			defer server.Close()

			client := NewClient(Config{
				LambungBaseURL: server.URL,
				ConnectTimeout: time.Second,
				RequestTimeout: time.Second,
			})

			if _, err := client.Units(context.Background(), "", 100); err == nil {
				t.Fatalf("Units() accepted %s", test.name)
			}
		})
	}
}

func TestWorkshopMapsDocumentVehicleAndPoliceDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/internal/v1/work-orders/by-wo" || request.URL.Query().Get("wo-number") != "WO-001" {
			t.Fatalf("request = %s", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer service-token" {
			t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = writer.Write([]byte(`{
			"success": true,
			"code": 1073741824,
			"data": {
				"work_order_number": "WO-001",
				"driver_name": " Workshop Driver ",
				"vehicle_no": "A122",
				"police_no": "DA 122 A"
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{WorkshopBaseURL: server.URL, WorkshopToken: "service-token", ConnectTimeout: time.Second, RequestTimeout: time.Second})
	document, err := client.Workshop(context.Background(), "WO-001")
	if err != nil {
		t.Fatalf("Workshop() error = %v", err)
	}
	if document.NoLambung != "A122" || document.NoPolisi != "DA 122 A" || document.DriverName != "Workshop Driver" {
		t.Fatalf("Workshop() = %#v", document)
	}
}

func TestSPPMapsDocumentVehiclePoliceAndCustomerDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/loading-orders" || request.URL.Query().Get("transNo") != "SPP-001" {
			t.Fatalf("request = %s", request.URL.String())
		}
		_, _ = writer.Write([]byte(`{
			"success": true,
			"code": 0,
			"data": [{
				"trans_no": "SPP-001",
				"driver_name": " SPP Driver ",
				"unit_name": "A122",
				"unit_police_no": "DA 122 A",
				"customer_code": "CUST-1",
				"customer_name": "Customer One"
			}]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{SAOSBaseURL: server.URL, SAOSToken: "service-token", ConnectTimeout: time.Second, RequestTimeout: time.Second})
	document, err := client.SPP(context.Background(), "SPP-001")
	if err != nil {
		t.Fatalf("SPP() error = %v", err)
	}
	if document.NoLambung != "A122" || document.NoPolisi != "DA 122 A" || document.DriverName != "SPP Driver" || document.CustomerID != "CUST-1" || document.CustomerName != "Customer One" {
		t.Fatalf("SPP() = %#v", document)
	}
}

func TestWorkshopRejectsUnsuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"success":false,"data":{"work_order_number":"WO-001","driver_name":"Driver","vehicle_no":"A122","police_no":"DA 122 A"}}`))
	}))
	defer server.Close()

	client := NewClient(Config{WorkshopBaseURL: server.URL, ConnectTimeout: time.Second, RequestTimeout: time.Second})
	if _, err := client.Workshop(context.Background(), "WO-001"); err == nil {
		t.Fatal("Workshop() accepted an unsuccessful response")
	}
}

func TestActiveDriversUsesSwaggerContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/internal/v1/active-drivers" {
			t.Errorf("request path = %q", request.URL.Path)
			return
		}
		if request.URL.Query().Get("name") != "Driver Name" {
			t.Errorf("request query = %q", request.URL.RawQuery)
			return
		}
		if request.Header.Get("Authorization") != "Bearer service-token" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
			return
		}
		_, _ = writer.Write([]byte(`{
			"success": true,
			"code": 200,
			"data": [
				{"id": 42, "driverName": "Driver Name", "organizationName": "Pool"},
				{"id": 43, "driverName": "Former Driver", "resignDate": "2026-01-01"}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(Config{
		DriverBaseURL:  server.URL,
		DriverToken:    "service-token",
		ConnectTimeout: time.Second,
		RequestTimeout: time.Second,
	})

	drivers, err := client.ActiveDrivers(context.Background(), "Driver Name")
	if err != nil {
		t.Fatalf("ActiveDrivers() error = %v", err)
	}
	if len(drivers) != 1 || drivers[0] != (ActiveDriver{ID: 42, DriverName: "Driver Name"}) {
		t.Fatalf("ActiveDrivers() = %#v", drivers)
	}
}
