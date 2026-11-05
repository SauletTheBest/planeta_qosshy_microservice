package main

import (
	"path/filepath"
	"testing"
	"time"
)

// A restart must not forget payments: they are reloaded from DATA_FILE.
func TestStoreSurvivesRestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "payments.json")
	now := time.Now()

	s, err := openStore(file)
	if err != nil {
		t.Fatal(err)
	}
	p, _, _ := s.create("key-1", Payment{AmountTiyn: 500, MerchantReference: "9", ExpiresAt: now.Add(time.Hour)})
	if _, err := s.complete(p.ID, StatusSucceeded, "4242", "", now); err != nil {
		t.Fatal(err)
	}

	again, err := openStore(file) // "the process restarted"
	if err != nil {
		t.Fatal(err)
	}
	got, ok := again.get(p.ID)
	if !ok || got.Status != StatusSucceeded || got.CardLast4 != "4242" {
		t.Fatalf("after restart: %+v (found %v)", got, ok)
	}
	// The idempotency key is remembered too: a retried create gets the same payment.
	same, created, _ := again.create("key-1", Payment{AmountTiyn: 500, MerchantReference: "9"})
	if created || same.ID != p.ID {
		t.Fatal("the idempotency key was forgotten after a restart")
	}
	if _, _, err := again.refund(p.ID, now); err != nil {
		t.Fatalf("refund of a payment made before the restart: %v", err)
	}
}

func TestOpenStoreWithoutFile(t *testing.T) {
	s, err := openStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || len(s.byID) != 0 {
		t.Fatalf("first start: %v, %d payments", err, len(s.byID))
	}
}
