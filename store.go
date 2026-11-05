package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Qosshy Pay is a TEST acquirer: it plays the role a real provider (a bank, CloudPayments,
// Stripe…) plays for the shop. Payments live in memory: a restart forgets them, which is
// fine for a test bank. What matters is that the shop talks to it exactly like to a real one.

type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusRefunded  Status = "refunded" // the money went back to the card
)

type Payment struct {
	ID                string
	AmountTiyn        int64
	Currency          string
	Description       string
	MerchantReference string // the shop's own payment ID, sent back in every webhook
	CustomerEmail     string
	ReturnURL         string // where the buyer goes after paying
	ExpiresAt         time.Time
	CreatedAt         time.Time

	Status        Status
	CardLast4     string // the only part of the card that is kept
	FailureReason string
	ReceiptURL    string
	RefundedAt    time.Time // zero unless refunded
}

var (
	ErrNotFound      = errors.New("payment not found")
	ErrNotPending    = errors.New("payment is already completed")
	ErrExpired       = errors.New("payment session has expired")
	ErrKeyReused     = errors.New("idempotency key reused with different parameters")
	ErrNotRefundable = errors.New("only a succeeded payment can be refunded")
)

// refund gives the whole amount back. A second call for the same payment changes
// nothing and reports already == true: the shop may retry after a timeout without
// the customer getting the money twice.
func (s *store) refund(id string, now time.Time) (p Payment, already bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.byID[id]
	switch {
	case !ok:
		return Payment{}, false, ErrNotFound
	case stored.Status == StatusRefunded:
		return *stored, true, nil
	case stored.Status != StatusSucceeded:
		return *stored, false, ErrNotRefundable
	}
	stored.Status = StatusRefunded
	stored.RefundedAt = now
	return *stored, false, nil
}

type store struct {
	mu        sync.Mutex // every field below is used by many request goroutines at once
	byID      map[string]*Payment
	byIdemKey map[string]string // Idempotency-Key -> payment ID
}

func newStore() *store {
	return &store{byID: map[string]*Payment{}, byIdemKey: map[string]string{}}
}

// create stores a new pending payment. If the same Idempotency-Key was used before,
// the earlier payment is returned instead (created == false): a client that retries
// after a timeout must not end up with two payments.
func (s *store) create(idemKey string, p Payment) (saved Payment, created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.byIdemKey[idemKey]; ok {
		old := s.byID[id]
		if old.AmountTiyn != p.AmountTiyn || old.MerchantReference != p.MerchantReference {
			return Payment{}, false, ErrKeyReused
		}
		return *old, false, nil
	}

	p.ID = newID("pay_")
	p.Status = StatusPending
	s.byID[p.ID] = &p
	s.byIdemKey[idemKey] = p.ID
	return p, true, nil
}

// get returns a copy, so callers can't change the stored payment without the lock.
func (s *store) get(id string) (Payment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return Payment{}, false
	}
	return *p, true
}

// complete moves a pending payment to its final status exactly once. A second submit
// of the same form (double click, back button) gets ErrNotPending and changes nothing.
func (s *store) complete(id string, status Status, cardLast4, reason string, now time.Time) (Payment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.byID[id]
	switch {
	case !ok:
		return Payment{}, ErrNotFound
	case p.Status != StatusPending:
		return *p, ErrNotPending
	case now.After(p.ExpiresAt):
		return *p, ErrExpired
	}

	p.Status = status
	p.CardLast4 = cardLast4
	p.FailureReason = reason
	return *p, nil
}

func (s *store) setReceiptURL(id, url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.byID[id]; ok {
		p.ReceiptURL = url
	}
}

// newID makes an unguessable ID: knowing one payment's ID must not let anyone find others.
func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failing means the OS is broken; nothing sensible to do
	}
	return prefix + hex.EncodeToString(b)
}
