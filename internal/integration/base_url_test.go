package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"truckin-be/internal/movement"
)

func TestIntegrationRequestsPreserveServiceBasePaths(t *testing.T) {
	const cursor = "cursor/+?=&中"
	const owner = "Example-East & Co/+?=中"
	const wo = "WO/2026/+?=&中"
	const driver = "Driver /+?=&中"
	for _, base := range []struct{ name, suffix, prefix string }{
		{"origin", "", ""},
		{"origin slash", "/", ""},
		{"service", "/lambung-service", "/lambung-service"},
		{"service slash", "/lambung-service/", "/lambung-service"},
		{"nested service", "/gateway/team/service", "/gateway/team/service"},
		{"nested service slash", "/gateway/team/service/", "/gateway/team/service"},
		{"escaped service", "/gateway/team%2Fone/service%20name/", "/gateway/team%2Fone/service%20name"},
		{"base query and fragment discarded", "/service?discard=base#discard", "/service"},
	} {
		for _, test := range []struct {
			name, endpoint, token, response string
			query                           url.Values
			call                            func(*Client) error
		}{
			{"Lambung", "/lambung/v1/internal/unit-snapshots", "lambung-test-token", `{"data":{"items":[],"nextCursor":null,"hasMore":false}}`,
				url.Values{"cursor": {cursor}, "limit": {"100"}}, func(c *Client) error { _, err := c.Units(context.Background(), cursor, 100); return err }},
			{"SAOS", "/api/v2/loading-orders", "saos-test-token", actualSPPResponse,
				url.Values{"id": {"123"}, "owner": {owner}}, func(c *Client) error { _, err := c.SPP(context.Background(), "123-"+owner); return err }},
			{"Workshop", "/internal/v1/work-orders/by-wo", "workshop-test-token", `{"success":true,"data":{"work_order_number":"` + wo + `","vehicle_no":"SAMT119","police_no":"DA 1234 AB","driver_name":"Driver"}}`,
				url.Values{"wo-number": {wo}}, func(c *Client) error { _, err := c.Workshop(context.Background(), wo); return err }},
			{"Driver", "/work-orders/v2/active-drivers", "driver-test-token", `{"success":true,"data":[{"id":42,"driverName":"` + driver + `"}]}`,
				url.Values{"name": {driver}}, func(c *Client) error { return c.Driver(context.Background(), 42, driver) }},
		} {
			t.Run(base.name+"/"+test.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.Method != http.MethodGet || r.URL.EscapedPath() != base.prefix+test.endpoint {
						t.Errorf("method/path = %s %s, want GET %s", r.Method, r.URL.EscapedPath(), base.prefix+test.endpoint)
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if !reflect.DeepEqual(r.URL.Query(), test.query) {
						t.Errorf("query data changed: got %v, want %v", r.URL.Query(), test.query)
					}
					if r.Header.Get("Authorization") != "Bearer "+test.token || r.Header.Get("Accept") != "application/json" {
						t.Error("service authentication/Accept headers changed")
					}
					_, _ = w.Write([]byte(test.response))
				}))
				defer server.Close()
				baseURL := server.URL + base.suffix
				client := NewClient(Config{
					LambungBaseURL: baseURL, LambungToken: "lambung-test-token",
					SAOSBaseURL: baseURL, SAOSToken: "saos-test-token",
					WorkshopBaseURL: baseURL, WorkshopToken: "workshop-test-token",
					DriverBaseURL: baseURL, DriverToken: "driver-test-token",
					ConnectTimeout: time.Second, RequestTimeout: time.Second,
				})
				if err := test.call(client); err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 1 {
					t.Fatalf("expected one request without a redirect/fallback, got %d", calls.Load())
				}
			})
		}
	}
}

func TestPrefixedWorkshopPreservesNotFoundClassification(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       error
	}{
		{"confirmed absence", workshopNotFoundResponse, movement.ErrWorkOrderNotFound},
		{"generic route 404", "private upstream body", movement.ErrIntegrationUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/workshop-service/internal/v1/work-orders/by-wo" {
					t.Error("Workshop service prefix was discarded")
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := NewClient(Config{WorkshopBaseURL: server.URL + "/workshop-service/", WorkshopToken: "private-test-token", RequestTimeout: time.Second})
			_, err := client.Workshop(context.Background(), "DOC/001")
			if !errors.Is(err, test.want) {
				t.Fatalf("not-found classification changed: %v", err)
			}
			for _, sensitive := range []string{server.URL, "private-test-token", "private upstream body", "DOC/001"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Error("integration error leaked URL/token/body/document")
				}
			}
		})
	}
}

func TestPrefixedLambungPreservesStatusErrors(t *testing.T) {
	for _, status := range []int{401, 403, 404, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/lambung-service/lambung/v1/internal/unit-snapshots" {
					t.Error("Lambung service prefix was discarded")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private upstream body"))
			}))
			defer server.Close()
			client := NewClient(Config{LambungBaseURL: server.URL + "/lambung-service", LambungToken: "private-test-token", RequestTimeout: time.Second})
			_, err := client.Units(context.Background(), "private-cursor", 100)
			if err == nil || err.Error() != "integration returned status "+strconv.Itoa(status) {
				t.Fatalf("HTTP status must remain distinct from transport failure: %v", err)
			}
		})
	}
}
