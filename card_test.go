package main

import (
	"testing"
	"time"
)

func TestLuhn(t *testing.T) {
	for _, tc := range []struct {
		number string
		valid  bool
	}{
		{"4242424242424242", true},
		{"4000000000000002", true},
		{"4242424242424241", false}, // last digit changed
		{"4242424242422442", false}, // two neighbours swapped
		{"79927398713", true},       // the textbook example
	} {
		if got := luhnValid(tc.number); got != tc.valid {
			t.Errorf("luhnValid(%s) = %v, want %v", tc.number, got, tc.valid)
		}
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in    string
		valid bool
	}{
		{"10/26", true}, // valid until the end of October
		{"09/26", false},
		{"12/30", true},
		{"13/30", false},
		{"1/30", false},
		{"1230", false},
	} {
		if got := expiryValid(tc.in, now); got != tc.valid {
			t.Errorf("expiryValid(%q) = %v, want %v", tc.in, got, tc.valid)
		}
	}
}

func TestValidateCard(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	card, msg := validateCard("4242 4242 4242 4242", "12/30", "123", "IVAN", now)
	if msg != "" || card.Last4 != "4242" || card.Number != "4242424242424242" {
		t.Fatalf("valid card rejected: %+v %q", card, msg)
	}
	for _, bad := range [][4]string{
		{"4242 4242 4242 4241", "12/30", "123", "IVAN"}, // Luhn
		{"4242 4242 4242 4242", "01/20", "123", "IVAN"}, // expired
		{"4242 4242 4242 4242", "12/30", "12", "IVAN"},  // short CVV
		{"4242 4242 4242 4242", "12/30", "123", " "},    // no holder
	} {
		if _, msg := validateCard(bad[0], bad[1], bad[2], bad[3], now); msg == "" {
			t.Errorf("validateCard(%q) accepted invalid input", bad)
		}
	}
}

func TestFormatTenge(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 1500000: "15 000", 1500050: "15 000,50", 1: "0,01"} {
		if got := formatTenge(in); got != want {
			t.Errorf("formatTenge(%d) = %q, want %q", in, got, want)
		}
	}
}
