package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Test cards, like the ones real providers publish for their sandboxes.
// Any other card number that passes the Luhn check is accepted.
var testDeclines = map[string]string{
	"4000000000000002": "card_declined",
	"4000000000009995": "insufficient_funds",
}

type cardInput struct {
	Number string // digits only
	Last4  string
}

var errCardInvalid = errors.New("invalid card")

// validateCard checks the form fields like a payment page would. The CVV is checked
// and then forgotten: it is never stored or logged anywhere.
func validateCard(number, expiry, cvv, holder string, now time.Time) (cardInput, string) {
	digits := strings.NewReplacer(" ", "", "-", "").Replace(number)
	if len(digits) < 13 || len(digits) > 19 || !allDigits(digits) || !luhnValid(digits) {
		return cardInput{}, "Номер карты указан неверно"
	}
	if !expiryValid(expiry, now) {
		return cardInput{}, "Срок действия указан неверно или карта просрочена"
	}
	if (len(cvv) != 3 && len(cvv) != 4) || !allDigits(cvv) {
		return cardInput{}, "CVV — это 3 или 4 цифры на обороте карты"
	}
	if h := strings.TrimSpace(holder); h == "" || len(h) > 60 {
		return cardInput{}, "Укажите имя владельца карты"
	}
	return cardInput{Number: digits, Last4: digits[len(digits)-4:]}, ""
}

// luhnValid is the checksum every card number has: it catches most typos
// (a wrong digit, two swapped neighbours) before anything is sent to a bank.
func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// expiryValid accepts "MM/YY" for a month that hasn't ended yet.
func expiryValid(expiry string, now time.Time) bool {
	mm, yy, ok := strings.Cut(strings.TrimSpace(expiry), "/")
	if !ok || len(mm) != 2 || len(yy) != 2 {
		return false
	}
	month, err1 := strconv.Atoi(mm)
	year, err2 := strconv.Atoi(yy)
	if err1 != nil || err2 != nil || month < 1 || month > 12 {
		return false
	}
	// The card works until the end of its month: compare with the first day of the next one.
	endOfMonth := time.Date(2000+year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)
	return now.Before(endOfMonth)
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// maskCard keeps only the last 4 digits, like a real bank receipt.
func maskCard(last4 string) string {
	return "**** **** **** " + last4
}

// formatTenge prints tiyn as "15 000,50" (same format as the shop).
func formatTenge(tiyn int64) string {
	whole := strconv.FormatInt(tiyn/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	if frac := tiyn % 100; frac != 0 {
		return fmt.Sprintf("%s,%02d", b.String(), frac)
	}
	return b.String()
}
