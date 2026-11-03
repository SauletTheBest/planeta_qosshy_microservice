package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// writeReceipt saves a PDF receipt for a successful payment and returns its path.
// Labels are in English: gofpdf's built-in fonts have no Cyrillic letters.
func writeReceipt(dir string, p Payment, paidAt time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(190, 10, "Qosshy Pay - payment receipt")
	pdf.Ln(15)

	addLine(pdf, "Payment ID:", p.ID)
	addLine(pdf, "Order reference:", p.MerchantReference)
	addLine(pdf, "Amount paid:", formatTenge(p.AmountTiyn)+" KZT")
	addLine(pdf, "Card:", maskCard(p.CardLast4)) // never the full number
	addLine(pdf, "Status:", "Paid")
	addLine(pdf, "Date:", paidAt.Format("02.01.2006 15:04:05 MST"))

	path := filepath.Join(dir, "receipt_"+p.ID+".pdf")
	return path, pdf.OutputFileAndClose(path)
}

func addLine(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(50, 10, label)
	pdf.SetFont("Arial", "", 12)
	pdf.Cell(0, 10, value)
	pdf.Ln(8)
}
