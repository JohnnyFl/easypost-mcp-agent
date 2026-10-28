package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.easypost.com/v2"

type Tracker struct {
	ID              string           `json:"id"`
	Mode            string           `json:"mode"`
	TrackingCode    string           `json:"tracking_code"`
	Carrier         string           `json:"carrier"`
	Status          string           `json:"status"`
	EstDeliveryDate *time.Time       `json:"est_delivery_date"`
	TrackingDetails []TrackingDetail `json:"tracking_details"`
}

type TrackingDetail struct {
	Message          string           `json:"message"`
	Status           string           `json:"status"`
	DateTime         time.Time        `json:"datetime"`
	TrackingLocation TrackingLocation `json:"tracking_location"`
}

type TrackingLocation struct {
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
	Zip     string `json:"zip"`
}

type Address struct {
	Name    string `json:"name,omitempty" jsonschema:"recipient or sender name"`
	Street1 string `json:"street1" jsonschema:"street address"`
	City    string `json:"city"`
	State   string `json:"state,omitempty" jsonschema:"two-letter state code for US"`
	Zip     string `json:"zip"`
	Country string `json:"country" jsonschema:"ISO country code, e.g. US"`
	Phone   string `json:"phone,omitempty"`
}

type Parcel struct {
	Length float64 `json:"length" jsonschema:"inches"`
	Width  float64 `json:"width" jsonschema:"inches"`
	Height float64 `json:"height" jsonschema:"inches"`
	Weight float64 `json:"weight" jsonschema:"ounces"`
}

type Rate struct {
	ID           string `json:"id"`
	Carrier      string `json:"carrier"`
	Service      string `json:"service"`
	Rate         string `json:"rate"`
	Currency     string `json:"currency"`
	DeliveryDays *int   `json:"delivery_days"`
}

type Shipment struct {
	ID           string `json:"id"`
	Mode         string `json:"mode"`
	Rates        []Rate `json:"rates"`
	TrackingCode string `json:"tracking_code"`
	PostageLabel *struct {
		LabelURL string `json:"label_url"`
	} `json:"postage_label"`
}

type Client struct {
	BaseURL string
	Key     string
	HTTP    *http.Client
}

func NewClient(key string) *Client {
	return &Client{BaseURL: baseURL, Key: key, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// IsTestKey: EasyPost test keys start with "EZTK", production keys with "EZAK".
func IsTestKey(key string) bool { return strings.HasPrefix(key, "EZTK") }

func (c *Client) do(ctx context.Context, path string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Key, "")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("easypost %s: %d %s", path, resp.StatusCode, data)
	}
	return json.Unmarshal(data, out)
}

func (c *Client) CreateShipment(ctx context.Context, from, to Address, p Parcel) (*Shipment, error) {
	var s Shipment
	body := map[string]any{"shipment": map[string]any{"from_address": from, "to_address": to, "parcel": p}}
	return &s, c.do(ctx, "/shipments", body, &s)
}

func (c *Client) Buy(ctx context.Context, shipmentID, rateID string) (*Shipment, error) {
	var s Shipment
	return &s, c.do(ctx, "/shipments/"+shipmentID+"/buy", map[string]any{"rate": map[string]string{"id": rateID}}, &s)
}

func (c *Client) Track(ctx context.Context, code, carrier string) (*Tracker, error) {
	tr := map[string]string{"tracking_code": code}
	if carrier != "" {
		tr["carrier"] = carrier
	}
	var t Tracker
	return &t, c.do(ctx, "/trackers", map[string]any{"tracker": tr}, &t)
}

// RankRates drops rates slower than maxDays (0 = no limit) or with unknown delivery time when a limit is set,
// then sorts cheapest first, breaking ties by faster delivery.
func RankRates(rates []Rate, maxDays int) []Rate {
	out := make([]Rate, 0, len(rates))
	for _, r := range rates {
		if maxDays > 0 && (r.DeliveryDays == nil || *r.DeliveryDays > maxDays) {
			continue
		}
		out = append(out, r)
	}
	price := func(r Rate) float64 { v, _ := strconv.ParseFloat(r.Rate, 64); return v }
	days := func(r Rate) int {
		if r.DeliveryDays == nil {
			return 1 << 30
		}
		return *r.DeliveryDays
	}
	sort.SliceStable(out, func(i, j int) bool {
		if price(out[i]) != price(out[j]) {
			return price(out[i]) < price(out[j])
		}
		return days(out[i]) < days(out[j])
	})
	return out
}
