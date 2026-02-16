package models

import (
	"fmt"
	"time"
)

// VehicleDetailsRequest represents nested vehicle details structure
type VehicleDetailsRequest struct {
	VehicleNumber           string `json:"vehicle_number"`
	Capacity                int    `json:"capacity"`
	TransporterID           int    `json:"transporter_id"`
	TruckType               string `json:"truck_type"`
	DriverName              string `json:"driver_name"`
	DriverPhoneNo           string `json:"driver_phone_no"`
	EmergencyContactPhoneNo string `json:"emergency_contact_phone_no"`
}

// DispatchOrderRequest represents the request structure for creating a dispatch order
type DispatchOrderRequest struct {
	ProjectID               int                    `json:"project_id" binding:"required"`
	DriverName              string                 `json:"driver_name"`
	DriverPhoneNo           string                 `json:"driver_phone_no"`
	EmergencyContactPhoneNo string                 `json:"emergency_contact_phone_no"`
	Items                   []int                  `json:"items" binding:"required"`
	VehicleNumber           string                 `json:"vehicle_number"`
	Capacity                int                    `json:"capacity"`
	TransporterID           int                    `json:"transporter_id"`
	TruckType               string                 `json:"truck_type"`
	VehicleDetails          *VehicleDetailsRequest `json:"vehicle_details"`
	// Legacy field for backward compatibility (optional)
	VehicleId int `json:"vehicle_id"`
}

// AfterUnmarshal processes the request to extract vehicle details from nested structure if present
func (r *DispatchOrderRequest) AfterUnmarshal() {
	// If vehicle_details is provided, extract values from it
	if r.VehicleDetails != nil {
		if r.VehicleDetails.VehicleNumber != "" && r.VehicleNumber == "" {
			r.VehicleNumber = r.VehicleDetails.VehicleNumber
		}
		if r.VehicleDetails.Capacity != 0 && r.Capacity == 0 {
			r.Capacity = r.VehicleDetails.Capacity
		}
		if r.VehicleDetails.TransporterID != 0 && r.TransporterID == 0 {
			r.TransporterID = r.VehicleDetails.TransporterID
		}
		if r.VehicleDetails.TruckType != "" && r.TruckType == "" {
			r.TruckType = r.VehicleDetails.TruckType
		}
		// Also extract driver details if provided in vehicle_details
		if r.VehicleDetails.DriverName != "" && r.DriverName == "" {
			r.DriverName = r.VehicleDetails.DriverName
		}
		if r.VehicleDetails.DriverPhoneNo != "" && r.DriverPhoneNo == "" {
			r.DriverPhoneNo = r.VehicleDetails.DriverPhoneNo
		}
		if r.VehicleDetails.EmergencyContactPhoneNo != "" && r.EmergencyContactPhoneNo == "" {
			r.EmergencyContactPhoneNo = r.VehicleDetails.EmergencyContactPhoneNo
		}
	}
}

// ErrItemsUnavailable represents an error when requested items are not available
type ErrItemsUnavailable struct {
	ItemIDs []int
}

func (e *ErrItemsUnavailable) Error() string {
	return fmt.Sprintf("items not available: %v", e.ItemIDs)
}

// DispatchTrackingLog represents a tracking log entry for a dispatch order
type DispatchTrackingLog struct {
	ID              int       `json:"id"`
	OrderNumber     string    `json:"order_number"`
	Status          string    `json:"status"`
	Location        string    `json:"location"`
	Remarks         string    `json:"remarks"`
	StatusTimestamp time.Time `json:"status_timestamp"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
