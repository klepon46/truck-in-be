package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"truckin-be/internal/movement"
)

// Verified against Workshop InternalController and SAOS V2 handler/repository sources.
const workshopNotFoundResponse = `{"success":false,"code":404,"message":"Work order not found","data":null}`

func TestDocumentAbsenceClassification(t *testing.T) {
	for _, test := range []struct {
		name, kind, response string
		status               int
		want                 error
	}{
		{"WO confirmed absent", "WO", workshopNotFoundResponse, 404, movement.ErrWorkOrderNotFound},
		{"WO absent data omitted", "WO", `{"success":false,"code":404,"message":"Work order not found"}`, 404, movement.ErrWorkOrderNotFound},
		{"WO generic proxy 404", "WO", "private proxy body", 404, movement.ErrIntegrationUnavailable},
		{"WO generic JSON 404", "WO", `{"success":false,"code":404,"message":"No route"}`, 404, movement.ErrIntegrationUnavailable},
		{"WO contradictory 404", "WO", `{"success":true,"code":404,"message":"Work order not found"}`, 404, movement.ErrIntegrationUnavailable},
		{"WO missing code on 404", "WO", `{"success":false,"message":"Work order not found"}`, 404, movement.ErrIntegrationUnavailable},
		{"WO false not-found over HTTP 200", "WO", workshopNotFoundResponse, 200, movement.ErrExternalDocument},
		{"WO mismatched number", "WO", `{"success":true,"data":{"work_order_number":"OTHER-WO"}}`, 200, movement.ErrExternalDocument},
		{"WO incomplete record", "WO", `{"success":true,"data":{"work_order_number":"DOC/001","driver_name":" "}}`, 200, movement.ErrExternalDocument},
		{"WO malformed envelope", "WO", `{"success":true,"data":null}`, 200, movement.ErrIntegrationUnavailable},
		{"WO missing success", "WO", `{"data":{}}`, 200, movement.ErrIntegrationUnavailable},
		{"WO auth failure", "WO", workshopNotFoundResponse, 401, movement.ErrIntegrationUnavailable},
		{"WO service failure", "WO", workshopNotFoundResponse, 500, movement.ErrIntegrationUnavailable},
		{"SPP empty list", "SPP", `{"success":true,"data":[]}`, 200, movement.ErrSPPNotFound},
		{"SPP nil producer slice", "SPP", `{"success":true,"message":"Success","code":200,"data":null}`, 200, movement.ErrSPPNotFound},
		{"SPP no exact match", "SPP", `{"success":true,"data":[{"trans_no":"DOC/0010"}]}`, 200, movement.ErrSPPNotFound},
		{"SPP unsuccessful envelope", "SPP", `{"success":false,"data":null}`, 200, movement.ErrExternalDocument},
		{"SPP incomplete record", "SPP", `{"success":true,"data":[{"trans_no":"DOC/001","driver_name":" "}]}`, 200, movement.ErrExternalDocument},
		{"SPP missing data", "SPP", `{"success":true}`, 200, movement.ErrIntegrationUnavailable},
		{"SPP wrong data type", "SPP", `{"success":true,"data":{}}`, 200, movement.ErrIntegrationUnavailable},
		{"SPP malformed JSON", "SPP", "private upstream body", 200, movement.ErrIntegrationUnavailable},
		{"SPP auth failure", "SPP", `{"success":true,"data":[]}`, 401, movement.ErrIntegrationUnavailable},
		{"SPP service failure", "SPP", `{"success":true,"data":[]}`, 503, movement.ErrIntegrationUnavailable},
		{"SPP route 404 is not search absence", "SPP", workshopNotFoundResponse, 404, movement.ErrIntegrationUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := "wo-number"
				if test.kind == "SPP" {
					query = "search"
				}
				if r.URL.Query().Get(query) != "DOC/001" {
					t.Error("document number must remain complete and trimmed")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.response))
			}))
			defer upstream.Close()
			client := NewClient(Config{WorkshopBaseURL: upstream.URL, SAOSBaseURL: upstream.URL, WorkshopToken: "private-token", SAOSToken: "private-token", RequestTimeout: time.Second})
			lookup := client.Workshop
			if test.kind == "SPP" {
				lookup = client.SPP
			}
			_, err := lookup(context.Background(), "  DOC/001  ")
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			for _, other := range []error{movement.ErrWorkOrderNotFound, movement.ErrSPPNotFound, movement.ErrExternalDocument, movement.ErrIntegrationUnavailable} {
				if other != test.want && errors.Is(err, other) {
					t.Fatalf("error must not be misclassified as %v", other)
				}
			}
			for _, sensitive := range []string{"private-token", "private upstream body", "private proxy body", upstream.URL, "DOC/001"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Error("document error leaked upstream or request data")
				}
			}
		})
	}
}

func TestDocumentConnectionFailureIsNeverNotFound(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	client := NewClient(Config{WorkshopBaseURL: upstream.URL, SAOSBaseURL: upstream.URL, RequestTimeout: time.Second})
	for _, lookup := range []func(context.Context, string) (movement.Document, error){client.Workshop, client.SPP} {
		_, err := lookup(context.Background(), "DOC-001")
		if !errors.Is(err, movement.ErrIntegrationUnavailable) || errors.Is(err, movement.ErrWorkOrderNotFound) || errors.Is(err, movement.ErrSPPNotFound) {
			t.Fatalf("connection failure misclassified: %v", err)
		}
	}
}

func TestWorkshop404DecodingDoesNotChangeOtherIntegrations(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(workshopNotFoundResponse))
	}))
	defer upstream.Close()
	client := NewClient(Config{DriverBaseURL: upstream.URL, LambungBaseURL: upstream.URL, RequestTimeout: time.Second})
	_, driverErr := client.ActiveDrivers(context.Background(), "Driver")
	_, unitsErr := client.Units(context.Background(), "", 100)
	for _, err := range []error{driverErr, unitsErr} {
		if err == nil || err.Error() != "integration returned status 404" || errors.Is(err, movement.ErrWorkOrderNotFound) {
			t.Fatalf("Workshop-only decoding changed Driver/Unit Lambung behavior: %v", err)
		}
	}
}
