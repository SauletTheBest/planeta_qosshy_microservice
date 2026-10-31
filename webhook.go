package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Event is what the bank tells the shop. The shop must not trust the buyer's browser
// ("I paid, honestly!"): only this signed server-to-server message marks an order paid.
type Event struct {
	ID                string    `json:"id"`
	Type              string    `json:"type"` // payment.succeeded | payment.failed
	PaymentID         string    `json:"payment_id"`
	MerchantReference string    `json:"merchant_reference"`
	AmountTiyn        int64     `json:"amount_tiyn"`
	Currency          string    `json:"currency"`
	CardLast4         string    `json:"card_last4,omitempty"`
	FailureReason     string    `json:"failure_reason,omitempty"`
	ReceiptURL        string    `json:"receipt_url,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// SignatureHeader carries "t=<unix time>,v1=<hex HMAC-SHA256>".
const SignatureHeader = "Qosshy-Signature"

// sign computes HMAC-SHA256(secret, "<t>.<body>"). The timestamp is inside the signed
// text, so an old captured webhook can't be replayed later with a fresh time.
func sign(secret, body []byte, t time.Time) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%d.", t.Unix())
	mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t.Unix(), hex.EncodeToString(mac.Sum(nil)))
}

type webhookSender struct {
	url    string
	secret []byte
	client *http.Client
	delays []time.Duration // pause before each attempt; the shop may be down for a moment
}

func newWebhookSender(url string, secret []byte) *webhookSender {
	return &webhookSender{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 5 * time.Second},
		delays: []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second},
	}
}

// send delivers the event in the background, retrying with growing pauses until the shop
// answers 2xx. Retrying is safe only because the shop handles duplicates (idempotency).
func (w *webhookSender) send(ev Event) {
	go func() {
		body, err := json.Marshal(ev)
		if err != nil {
			log.Println("webhook: marshal:", err)
			return
		}
		for attempt, delay := range w.delays {
			time.Sleep(delay)
			req, _ := http.NewRequest(http.MethodPost, w.url, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(SignatureHeader, sign(w.secret, body, time.Now())) // signed again on every attempt
			resp, err := w.client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					log.Printf("webhook %s %s delivered (attempt %d)", ev.Type, ev.PaymentID, attempt+1)
					return
				}
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			log.Printf("webhook %s %s attempt %d failed: %v", ev.Type, ev.PaymentID, attempt+1, err)
		}
		log.Printf("webhook %s %s: giving up after %d attempts", ev.Type, ev.PaymentID, len(w.delays))
	}()
}
