package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/models"

	"github.com/gin-gonic/gin"
	"github.com/jung-kurt/gofpdf"
	"github.com/lib/pq"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// GenerateInvoicePDF godoc
// @Summary      Generate invoice PDF
// @Tags         invoices
// @Param        id   path  int  true  "Invoice ID"
// @Success      200  "PDF file"
// @Failure      400  {object}  object
// @Failure      404  {object}  object
// @Router       /api/invoice_pdf/{id} [get]
func GenerateInvoicePDF(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		invoiceID := c.Param("id")
		if invoiceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing invoice_id"})
			return
		}

		titleCaser := cases.Title(language.Und)

		// --- Fetch invoice details ---
		var inv models.GetInvoice
		var totalPaid, totalAmount sql.NullFloat64
		var updatedBy sql.NullInt64
		var updatedAt sql.NullTime

		err := db.QueryRow(`
			SELECT 
				i.id, i.work_order_id, i.created_by, i.revision_no, i.billing_address, 
				i.shipping_address, i.created_at, i.payment_status, i.total_paid, i.total_amount, 
				i.updated_by, i.updated_at, CONCAT(u.first_name, ' ', u.last_name) AS created_by_name, 
                i.name, wo.wo_number, wo.wo_date, wo.wo_validate, wo.total_value, 
                wo.contact_person, wo.payment_term::text, wo.wo_description,
				wo.endclient_id, ec.contact_person AS endclient_name,
				wo.project_id, p.name AS project_name, 
				wo.contact_email, wo.contact_number, wo.phone_code, 
				pc.phone_code AS phone_code_name, wo.revision
			FROM invoice i
			JOIN users u ON i.created_by = u.id
			JOIN work_order wo ON i.work_order_id = wo.id
			JOIN end_client ec ON wo.endclient_id = ec.id
			JOIN project p ON wo.project_id = p.project_id
			JOIN phone_code pc ON wo.phone_code = pc.id
			WHERE i.id = $1
        `, invoiceID).Scan(
			&inv.ID, &inv.WorkOrderID, &inv.CreatedBy, &inv.RevisionNo,
			&inv.BillingAddress, &inv.ShippingAddress, &inv.CreatedAt, &inv.PaymentStatus,
			&totalPaid, &totalAmount, &updatedBy, &updatedAt, &inv.CreatedByName, &inv.Name,
			&inv.WONumber, &inv.WODate, &inv.WOValidate, &inv.TotalValue,
			&inv.ContactPerson, new(sql.NullString), &inv.WODescription,
			&inv.EndClientID, &inv.EndClient, &inv.ProjectID, &inv.ProjectName,
			&inv.ContactEmail, &inv.ContactNumber, &inv.PhoneCode,
			&inv.PhoneCodeName, &inv.WORevision,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// read and unmarshal payment_term (jsonb) and prepare display string
		var paymentTermDisplay string
		var paymentTermRaw sql.NullString
		if err := db.QueryRow(`SELECT wo.payment_term::text FROM work_order wo WHERE wo.id = $1`, inv.WorkOrderID).Scan(&paymentTermRaw); err == nil && paymentTermRaw.Valid {
			var pt map[string]float64
			if json.Unmarshal([]byte(paymentTermRaw.String), &pt) == nil {
				parts := []string{}
				if v, ok := pt["casted"]; ok {
					parts = append(parts, fmt.Sprintf("Casted: %.0f%%", v))
				}
				if v, ok := pt["dispatch"]; ok {
					parts = append(parts, fmt.Sprintf("Dispatch: %.0f%%", v))
				}
				if v, ok := pt["erection"]; ok {
					parts = append(parts, fmt.Sprintf("Erection: %.0f%%", v))
				}
				if v, ok := pt["handover"]; ok {
					parts = append(parts, fmt.Sprintf("Handover: %.0f%%", v))
				}
				if len(parts) > 0 {
					paymentTermDisplay = strings.Join(parts, ", ")
				} else {
					paymentTermDisplay = paymentTermRaw.String
				}
			} else {
				paymentTermDisplay = paymentTermRaw.String
			}
		}

		if totalPaid.Valid {
			inv.TotalPaid = totalPaid.Float64
		}
		// if totalAmount.Valid {
		// 	inv.TotalAmount = totalAmount.Float64
		// }
		// if updatedBy.Valid {
		// 	inv.UpdatedBy = int(updatedBy.Int64)
		// }
		// if updatedAt.Valid {
		// 	inv.UpdatedAt = updatedAt.Time
		// }

		// --- Fetch invoice items ---
		rows, err := db.Query(`
			SELECT i.id, i.invoice_id, i.item_id, i.volume, wom.hsn_code, wom.tower_id, wom.floor_id, 
				   wom.item_name, wom.unit_rate, wom.tax
			FROM invoice_item i
			LEFT JOIN work_order_material wom ON i.item_id = wom.id
			WHERE invoice_id = $1
		`, invoiceID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var items []models.GetInvoiceItem
		for rows.Next() {
			var item models.GetInvoiceItem
			var towerID sql.NullInt64
			var floorIDs pq.Int64Array
			if err := rows.Scan(&item.ID, &item.InvoiceID, &item.ItemID, &item.Volume,
				&item.HSNCode, &towerID, &floorIDs, &item.ItemName, &item.UnitRate, &item.Tax); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			items = append(items, item)
		}
		inv.Items = items

		// --- Fetch payment details ---
		var paymentDetails []struct {
			UTRNumber   string
			AmountPaid  float64
			PaymentDate time.Time
			PaymentMode string
			Remarks     sql.NullString
		}

		if inv.PaymentStatus == "partial_paid" || inv.PaymentStatus == "fully_paid" {
			paymentRows, err := db.Query(`
				SELECT utr_number, amount_paid, payment_date, payment_mode, remarks 
				FROM invoice_payment 
				WHERE invoice_id = $1 
				ORDER BY payment_date ASC
			`, invoiceID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			defer paymentRows.Close()

			for paymentRows.Next() {
				var p struct {
					UTRNumber   string
					AmountPaid  float64
					PaymentDate time.Time
					PaymentMode string
					Remarks     sql.NullString
				}
				if err := paymentRows.Scan(&p.UTRNumber, &p.AmountPaid, &p.PaymentDate, &p.PaymentMode, &p.Remarks); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				paymentDetails = append(paymentDetails, p)
			}
		}

		// --- Generate PDF ---
		pdf := gofpdf.New("P", "mm", "A4", "")
		pdf.AddPage()
		pdf.SetMargins(10, 10, 10)
		pdf.SetFont("Arial", "", 10)

		// --- Header ---
		pdf.SetFont("Arial", "B", 18)
		pdf.Cell(190, 10, "INVOICE")
		pdf.Ln(12)

		// --- Billing & Shipping ---
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(95, 8, "Billing Address")
		pdf.Cell(95, 8, "Shipping Address")
		pdf.Ln(8)

		pdf.SetFont("Arial", "", 10)
		pdf.MultiCell(90, 6, inv.BillingAddress, "", "", false)
		pdf.SetXY(110, 38)
		pdf.MultiCell(90, 6, fmt.Sprintf(
			"%s\n%s\n%s\n%s\n%s",
			inv.EndClient, inv.ContactPerson, inv.ShippingAddress, inv.ContactEmail, inv.ContactNumber,
		), "", "", false)
		pdf.Ln(10)

		// --- Invoice Info ---
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(95, 6, fmt.Sprintf("Invoice No: %s", inv.WONumber))
		pdf.Cell(95, 6, fmt.Sprintf("Due Date: %s", inv.WOValidate.Format("02-Jan-2006")))
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		pdf.Cell(95, 6, fmt.Sprintf("Invoice Date: %s", inv.WODate.Format("02-Jan-2006")))
		pdf.Ln(10)

		// --- Table Header ---
		pdf.SetFont("Arial", "B", 11)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(60, 8, "Item", "1", 0, "L", true, 0, "")
		pdf.CellFormat(25, 8, "Qty", "1", 0, "C", true, 0, "")
		pdf.CellFormat(25, 8, "Unit Rate", "1", 0, "C", true, 0, "")
		pdf.CellFormat(25, 8, "Tax (%)", "1", 0, "C", true, 0, "")
		pdf.CellFormat(35, 8, "Subtotal", "1", 1, "C", true, 0, "")

		pdf.SetFont("Arial", "", 10)
		var grandTotal, totalTax float64

		for _, item := range inv.Items {
			subtotal := item.UnitRate * item.Volume
			taxAmount := subtotal * item.Tax / 100
			total := subtotal + taxAmount
			grandTotal += total
			totalTax += taxAmount

			pdf.CellFormat(60, 8, item.ItemName, "1", 0, "L", false, 0, "")
			pdf.CellFormat(25, 8, fmt.Sprintf("%.2f", item.Volume), "1", 0, "C", false, 0, "")
			pdf.CellFormat(25, 8, fmt.Sprintf("%.2f", item.UnitRate), "1", 0, "C", false, 0, "")
			pdf.CellFormat(25, 8, fmt.Sprintf("%.2f", item.Tax), "1", 0, "C", false, 0, "")
			pdf.CellFormat(35, 8, fmt.Sprintf("%.2f", total), "1", 1, "R", false, 0, "")
		}

		pdf.Ln(5)

		// --- Payment Summary ---
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(140, 8, "Subtotal")
		pdf.CellFormat(35, 8, fmt.Sprintf("%.2f", grandTotal-totalTax), "1", 1, "R", false, 0, "")
		pdf.Cell(140, 8, "Tax Total")
		pdf.CellFormat(35, 8, fmt.Sprintf("%.2f", totalTax), "1", 1, "R", false, 0, "")
		pdf.Cell(140, 8, "Total Amount")
		pdf.CellFormat(35, 8, fmt.Sprintf("%.2f", grandTotal), "1", 1, "R", false, 0, "")
		pdf.Cell(140, 8, "Amount Paid")
		pdf.CellFormat(35, 8, fmt.Sprintf("%.2f", inv.TotalPaid), "1", 1, "R", false, 0, "")
		pdf.Cell(140, 8, "Balance Due")
		pdf.CellFormat(35, 8, fmt.Sprintf("%.2f", grandTotal-inv.TotalPaid), "1", 1, "R", false, 0, "")

		// --- Payment Terms ---
		pdf.Ln(8)
		pdf.SetFont("Arial", "B", 11)
		pdf.Cell(190, 8, "Payment Terms:")
		pdf.Ln(6)
		pdf.SetFont("Arial", "", 10)
		pdf.MultiCell(190, 6, paymentTermDisplay, "", "L", false)
		pdf.Ln(5)

		// --- Payment Info ---
		pdf.Ln(10)
		pdf.SetFont("Arial", "B", 12)
		pdf.Cell(190, 8, "Payment Details:")
		pdf.Ln(8)
		pdf.SetFont("Arial", "", 11)
		if inv.PaymentStatus == "unpaid" {
			pdf.Cell(190, 8, "Payment Status: Unpaid")
			pdf.Ln(8)
		} else {
			for _, p := range paymentDetails {
				pdf.Cell(190, 6, fmt.Sprintf("UTR: %s | Amount: %.2f | Date: %s | Mode: %s",
					p.UTRNumber, p.AmountPaid, p.PaymentDate.Format("02-Jan-2006"), p.PaymentMode))
				pdf.Ln(6)
				if p.Remarks.Valid && p.Remarks.String != "" {
					pdf.Cell(190, 6, fmt.Sprintf("Remarks: %s", p.Remarks.String))
					pdf.Ln(6)
				}
			}
			pdf.Ln(4)
			pdf.SetFont("Arial", "B", 11)
			pdf.Cell(190, 6, fmt.Sprintf("Total Paid: %.2f | Status: %s", inv.TotalPaid, titleCaser.String(inv.PaymentStatus)))
		}

		// --- Footer ---
		pdf.SetY(-20)
		pdf.SetFont("Arial", "I", 8)
		pdf.Cell(190, 6, "This is a computer-generated invoice. No signature required.")
		pdf.Ln(5)
		pdf.Cell(190, 6, "Generated on: "+time.Now().Format("2006-01-02 15:04:05"))

		// --- Output PDF ---
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=invoice_%s.pdf", inv.WONumber))
		if err := pdf.Output(c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate PDF"})
			return
		}
	}
}
