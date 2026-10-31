package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"gopkg.in/gomail.v2"
)

var errSMTPNotConfigured = errors.New("SMTP is not configured")

// SendEmail sends an email with an optional attachment
func SendEmail(to, subject, body, attachmentPath string) error {
	host, user, pass := os.Getenv("SMTP_HOST"), os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASS")
	if host == "" || user == "" {
		return errSMTPNotConfigured
	}

	m := gomail.NewMessage()
	m.SetHeader("From", user)
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	// Attach the receipt if provided
	if attachmentPath != "" {
		m.Attach(attachmentPath)
	}

	port, err := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if err != nil {
		port = 587
	}

	d := gomail.NewDialer(host, port, user, pass)

	if err := d.DialAndSend(m); err != nil {
		log.Println("Email send error:", err)
		return err
	}
	return nil
}

// SendReceiptEmail sends the receipt PDF to the buyer.
func SendReceiptEmail(to string, p Payment, receiptPath string) error {
	subject := "Чек об оплате — Qosshy Pay"
	body := fmt.Sprintf(`Здравствуйте!

Оплата прошла успешно, чек во вложении.

Платёж: %s
Сумма: %s ₸
Карта: %s

Qosshy Pay (тестовый платёжный сервис)`, p.ID, formatTenge(p.AmountTiyn), maskCard(p.CardLast4))

	return SendEmail(to, subject, body, receiptPath)
}
