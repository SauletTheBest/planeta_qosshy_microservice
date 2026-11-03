package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
	"net/http"
	"os"
	"strings"
	"time"
)

type PaymentRequest struct {
	TransactionID uint    `json:"transaction_id"`
	Amount        float64 `json:"amount"`
	CardNumber    string  `json:"card_number"`
	Expiry        string  `json:"expiry"`
	Model         string
	UserName      string
	Price         float64
	Quantity      int
	TotalAmount   float64
	PaymentMethod string
	Status        string
	Email         string
	CreatedAt     time.Time `gorm:"autoCreateTime"`
}

type PaymentResponse struct {
	Success bool `json:"success"`
}

func ProcessPayment(c *gin.Context) {
	var request PaymentRequest

	// Card data is never printed to logs: anyone with access to the logs would see it.
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	success := true

	if success {
		generateReceipt(request)
		SendReceiptEmail(request.Email, request.TransactionID, request.Amount)
	}

	c.JSON(http.StatusOK, PaymentResponse{Success: success})
}

func generateReceipt(request PaymentRequest) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)

	pdf.Cell(190, 10, "Fiscal Receipt")
	pdf.Ln(15)

	pdf.SetFont("Arial", "", 12)
	addLine(pdf, "Transaction ID:", fmt.Sprintf("%d", request.TransactionID))
	addLine(pdf, "Amount Paid:", fmt.Sprintf("$%.2f", request.Amount))
	addLine(pdf, "Card Number:", maskCard(request.CardNumber))
	addLine(pdf, "Item:", request.Model)
	addLine(pdf, "User Name:", request.UserName)
	addLine(pdf, "Price per Unit:", fmt.Sprintf("$%.2f", request.Price))
	addLine(pdf, "Quantity:", fmt.Sprintf("%d", request.Quantity))
	addLine(pdf, "Total Amount:", fmt.Sprintf("$%.2f", request.TotalAmount))
	addLine(pdf, "Payment Method:", request.PaymentMethod)
	addLine(pdf, "Status:", "Paid")
	addLine(pdf, "Email:", request.Email)
	addTime(pdf, time.Now())

	filename := fmt.Sprintf("receipts/receipt_%d.pdf", request.TransactionID)
	err := os.MkdirAll("receipts", os.ModePerm)
	if err != nil {
		fmt.Println("Error creating directory:", err)
		return
	}
	err = pdf.OutputFileAndClose(filename)
	if err != nil {
		fmt.Println("Error saving PDF:", err)
		return
	}

	fmt.Println("Receipt generated:", filename)
}

// maskCard keeps only the last 4 digits, like a real bank receipt.
func maskCard(number string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1 // drop spaces and dashes
	}, number)
	if len(digits) < 4 {
		return "****"
	}
	return "**** **** **** " + digits[len(digits)-4:]
}

func addLine(pdf *gofpdf.Fpdf, label, value string) {
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(50, 10, label)
	pdf.SetFont("Arial", "", 12)
	pdf.Cell(0, 10, value)
	pdf.Ln(8)
}
func addTime(pdf *gofpdf.Fpdf, value time.Time) {
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(50, 10, "Created At")
	pdf.SetFont("Arial", "", 12)
	pdf.Cell(0, 10, value.String())
	pdf.Ln(8)
}
func main() {
	r := gin.Default()
	r.POST("/process-payment", ProcessPayment)
	r.Run(":8081")
}
