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

const actualSPPResponse = `{
	"success": true, "message": "Success", "code": 200,
	"data": [{
		"trans_no": "SPG-BJM-2612481",
		"unit_police_no": "DA 8729 CB",
		"driver_name": "JOYO LUMBANRAJA ",
		"unit_capacity": "10000", "unit_name": "SAMT072",
		"customer_code": "C400", "customer_name": "PT KAPUAS MAJU JAYA",
		"qty": 10000, "is_marked": false
	}]
}`

func TestSPPUsesConfirmedSAOSContract(t *testing.T) {
	for _, number := range []string{"SPG-BJM-2612481", "  SPG/BJM/2612481  "} {
		t.Run(number, func(t *testing.T) {
			wantNumber := strings.TrimSpace(number)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/loading-orders" || r.URL.Query().Get("transNo") != wantNumber {
					t.Errorf("unexpected request method, path, or complete document number")
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Error("SAOS request must explicitly accept JSON")
				}
				if r.Header.Get("Authorization") != "Bearer configured-saos-token" {
					t.Error("SAOS request must use the configured service token")
				}
				_, _ = w.Write([]byte(strings.ReplaceAll(actualSPPResponse, "SPG-BJM-2612481", wantNumber)))
			}))
			defer server.Close()
			client := NewClient(Config{SAOSBaseURL: server.URL, SAOSToken: "configured-saos-token", RequestTimeout: time.Second})
			document, err := client.SPP(context.Background(), number)
			if err != nil {
				t.Fatalf("SPP() error = %v", err)
			}
			if document.NoLambung != "SAMT072" || document.NoPolisi != "DA 8729 CB" || document.DriverName != "JOYO LUMBANRAJA" || document.CustomerID != "C400" || document.CustomerName != "PT KAPUAS MAJU JAYA" {
				t.Fatal("SPP() did not map the confirmed SAOS record")
			}
		})
	}
}

func TestSPPSelectsExactMatchAfterUnrelatedRecord(t *testing.T) {
	response := strings.Replace(actualSPPResponse, `"data": [{`, `"data": [{"trans_no":"OTHER-SPP"}, {`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	client := NewClient(Config{SAOSBaseURL: server.URL, RequestTimeout: time.Second})
	document, err := client.SPP(context.Background(), "SPG-BJM-2612481")
	if err != nil || document.NoLambung != "SAMT072" {
		t.Fatalf("SPP() did not select the exact matching record: %v", err)
	}
}

func TestSPPRejectsInvalidRecords(t *testing.T) {
	for name, response := range map[string]string{
		"empty array":        `{"success":true,"data":[]}`,
		"no exact match":     strings.ReplaceAll(actualSPPResponse, "SPG-BJM-2612481", "SPG-BJM-26124810"),
		"missing success":    strings.ReplaceAll(actualSPPResponse, `"success": true,`, ``),
		"unsuccessful":       strings.ReplaceAll(actualSPPResponse, `"success": true`, `"success": false`),
		"missing unit":       strings.ReplaceAll(actualSPPResponse, `"SAMT072"`, `"  "`),
		"missing police":     strings.ReplaceAll(actualSPPResponse, `"DA 8729 CB"`, `""`),
		"missing driver":     strings.ReplaceAll(actualSPPResponse, `"JOYO LUMBANRAJA "`, `"  "`),
		"null data":          `{"success":true,"data":null}`,
		"object not array":   `{"success":true,"data":{"trans_no":"SPG-BJM-2612481"}}`,
		"malformed response": `{"success":true,"data":[`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			client := NewClient(Config{SAOSBaseURL: server.URL, RequestTimeout: time.Second})
			if _, err := client.SPP(context.Background(), "SPG-BJM-2612481"); err == nil {
				t.Fatal("SPP() accepted an invalid response")
			}
		})
	}
}

func TestSPPConnectionFailureIsNotDocumentValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	client := NewClient(Config{SAOSBaseURL: server.URL, SAOSToken: "secret-service-token", RequestTimeout: time.Second})
	_, err := client.SPP(context.Background(), "SPG-BJM-2612481")
	if !errors.Is(err, movement.ErrIntegrationUnavailable) {
		t.Fatalf("SPP() connection error must be an infrastructure failure: %v", err)
	}
	if strings.Contains(err.Error(), "secret-service-token") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("SPP() error exposed credentials or the upstream URL")
	}
}
