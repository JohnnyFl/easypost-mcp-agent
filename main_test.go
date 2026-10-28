package main

import "testing"

func days(n int) *int { return &n }

func TestRankRates(t *testing.T) {
	rates := []Rate{
		{ID: "a", Rate: "12.50", DeliveryDays: days(1)},
		{ID: "b", Rate: "7.10", DeliveryDays: days(5)},
		{ID: "c", Rate: "7.10", DeliveryDays: days(3)},
		{ID: "d", Rate: "5.00", DeliveryDays: nil},
	}

	got := RankRates(rates, 0)
	want := []string{"d", "c", "b", "a"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("no limit: got %v at %d, want %s", got[i].ID, i, id)
		}
	}

	got = RankRates(rates, 3) // drops b (5 days) and d (unknown)
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "a" {
		t.Fatalf("max 3 days: got %+v", got)
	}
}

func TestCheckKey(t *testing.T) {
	if checkKey("", false) == nil {
		t.Fatal("empty key must fail")
	}
	if checkKey("EZTKabc", false) != nil {
		t.Fatal("test key must pass")
	}
	if checkKey("EZAKabc", false) == nil {
		t.Fatal("production key must be refused by default")
	}
	if checkKey("EZAKabc", true) != nil {
		t.Fatal("production key must pass when explicitly allowed")
	}
}
