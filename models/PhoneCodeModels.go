package models

type PhoneCode struct {
	ID          int    `json:"id" example:"1"`
	CountryName string `json:"country_name" example:"India"`
	PhoneCode   string `json:"phone_code" example:"+91"`
}
