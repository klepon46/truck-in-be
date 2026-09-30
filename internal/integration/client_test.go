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
