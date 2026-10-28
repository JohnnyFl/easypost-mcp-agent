package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func createFakeServer(t *testing.T) *httptest.Server {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/trackers" {
			w.WriteHeader(http.StatusBadRequest)
			t.Errorf("Bad Request: %s %s", r.Method, r.URL.Path)
			return
		}
		var tr map[string]map[string]string
		err := json.NewDecoder(r.Body).Decode(&tr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			t.Errorf("Error decoding body: %s", err)
			return
		}
		if tr["tracker"]["tracking_code"] == "EZ4000000004" {
			w.Write([]byte(`
				{
					"id": "trk_123",
					"mode": "test",
					"tracking_code": "EZ4000000004",
					"carrier": "USPS",
					"status": "delivered",
					"est_delivery_date": null,
					"tracking_details": [
						{
						  "message": "Delivered",
						  "status": "delivered",
						  "datetime": "2026-10-01T12:00:00Z",
						  "tracking_location": { "city": "SAN FRANCISCO", "state": "CA", "country": "US", "zip": "94107" }
						},
						{
						  "message": "Message out for Delivery",
						  "status": "out_for_delivery",
						  "datetime": "2026-09-01T12:00:00Z",
						  "tracking_location": { "city": "SAN FRANCISCO", "state": "CA", "country": "US", "zip": "94107" }
						}
					]
				}`))
		}

		if tr["tracker"]["tracking_code"] == "EZ4000000001" {
			w.Write([]byte(`
				{
					"id": "trk_123",
					"mode": "test",
					"tracking_code": "EZ4000000001",
					"carrier": "USPS",
					"status": "delivered",
					"est_delivery_date": null,
					"tracking_details": []
				}`))
		}

	}))
	return fake
}

func TestTrack(t *testing.T) {
	fake := createFakeServer(t)

	defer fake.Close()

	c := NewClient("EZTKtest")
	c.BaseURL = fake.URL
	tr, err := c.Track(context.Background(), "EZ4000000004", "USPS")

	if err != nil || tr.Status != "delivered" || len(tr.TrackingDetails) < 2 {
		t.Fatalf("error: %v, %v", err, tr)
	}

	if tr.TrackingDetails[0].DateTime.IsZero() {
		t.Errorf("Tracking datetime is zero")
	}

	tr, err = c.Track(context.Background(), "EZ4000000001", "USPS")
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	if tr.TrackingDetails == nil || len(tr.TrackingDetails) > 0 {
		t.Errorf("Tracking details is empty")
	}

}

func TestTrackShipmentOverMCP(t *testing.T) {
	fake := createFakeServer(t)
	defer fake.Close()
	c := NewClient("EZTKtest")
	c.BaseURL = fake.URL

	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := newServer(c).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "track_shipment", Arguments: map[string]any{"tracking_code": "EZ4000000004"}})
	if err != nil {
		t.Fatal(err)
	}

	if res.IsError {
		t.Errorf("Error should be thrown %v", res.GetError())
	}

	m, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content type %T", res.StructuredContent)
	}

	if m["status"] != "delivered" {
		t.Errorf("status = %v, want delivered", m["status"])
	}

	// No events: the client must get "events": [], not null.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "track_shipment", Arguments: map[string]any{"tracking_code": "EZ4000000001"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool returned an error: %v", res.Content)
	}
	m, ok = res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("unexpected structured content type %T", res.StructuredContent)
	}
	events, ok := m["events"].([]any)
	if !ok || len(events) != 0 {
		t.Errorf("events = %#v, want empty []", m["events"])
	}
}
