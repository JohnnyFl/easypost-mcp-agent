// easypost-mcp: MCP server exposing EasyPost shipping rates and label purchase.
//
//	EASYPOST_API_KEY=EZTK... go run .              # stdio (Claude Desktop, local agents)
//	EASYPOST_API_KEY=EZTK... go run . -http :8080  # streamable HTTP (deploy)
//
// Production keys are refused unless ALLOW_PRODUCTION=1: buying a label with one spends real money.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RatesIn struct {
	From    Address `json:"from"`
	To      Address `json:"to"`
	Parcel  Parcel  `json:"parcel"`
	MaxDays int     `json:"max_days,omitempty" jsonschema:"only return rates delivered within this many days; 0 = any"`
}

type RatesOut struct {
	ShipmentID string `json:"shipment_id"`
	Mode       string `json:"mode" jsonschema:"test or production"`
	Rates      []Rate `json:"rates" jsonschema:"cheapest first"`
}

type BuyIn struct {
	ShipmentID string `json:"shipment_id"`
	RateID     string `json:"rate_id"`
}

type BuyOut struct {
	TrackingCode string `json:"tracking_code"`
	LabelURL     string `json:"label_url"`
	Mode         string `json:"mode"`
}

type TrackIn struct {
	TrackingCode string `json:"tracking_code" jsonschema:"The shipment tracking code given by the user, e.g EZ4000000004"`
	Carrier      string `json:"carrier,omitempty" jsonschema:"Carrier, e.g. USPS or UPS. Omit if unknown"`
}

type TrackEvent struct {
	Time     time.Time `json:"time" jsonschema:"When the event happened. In RFC 3339 format, carrier's time"`
	Status   string    `json:"status" jsonschema:"Shipment status: pre_transit, in_transit, out_for_delivery, delivered, available_for_pickup, return_to_sender, failure or unknown"`
	Message  string    `json:"message" jsonschema:"Carrier's description of the event"`
	Location string    `json:"location" jsonschema:"Event location as 'CITY, STATE'; may be empty"`
}

type TrackOut struct {
	TrackingCode    string       `json:"tracking_code" jsonschema:"The shipment tracking code given by the user, e.g EZ4000000004"`
	Status          string       `json:"status" jsonschema:"Shipment status: pre_transit, in_transit, out_for_delivery, delivered, available_for_pickup, return_to_sender, failure or unknown"`
	EstDeliveryDate string       `json:"est_delivery_date,omitempty" jsonschema:"Estimated delivery date (RFC 3339); empty if unknown"`
	Mode            string       `json:"mode" jsonschema:"test = fake EasyPost shipment, production = real"`
	Events          []TrackEvent `json:"events" jsonschema:"Up to 5 latest tracking events, newest first"`
}

func newServer(c *Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "easypost-mcp", Version: "0.1.0"}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_rates",
		Description: "Create an EasyPost shipment and return available carrier rates, cheapest first. Does not buy anything.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RatesIn) (*mcp.CallToolResult, RatesOut, error) {
		sh, err := c.CreateShipment(ctx, in.From, in.To, in.Parcel)
		if err != nil {
			return nil, RatesOut{}, err
		}
		return nil, RatesOut{ShipmentID: sh.ID, Mode: sh.Mode, Rates: RankRates(sh.Rates, in.MaxDays)}, nil
	})

	destructive := true
	mcp.AddTool(s, &mcp.Tool{
		Name: "buy_label",
		Description: "Buy a shipping label for a shipment at the given rate. Spends money in production mode. " +
			"Only call after a human has explicitly approved this exact carrier, service and price.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in BuyIn) (*mcp.CallToolResult, BuyOut, error) {
		sh, err := c.Buy(ctx, in.ShipmentID, in.RateID)
		if err != nil {
			return nil, BuyOut{}, err
		}
		out := BuyOut{TrackingCode: sh.TrackingCode, Mode: sh.Mode}
		if sh.PostageLabel != nil {
			out.LabelURL = sh.PostageLabel.LabelURL
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "track_shipment",
		Description: "Get shipment tracking status and last events. Call when users ask where is a shipment or if it's delivered. " +
			"But only when user has tracking code for shipments. Does not buy or change anything. " +
			"Test codes can be similar to these EZ1000000001, EZ2000000002, EZ4000000004, EZ6000000006",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input TrackIn) (*mcp.CallToolResult, TrackOut, error) {
		tr, err := c.Track(ctx, input.TrackingCode, input.Carrier)
		if err != nil {
			return nil, TrackOut{}, err
		}
		var formatedDate string
		if tr.EstDeliveryDate != nil {
			formatedDate = tr.EstDeliveryDate.Format(time.RFC3339)
		}

		sorted := slices.SortedFunc(slices.Values(tr.TrackingDetails), func(a, b TrackingDetail) int {
			return b.DateTime.Compare(a.DateTime)
		})

		events := make([]TrackEvent, 0, 5)
		sorted = sorted[:min(5, len(sorted))]
		for idx, trDetail := range sorted {
			if idx < 5 {
				events = append(events, TrackEvent{
					Time:    trDetail.DateTime,
					Status:  trDetail.Status,
					Message: trDetail.Message,
					Location: fmt.Sprintf("%s, %s, %s, %s", trDetail.TrackingLocation.City,
						trDetail.TrackingLocation.State,
						trDetail.TrackingLocation.Country,
						trDetail.TrackingLocation.Zip),
				})
			}
		}
		out := TrackOut{TrackingCode: tr.TrackingCode, Mode: tr.Mode, Status: tr.Status, EstDeliveryDate: formatedDate, Events: events}
		return nil, out, nil

	})

	return s

}

func checkKey(key string, allowProd bool) error {
	if key == "" {
		return errors.New("EASYPOST_API_KEY is not set")
	}
	if !IsTestKey(key) && !allowProd {
		return errors.New("refusing a non-test EasyPost key; set ALLOW_PRODUCTION=1 to use production")
	}
	return nil
}

func newClientFromEnv(key string) *Client {
	c := NewClient(key)
	if u := os.Getenv("EASYPOST_BASE_URL"); u != "" {
		c.BaseURL = os.Getenv("EASYPOST_BASE_URL")
	}
	return c
}

func main() {
	addr := flag.String("http", "", "serve streamable HTTP on this address instead of stdio, e.g. :8080")
	flag.Parse()

	key := os.Getenv("EASYPOST_API_KEY")
	if err := checkKey(key, os.Getenv("ALLOW_PRODUCTION") == "1"); err != nil {
		log.Fatal(err)
	}
	server := newServer(newClientFromEnv(key))

	if *addr == "" {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
		return
	}
	// ponytail: no auth on the HTTP endpoint; put it behind an API key or IAM before exposing publicly.
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, handler))
}
