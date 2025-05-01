package main

import (
	"fmt"
	"log"
	_ "net/smtp"
	"os"
	"strconv"

	"gopkg.in/gomail.v2"
)

var (
	smtpHost = os.Getenv("SMTP_HOST")
	smtpPort = 587
	smtpUser = os.Getenv("SMTP_USER")
	smtpPass = os.Getenv("SMTP_PASS")
)

// SendEmail sends an email with an optional attachment
func SendEmail(to, subject, body, attachmentPath string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", smtpUser)
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

	d := gomail.NewDialer(smtpHost, port, smtpUser, smtpPass)

	if err := d.DialAndSend(m); err != nil {
		log.Println("Email send error:", err)
		return err
	}
	return nil
}

// SendReceiptEmail sends a receipt email with the PDF attachment
func SendReceiptEmail(email string, transactionID uint, amount float64) error {
	receiptPath := fmt.Sprintf("receipts/receipt_%d.pdf", transactionID)

	subject := "Your Payment Receipt"
	body := fmt.Sprintf(`Dear Customer,

Thank you for your purchase. Please find your payment receipt attached.

Transaction ID: %d
Amount Paid: $%.2f

Best Regards,
Your Company`, transactionID, amount)

	return SendEmail(email, subject, body, receiptPath)
}
