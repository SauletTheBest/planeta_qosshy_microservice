package main

import (
	"crypto/subtle"
	"embed"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed templates/*.html
var templatesFS embed.FS

type config struct {
	Port               string
	PublicURL          string // how buyers' browsers reach this service
	APIKey             string // the shop proves who it is with this (Authorization: Bearer …)
	WebhookSecret      string // this service signs webhooks with it, the shop checks the signature
	MerchantWebhookURL string // where events are sent; fixed here, not taken from requests
	MerchantSiteURL    string // return_url must start with it
}

func loadConfig() config {
	cfg := config{
		Port:               envOr("PORT", "8081"),
		PublicURL:          strings.TrimRight(envOr("PUBLIC_URL", "http://localhost:8081"), "/"),
		APIKey:             os.Getenv("API_KEY"),
		WebhookSecret:      os.Getenv("WEBHOOK_SECRET"),
		MerchantWebhookURL: os.Getenv("MERCHANT_WEBHOOK_URL"),
		MerchantSiteURL:    strings.TrimRight(os.Getenv("MERCHANT_SITE_URL"), "/"),
	}
	if len(cfg.APIKey) < 32 || len(cfg.WebhookSecret) < 32 {
		log.Fatal("API_KEY and WEBHOOK_SECRET must be set and at least 32 characters long")
	}
	if cfg.MerchantWebhookURL == "" || cfg.MerchantSiteURL == "" {
		log.Fatal("MERCHANT_WEBHOOK_URL and MERCHANT_SITE_URL must be set")
	}
	return cfg
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type server struct {
	cfg         config
	store       *store
	webhooks    *webhookSender
	receiptsDir string
	now         func() time.Time // replaced in tests
}

func newServer(cfg config) *server {
	return &server{
		cfg:         cfg,
		store:       newStore(),
		webhooks:    newWebhookSender(cfg.MerchantWebhookURL, []byte(cfg.WebhookSecret)),
		receiptsDir: "receipts",
		now:         time.Now,
	}
}

func main() {
	cfg := loadConfig()
	log.Printf("Qosshy Pay (test bank) listening on :%s, public URL %s", cfg.Port, cfg.PublicURL)
	if err := newServer(cfg).router().Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}

func (s *server) router() *gin.Engine {
	r := gin.Default()
	r.SetHTMLTemplate(template.Must(
		template.New("").Funcs(template.FuncMap{"tenge": formatTenge}).ParseFS(templatesFS, "templates/*.html")))

	// Server-to-server API for the shop.
	api := r.Group("/api/v1", s.requireAPIKey)
	api.POST("/payments", s.createPayment)

	// Pages for the buyer's browser.
	r.GET("/pay/:id", s.payPage)
	r.POST("/pay/:id", s.submitPayment)
	r.GET("/receipts/:id", s.receipt)
	return r
}

func (s *server) requireAPIKey(c *gin.Context) {
	got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	// ConstantTimeCompare takes the same time no matter where the strings differ,
	// so an attacker can't guess the key character by character from response times.
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.APIKey)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
		return
	}
	c.Next()
}

type createRequest struct {
	AmountTiyn        int64     `json:"amount_tiyn"`
	Currency          string    `json:"currency"`
	Description       string    `json:"description"`
	MerchantReference string    `json:"merchant_reference"`
	CustomerEmail     string    `json:"customer_email"`
	ReturnURL         string    `json:"return_url"`
	ExpiresAt         time.Time `json:"expires_at"`
}

func (s *server) createPayment(c *gin.Context) {
	key := c.GetHeader("Idempotency-Key")
	if key == "" || len(key) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key header is required"})
		return
	}

	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}

	now := s.now()
	var problem string
	switch {
	case req.AmountTiyn <= 0:
		problem = "amount_tiyn must be positive"
	case req.Currency != "KZT":
		problem = "only KZT is supported"
	case req.MerchantReference == "" || len(req.MerchantReference) > 64:
		problem = "merchant_reference is required (max 64 characters)"
	case len(req.Description) > 200:
		problem = "description is too long"
	case !strings.HasPrefix(req.ReturnURL, s.cfg.MerchantSiteURL+"/"):
		// otherwise a leaked API key could send buyers to any site after paying
		problem = "return_url must point to the merchant site"
	case !req.ExpiresAt.After(now) || req.ExpiresAt.After(now.Add(24*time.Hour)):
		problem = "expires_at must be in the next 24 hours"
	}
	if problem != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": problem})
		return
	}

	p, created, err := s.store.create(key, Payment{
		AmountTiyn:        req.AmountTiyn,
		Currency:          req.Currency,
		Description:       req.Description,
		MerchantReference: req.MerchantReference,
		CustomerEmail:     req.CustomerEmail,
		ReturnURL:         req.ReturnURL,
		ExpiresAt:         req.ExpiresAt,
		CreatedAt:         now,
	})
	if errors.Is(err, ErrKeyReused) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusCreated
	if !created {
		status = http.StatusOK // the same request repeated: same payment, nothing new created
	}
	c.JSON(status, gin.H{
		"payment_id":  p.ID,
		"payment_url": s.cfg.PublicURL + "/pay/" + p.ID,
		"status":      p.Status,
		"expires_at":  p.ExpiresAt,
	})
}

func (s *server) payPage(c *gin.Context) {
	p, ok := s.store.get(c.Param("id"))
	switch {
	case !ok:
		c.HTML(http.StatusNotFound, "done.html", gin.H{"title": "Платёж не найден"})
	case p.Status != StatusPending:
		s.renderDone(c, p)
	case s.now().After(p.ExpiresAt):
		s.renderExpired(c, p)
	default:
		c.HTML(http.StatusOK, "pay.html", gin.H{"p": p})
	}
}

func (s *server) submitPayment(c *gin.Context) {
	now := s.now()
	p, ok := s.store.get(c.Param("id"))
	if !ok {
		c.HTML(http.StatusNotFound, "done.html", gin.H{"title": "Платёж не найден"})
		return
	}
	if p.Status != StatusPending {
		s.renderDone(c, p)
		return
	}

	card, msg := validateCard(c.PostForm("card_number"), c.PostForm("expiry"), c.PostForm("cvv"), c.PostForm("holder"), now)
	if msg != "" {
		// A typo is not a failed payment: the buyer fixes it and tries again.
		c.HTML(http.StatusBadRequest, "pay.html", gin.H{"p": p, "error": msg})
		return
	}

	status, reason := StatusSucceeded, ""
	if r, declined := testDeclines[card.Number]; declined {
		status, reason = StatusFailed, r
	}

	p, err := s.store.complete(p.ID, status, card.Last4, reason, now)
	switch {
	case errors.Is(err, ErrNotPending): // the form was submitted twice
		s.renderDone(c, p)
		return
	case errors.Is(err, ErrExpired):
		s.renderExpired(c, p)
		return
	case err != nil:
		c.HTML(http.StatusNotFound, "done.html", gin.H{"title": "Платёж не найден"})
		return
	}

	ev := Event{
		ID:                newID("evt_"),
		Type:              "payment.failed",
		PaymentID:         p.ID,
		MerchantReference: p.MerchantReference,
		AmountTiyn:        p.AmountTiyn,
		Currency:          p.Currency,
		CardLast4:         p.CardLast4,
		FailureReason:     p.FailureReason,
		CreatedAt:         now,
	}
	if p.Status == StatusSucceeded {
		ev.Type = "payment.succeeded"
		if path, err := writeReceipt(s.receiptsDir, p, now); err != nil {
			log.Println("receipt:", err) // the payment is done anyway; a missing PDF must not undo it
		} else {
			ev.ReceiptURL = s.cfg.PublicURL + "/receipts/" + p.ID
			s.store.setReceiptURL(p.ID, ev.ReceiptURL)
			if p.CustomerEmail != "" {
				go func() {
					if err := SendReceiptEmail(p.CustomerEmail, p, path); err != nil {
						log.Println("receipt email:", err)
					}
				}()
			}
		}
	}
	s.webhooks.send(ev)

	c.Redirect(http.StatusSeeOther, p.ReturnURL)
}

var failureText = map[string]string{
	"card_declined":      "Банк отклонил операцию",
	"insufficient_funds": "Недостаточно средств на карте",
}

func (s *server) renderDone(c *gin.Context, p Payment) {
	title, text := "Оплата прошла успешно", "Можно вернуться в магазин."
	if p.Status == StatusFailed {
		title, text = "Оплата отклонена", failureText[p.FailureReason]
	}
	c.HTML(http.StatusOK, "done.html", gin.H{"title": title, "text": text, "returnURL": p.ReturnURL})
}

func (s *server) renderExpired(c *gin.Context, p Payment) {
	c.HTML(http.StatusGone, "done.html", gin.H{
		"title": "Срок оплаты истёк", "text": "Резерв товара в магазине снят.", "returnURL": p.ReturnURL})
}

var paymentIDRegex = regexp.MustCompile(`^pay_[0-9a-f]{32}$`)

func (s *server) receipt(c *gin.Context) {
	id := c.Param("id")
	// The ID becomes part of a file path: only a strict pattern keeps "../../etc" out.
	if !paymentIDRegex.MatchString(id) {
		c.Status(http.StatusNotFound)
		return
	}
	path := filepath.Join(s.receiptsDir, "receipt_"+id+".pdf")
	if _, err := os.Stat(path); err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.FileAttachment(path, "receipt_"+id+".pdf")
}
