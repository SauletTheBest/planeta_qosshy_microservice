package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	testAPIKey = "test-api-key-0123456789abcdef0123456789"
	testSecret = "test-webhook-secret-0123456789abcdef0123"
)

// fakeShop stands in for the shop: it receives webhooks and checks their signature
// exactly like the real shop must.
type fakeShop struct {
	srv    *httptest.Server
	events chan Event
}

func newFakeShop(t *testing.T) *fakeShop {
	fs := &fakeShop{events: make(chan Event, 10)}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var ts int64
		var sig string
		for _, part := range strings.Split(r.Header.Get(SignatureHeader), ",") {
			k, v, _ := strings.Cut(part, "=")
			switch k {
			case "t":
				fmt.Sscan(v, &ts)
			case "v1":
				sig = v
			}
		}
		mac := hmac.New(sha256.New, []byte(testSecret))
		fmt.Fprintf(mac, "%d.", ts)
		mac.Write(body)
		if !hmac.Equal([]byte(sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
			t.Errorf("webhook signature does not verify")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var ev Event
		json.Unmarshal(body, &ev)
		fs.events <- ev
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (fs *fakeShop) next(t *testing.T) Event {
	select {
	case ev := <-fs.events:
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("no webhook received")
		return Event{}
	}
}

func setup(t *testing.T) (*server, *fakeShop, http.Handler) {
	gin.SetMode(gin.TestMode)
	shop := newFakeShop(t)
	s := newServer(config{
		PublicURL:          "http://bank.test",
		APIKey:             testAPIKey,
		WebhookSecret:      testSecret,
		MerchantWebhookURL: shop.srv.URL + "/payments/webhook",
		MerchantSiteURL:    "http://shop.test",
	})
	s.receiptsDir = t.TempDir()
	s.webhooks.delays = []time.Duration{0}
	return s, shop, s.router()
}

func do(h http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func createBody(amount int64, ref string, expires time.Time) string {
	b, _ := json.Marshal(map[string]any{
		"amount_tiyn": amount, "currency": "KZT", "description": "Planeta Qosshy, заказ №1",
		"merchant_reference": ref, "return_url": "http://shop.test/orders/1/result", "expires_at": expires,
	})
	return string(b)
}

var authJSON = map[string]string{"Authorization": "Bearer " + testAPIKey, "Content-Type": "application/json", "Idempotency-Key": "k1"}

func createPayment(t *testing.T, h http.Handler, key string) map[string]any {
	headers := map[string]string{"Authorization": "Bearer " + testAPIKey, "Content-Type": "application/json", "Idempotency-Key": key}
	rec := do(h, "POST", "/api/v1/payments", createBody(1500050, "57", time.Now().Add(15*time.Minute)), headers)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp
}

func pay(h http.Handler, id, number string) *httptest.ResponseRecorder {
	form := url.Values{"card_number": {number}, "expiry": {"12/30"}, "cvv": {"123"}, "holder": {"IVAN"}}
	return do(h, "POST", "/pay/"+id, form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

func TestAPIRequiresKey(t *testing.T) {
	_, _, h := setup(t)
	rec := do(h, "POST", "/api/v1/payments", createBody(100, "1", time.Now().Add(time.Hour)),
		map[string]string{"Authorization": "Bearer wrong", "Idempotency-Key": "x"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key: got %d", rec.Code)
	}
}

func TestCreateValidation(t *testing.T) {
	_, _, h := setup(t)
	evil := strings.Replace(createBody(100, "1", time.Now().Add(time.Hour)), "http://shop.test", "http://evil.test", 1)
	for name, body := range map[string]string{
		"zero amount":           createBody(0, "1", time.Now().Add(time.Hour)),
		"already expired":       createBody(100, "1", time.Now().Add(-time.Minute)),
		"foreign return_url":    evil,
		"no merchant reference": createBody(100, "", time.Now().Add(time.Hour)),
	} {
		if rec := do(h, "POST", "/api/v1/payments", body, authJSON); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, rec.Code)
		}
	}
}

func TestIdempotentCreate(t *testing.T) {
	_, _, h := setup(t)
	first := createPayment(t, h, "same-key")
	again := createPayment(t, h, "same-key")
	if first["payment_id"] != again["payment_id"] {
		t.Fatalf("retry with the same Idempotency-Key created a second payment")
	}
	other := createBody(999, "57", time.Now().Add(time.Hour)) // same key, different amount
	headers := map[string]string{"Authorization": "Bearer " + testAPIKey, "Idempotency-Key": "same-key"}
	if rec := do(h, "POST", "/api/v1/payments", other, headers); rec.Code != http.StatusConflict {
		t.Fatalf("reused key with other params: got %d, want 409", rec.Code)
	}
}

func TestSuccessfulPayment(t *testing.T) {
	s, shop, h := setup(t)
	id := createPayment(t, h, "ok")["payment_id"].(string)

	if rec := do(h, "GET", "/pay/"+id, "", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "15 000,50") {
		t.Fatalf("payment page: %d", rec.Code)
	}
	if rec := pay(h, id, "4242 4242 4242 4241"); rec.Code != http.StatusBadRequest {
		t.Fatalf("typo in card number: got %d, want 400", rec.Code)
	}
	if p, _ := s.store.get(id); p.Status != StatusPending {
		t.Fatalf("a typo must not fail the payment, status %s", p.Status)
	}

	rec := pay(h, id, "4242 4242 4242 4242")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "http://shop.test/orders/1/result" {
		t.Fatalf("pay: %d → %s", rec.Code, rec.Header().Get("Location"))
	}
	ev := shop.next(t)
	if ev.Type != "payment.succeeded" || ev.PaymentID != id || ev.MerchantReference != "57" ||
		ev.AmountTiyn != 1500050 || ev.CardLast4 != "4242" || ev.ReceiptURL != "http://bank.test/receipts/"+id {
		t.Fatalf("unexpected event %+v", ev)
	}

	if rec := do(h, "GET", "/receipts/"+id, "", nil); rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "%PDF") {
		t.Fatalf("receipt: %d", rec.Code)
	}
	if rec := pay(h, id, "4242 4242 4242 4242"); rec.Code != 200 { // double submit shows the result
		t.Fatalf("second submit: %d", rec.Code)
	}
	select {
	case ev := <-shop.events:
		t.Fatalf("double submit sent a second webhook: %+v", ev)
	case <-time.After(300 * time.Millisecond):
	}

	// The PDF content is compressed, so searching it proves little. What matters is that
	// the full number is never stored, so neither the receipt nor anything else can leak it.
	if p, _ := s.store.get(id); strings.Contains(fmt.Sprintf("%+v", p), "4242424242424242") {
		t.Fatal("the full card number is stored")
	}
}

func TestDeclinedPayment(t *testing.T) {
	s, shop, h := setup(t)
	id := createPayment(t, h, "declined")["payment_id"].(string)
	pay(h, id, "4000 0000 0000 0002")
	if ev := shop.next(t); ev.Type != "payment.failed" || ev.FailureReason != "card_declined" {
		t.Fatalf("unexpected event %+v", ev)
	}
	if p, _ := s.store.get(id); p.Status != StatusFailed {
		t.Fatalf("status %s", p.Status)
	}
}

func TestExpiredSession(t *testing.T) {
	s, _, h := setup(t)
	id := createPayment(t, h, "late")["payment_id"].(string)
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	if rec := pay(h, id, "4242 4242 4242 4242"); rec.Code != http.StatusGone {
		t.Fatalf("paying an expired session: got %d, want 410", rec.Code)
	}
}

func TestReceiptPathTraversal(t *testing.T) {
	_, _, h := setup(t)
	for _, id := range []string{"..%2F..%2Fgo.mod", "pay_../../x", "pay_123"} {
		if rec := do(h, "GET", "/receipts/"+id, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("/receipts/%s: got %d", id, rec.Code)
		}
	}
}
