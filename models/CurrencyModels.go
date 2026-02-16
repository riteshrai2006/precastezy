package models

type Currency struct {
	ID           int    `json:"id" example:"1"`
	CurrencyName string `json:"currency_name" example:"Indian Rupee"`
	CurrencyCode string `json:"currency_code" example:"INR"`
	Symbol       string `json:"symbol" example:"₹"`
}
