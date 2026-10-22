package integration

import (
	"context"
	"encoding/json"
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
	for _, test := range []struct{ number, id, owner string }{
		{"123-example", "123", "example"},
		{"  123 - Example-East & Co/+?=中  ", "123", "Example-East & Co/+?=中"},
	} {
		t.Run(test.number, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if r.Method != http.MethodGet || r.URL.Path != "/api/v2/loading-orders" || len(query) != 2 || query.Get("id") != test.id || query.Get("owner") != test.owner {
					t.Error("SAOS must receive exactly the separately encoded ID/owner, preserving case and owner hyphens")
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Error("SAOS request must explicitly accept JSON")
				}
				if r.Header.Get("Authorization") != "Bearer configured-saos-token" {
					t.Error("SAOS request must use the configured service token")
				}
				_, _ = w.Write([]byte(actualSPPResponse))
			}))
			defer server.Close()
			client := NewClient(Config{SAOSBaseURL: server.URL, SAOSToken: "configured-saos-token", RequestTimeout: time.Second})
			document, err := client.SPP(context.Background(), test.number)
			if err != nil {
				t.Fatalf("SPP() error = %v", err)
			}
			if document.SPPNumber != "SPG-BJM-2612481" || document.NoLambung != "SAMT072" || document.NoPolisi != "DA 8729 CB" || document.DriverName != "JOYO LUMBANRAJA" || document.CustomerID != "C400" || document.CustomerName != "PT KAPUAS MAJU JAYA" {
				t.Fatal("SPP() did not map the confirmed SAOS record")
			}
		})
	}
}

func TestSPPRejectsConflictingRecords(t *testing.T) {
	response := strings.Replace(actualSPPResponse, `"data": [{`, `"data": [{"trans_no":"OTHER-SPP"}, {`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	client := NewClient(Config{SAOSBaseURL: server.URL, RequestTimeout: time.Second})
	_, err := client.SPP(context.Background(), "123-example")
	if !errors.Is(err, movement.ErrExternalDocument) {
		t.Fatalf("SPP() must not guess among conflicting results: %v", err)
	}
}

func TestSPPRejectsInvalidRecords(t *testing.T) {
	for name, response := range map[string]string{
		"empty array":        `{"success":true,"data":[]}`,
		"missing number":     strings.ReplaceAll(actualSPPResponse, "SPG-BJM-2612481", ""),
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
			if _, err := client.SPP(context.Background(), "123-example"); err == nil {
				t.Fatal("SPP() accepted an invalid response")
			}
		})
	}
}

func TestSPPConnectionFailureIsNotDocumentValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	client := NewClient(Config{SAOSBaseURL: server.URL, SAOSToken: "secret-service-token", RequestTimeout: time.Second})
	_, err := client.SPP(context.Background(), "123-example")
	if !errors.Is(err, movement.ErrIntegrationUnavailable) {
		t.Fatalf("SPP() connection error must be an infrastructure failure: %v", err)
	}
	if strings.Contains(err.Error(), "secret-service-token") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("SPP() error exposed credentials or the upstream URL")
	}
}

func TestSPPReferenceRejectsMalformedInputBeforeCallingUpstream(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	client := NewClient(Config{SAOSBaseURL: server.URL, RequestTimeout: time.Second})
	for _, number := range []string{"", " ", "123", "-example", "123-", "123-  ", " -example"} {
		if _, err := client.SPP(context.Background(), number); !errors.Is(err, movement.ErrInvalidInput) {
			t.Errorf("malformed reference must be input validation, not absence or integration failure: %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("malformed references must not call SAOS")
	}
}

func TestSPPCollapsesOnlyConsistentDocumentProjections(t *testing.T) {
	for _, test := range []struct {
		name, second string
		valid        bool
	}{
		{"duplicate projection", actualSPPResponse, true},
		{"different ignored quantity", strings.ReplaceAll(actualSPPResponse, `"qty": 10000`, `"qty": 500`), true},
		{"different canonical number", strings.ReplaceAll(actualSPPResponse, "SPG-BJM-2612481", "OTHER-SPP"), false},
		{"different customer", strings.ReplaceAll(actualSPPResponse, "C400", "C401"), false},
		{"different police", strings.ReplaceAll(actualSPPResponse, "DA 8729 CB", "DA 0000 XX"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var first, second struct {
				Data []json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal([]byte(actualSPPResponse), &first); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.second), &second); err != nil {
				t.Fatal(err)
			}
			body := `{"success":true,"data":[` + string(first.Data[0]) + `,` + string(second.Data[0]) + `]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			client := NewClient(Config{SAOSBaseURL: server.URL, RequestTimeout: time.Second})
			document, err := client.SPP(context.Background(), "123-example")
			if test.valid {
				if err != nil || document.SPPNumber != "SPG-BJM-2612481" {
					t.Fatalf("consistent projections should resolve: %v", err)
				}
			} else if !errors.Is(err, movement.ErrExternalDocument) {
				t.Fatalf("contradictory document projections must fail closed: %v", err)
			}
		})
	}
}
