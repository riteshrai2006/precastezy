package models

// import (
// 	"time"
// )

// // Quotation represents a quotation document
// type Quotation struct {
// 	ID              int       `json:"id" db:"id"`
// 	ProjectID       int       `json:"project_id" db:"project_id"`
// 	VendorID        int       `json:"vendor_id" db:"vendor_id"`
// 	QuotationNumber string    `json:"quotation_number" db:"quotation_number"`
// 	QuotationDate   time.Time `json:"quotation_date" db:"quotation_date"`
// 	ValidUntil      time.Time `json:"valid_until" db:"valid_until"`
// 	TotalAmount     float64   `json:"total_amount" db:"total_amount"`
// 	Currency        string    `json:"currency" db:"currency"`
// 	Status          string    `json:"status" db:"status"` // pending, approved, rejected
// 	FileURL         string    `json:"file_url" db:"file_url"`
// 	CreatedAt       time.Time `json:"created_at" db:"created_at"`
// 	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
// }

// // QuotationLineItem represents individual items in a quotation
// type QuotationLineItem struct {
// 	ID           int       `json:"id" db:"id"`
// 	QuotationID  int       `json:"quotation_id" db:"quotation_id"`
// 	ItemName     string    `json:"item_name" db:"item_name"`
// 	Description  string    `json:"description" db:"description"`
// 	Quantity     float64   `json:"quantity" db:"quantity"`
// 	Unit         string    `json:"unit" db:"unit"`
// 	UnitPrice    float64   `json:"unit_price" db:"unit_price"`
// 	TotalPrice   float64   `json:"total_price" db:"total_price"`
// 	StandardUnit string    `json:"standard_unit" db:"standard_unit"`   // Converted to standard unit
// 	StdQuantity  float64   `json:"std_quantity" db:"std_quantity"`     // Quantity in standard unit
// 	StdUnitPrice float64   `json:"std_unit_price" db:"std_unit_price"` // Unit price in standard unit
// 	CreatedAt    time.Time `json:"created_at" db:"created_at"`
// 	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
// }

// // Vendor represents quotation vendor details
// type Vendors struct {
// 	ID          int       `json:"id" db:"id"`
// 	ProjectID   int       `json:"project_id" db:"project_id"`
// 	Name        string    `json:"name" db:"name"`
// 	Email       string    `json:"email" db:"email"`
// 	Phone       string    `json:"phone" db:"phone"`
// 	Address     string    `json:"address" db:"address"`
// 	CompanyName string    `json:"company_name" db:"company_name"`
// 	GSTNumber   string    `json:"gst_number" db:"gst_number"`
// 	CreatedAt   time.Time `json:"created_at" db:"created_at"`
// 	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
// }

// // QuotationComparison represents comparison results
// type QuotationComparison struct {
// 	ItemName        string                    `json:"item_name"`
// 	BestVendor      string                    `json:"best_vendor"`
// 	BestPrice       float64                   `json:"best_price"`
// 	BestQuotationID int                       `json:"best_quotation_id"`
// 	AllQuotations   []QuotationComparisonItem `json:"all_quotations"`
// }

// // QuotationComparisonItem represents individual quotation comparison
// type QuotationComparisonItem struct {
// 	Id           int     `json:"id"`
// 	ItemName     string  `json:"item_name"`
// 	QuotationId  int     `json:"quotation_id"`
// 	VendorName   string  `json:"vendor_name"`
// 	Quantity     float64 `json:"quantity"`
// 	Unit         string  `json:"unit"`
// 	UnitPrice    float64 `json:"unit_price"`
// 	TotalPrice   float64 `json:"total_price"`
// 	StdQuantity  float64 `json:"std_quantity"`
// 	StdUnitPrice float64 `json:"std_unit_price"`
// 	IsBest       bool    `json:"is_best"`
// }

// // UnitConversion represents unit conversion rules
// type UnitConversion struct {
// 	FromUnit         string  `json:"from_unit" db:"from_unit"`
// 	ToUnit           string  `json:"to_unit" db:"to_unit"`
// 	ConversionFactor float64 `json:"conversion_factor" db:"conversion_factor"`
// 	Category         string  `json:"category" db:"category"` // weight, length, volume, etc.
// }

// // QuotationUploadRequest represents the upload request
// type QuotationUploadRequest struct {
// 	ProjectID       int    `json:"project_id" binding:"required"`
// 	QuotationNumber string `json:"quotation_number"`
// 	QuotationDate   string `json:"quotation_date"`
// 	ValidUntil      string `json:"valid_until"`
// 	Currency        string `json:"currency"`
// 	VendorName      string `json:"vendor_name"`
// 	VendorEmail     string `json:"vendor_email"`
// 	VendorPhone     string `json:"vendor_phone"`
// 	VendorAddress   string `json:"vendor_address"`
// 	CompanyName     string `json:"company_name"`
// 	GSTNumber       string `json:"gst_number"`
// }

// // QuotationUploadResponse represents the upload response
// type QuotationUploadResponse struct {
// 	QuotationID      int                   `json:"quotation_id"`
// 	VendorID         int                   `json:"vendor_id"`
// 	ExtractedItems   []QuotationLineItem   `json:"extracted_items"`
// 	ComparisonResult []QuotationComparison `json:"comparison_result"`
// 	Message          string                `json:"message"`
// }
