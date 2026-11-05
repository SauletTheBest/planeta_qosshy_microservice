# Qosshy Pay — test payment service

A **test acquirer** for [Planeta Qosshy](https://github.com/SauletTheBest/Planeta_Qosshy).
It plays the part a real provider (a bank, CloudPayments, Stripe…) plays for a shop, so
the shop's payment code can be written and tested exactly as it would be for real money.
No real money moves.

```text
 shop ── POST /api/v1/payments ──► Qosshy Pay ── 303 ──► buyer types the card here
  ▲                                     │
  └──── signed webhook (HMAC-SHA256) ◄──┘   payment.succeeded / payment.failed / payment.refunded
```

## API (for the shop, `Authorization: Bearer <API_KEY>`)

| Method | Path | What it does |
| :--- | :--- | :--- |
| `POST` | `/api/v1/payments` | Opens a payment session, returns `payment_id` and `payment_url`. `Idempotency-Key` header is required: the same key returns the same payment (safe retries after a timeout). |
| `POST` | `/api/v1/payments/:id/refund` | Full refund of a succeeded payment. Repeating it is safe: same answer, no second refund. `409` if the payment never succeeded. |

## Pages (for the buyer's browser)

| Path | |
| :--- | :--- |
| `GET/POST /pay/:id` | The card form. Only the last 4 digits are kept. |
| `GET /receipts/:id` | PDF receipt (card number masked). |
| `GET /healthz` | Health check for Docker. |

## Webhooks

Every event is sent to `MERCHANT_WEBHOOK_URL` with the header
`Qosshy-Signature: t=<unix time>,v1=<hex HMAC-SHA256(secret, "<t>.<body>")>`.
The timestamp is signed too, so an old captured webhook can't be replayed later.
Delivery is retried (1, 2, 4 … 32 s) until the shop answers `2xx` — the shop must
handle duplicates.

## Test cards

| Card | Result |
| :--- | :--- |
| `4242 4242 4242 4242` | success |
| `4000 0000 0000 0002` | declined (`card_declined`) |
| `4000 0000 0000 9995` | declined (`insufficient_funds`) |

Any valid card number (Luhn check) with a future expiry succeeds.

## Settings

| Variable | |
| :--- | :--- |
| `API_KEY`, `WEBHOOK_SECRET` | 32+ characters each, the same values the shop has |
| `MERCHANT_WEBHOOK_URL` | where webhooks go |
| `MERCHANT_SITE_URL` | `return_url` must point there (a leaked key can't send buyers elsewhere) |
| `PUBLIC_URL` | how browsers reach this service (default `http://localhost:8081`) |
| `DATA_FILE` | payments are saved there as JSON and survive a restart (`/app/data/payments.json` in Docker); empty = memory only |
| `PORT` | default `8081` |
| `SMTP_*` | optional: email the receipt to the buyer |

Normally it is started by the shop's `docker-compose.yml`. Alone:

```bash
API_KEY=... WEBHOOK_SECRET=... MERCHANT_WEBHOOK_URL=http://localhost:8080/payments/webhook \
MERCHANT_SITE_URL=http://localhost:8080 go run .
```

## Tests

```bash
go test -race ./...    # -race needs cgo; CI runs it on Linux
```

Known limit: webhooks still waiting for a retry are lost if the service restarts
(a real provider keeps them in a database — the "outbox" pattern).
