package ecloudsdk

import (
	"errors"
	"fmt"
	"time"
)

// Errors
var (
	ErrNotAuthenticated        = errors.New("client not authenticated")
	ErrInvalidConfig           = errors.New("invalid configuration")
	ErrHospitalNameRequired    = errors.New("hospital name is required")
	ErrHospitalNumberRequired  = errors.New("hospital number is required")
	ErrApiBaseURLRequired      = errors.New("ecloud ApiBaseURL is required")
	ErrEclinicIDRequired       = errors.New("ecloud ElinicID  is required")
	ErrEclinicBaseURL          = errors.New("EclinicBaseURL is required")
	ErrEcloudPasswordRequired  = errors.New("ecloud password is required")
	ErrEmptyToken              = errors.New("empty token received")
	ErrInvalidMedicalReportPDF = errors.New("invalid PDF for laboratory report")
	ErrInvalidLabReportPDF     = errors.New("invalid PDF for medical report")
)

// LoginRequest is used to send login credentials.
type LoginRequest struct {
	EclinicID string `json:"eclinic_id"` // Login Ecloud ID
	Password  string `json:"password"`   // Password.
}

// Partial user metadata after embedded in LoginResponse.
type User struct {
	ID        uint      `json:"id,omitempty"`         // Ecloud user ID.
	EclinicID string    `json:"eclinic_id,omitempty"` // Unique Ecloud ID.
	CreatedAt time.Time `json:"created_at,omitzero"`  // Populated by server when user is created.
	IsAdmin   bool      `json:"is_admin,omitempty"`   // Whether user is an administrator.
	Active    bool      `json:"active,omitempty"`     // Whether the user account is allowed to login.
}

// LoginResponse is used to decode Login response.
type LoginResponse struct {
	Token string `json:"token"` // JWT token.
	User  User   `json:"user"`  // User object
}

// Bill represents the user subscription amount and duration of the subscription.
type Bill struct {
	Amount   float64       // Subscription amount.
	Duration time.Duration // Duration of the subscription before expiry.
}

type Subscriber struct {
	ID             uint      `json:"id"`              // Primary Key for the subscription.
	EclinicID      string    `json:"eclinic_id"`      // Unique subcription ID
	PatientID      uint      `json:"patient_id"`      // Patient ID in Eclinic HMS
	PatientName    string    `json:"patient_name"`    // Name of the patient.
	Email          string    `json:"email"`           // Optional email.
	HospitalNumber string    `json:"hospital_number"` // Globally unique Hospital Number.
	HospitalName   string    `json:"hospital_name"`   // Hospital name.
	RegisteredBy   string    `json:"registered_by"`   // The person who subscribed the patient.
	CreatedAt      time.Time `json:"created_at"`      // Populated by the remote server.
}

// Payment represents a payment for a patient's subscription.
// We assume that a payment is valid from the time it is made until the subscription
// duration.
type Payment struct {
	ID           uint      `json:"id,omitempty"`            // Payment ID.
	SubscriberID uint      `json:"subscriber_id,omitempty"` // ID of the subscriber.
	Amount       float64   `json:"amount,omitempty"`        // Amount paid for the subscription.
	CreatedAt    time.Time `json:"created_at,omitzero"`     // Creation timestamp for the payment.

	// ValidTo is the time the payment is valid to.
	// After this time, the patient's records will not be accessible and must be renewed.
	ValidTo time.Time `json:"valid_to,omitzero"`

	// The person who made the payment.
	RegisteredBy string `json:"registered_by,omitempty"`

	// Whether the records associated with this payment have been uploaded to the cloud.
	// This helps to dedupe records preventing multiple uploads.
	RecordsUploaded bool `json:"records_uploaded,omitempty"`

	// The last time the records were uploaded.
	LastUploaded *time.Time `json:"last_uploaded,omitempty"`
}

// PatientRecord represents a patient's medical record.
// When syncing medical records, either MedicalReport or LabReport or both
// must be provided.
type PatientRecord struct {
	ID             uint      `json:"id,omitempty"`
	HospitalNumber string    `json:"hospital_number,omitempty"`
	VisitID        uint      `json:"visit_id,omitempty"`
	SubscriberID   uint      `json:"subscriber_id,omitempty"`
	VisitTimestamp time.Time `json:"visit_timestamp,omitzero"`
	CreatedAt      time.Time `json:"created_at,omitzero"`
	Title          string    `json:"title,omitempty"`

	// Only present when decoding from JSON.
	// Uploaded separately as files.
	MedicalReport []byte `json:"medical_report,omitempty"`

	// Only present when decoding from JSON.
	// Uploaded separately as files.
	LabReport []byte `json:"lab_report,omitempty"`
}

func (pr *PatientRecord) Validate() error {
	if pr == nil {
		return fmt.Errorf("patient record is nil")
	}

	if pr.VisitID == 0 {
		return fmt.Errorf("patient record missing VisitID")
	}
	if pr.SubscriberID == 0 {
		return fmt.Errorf("patient record missing SubscriberID")
	}
	if pr.Title == "" {
		return fmt.Errorf("patient record missing Title")
	}
	if pr.VisitTimestamp.IsZero() {
		return fmt.Errorf("patient record missing valid VisitTimestamp")
	}
	if pr.MedicalReport == nil && pr.LabReport == nil {
		return fmt.Errorf("no medical report or laboratory report to upload")
	}

	return nil
}

type SubscribeRequest struct {
	PatientID    uint   `json:"patient_id"`
	PatientName  string `json:"patient_name"`
	Email        string `json:"email"`
	RegisteredBy string `json:"registered_by"`
}

// Configuration for the client
type Config struct {
	ApiBaseUrl     string
	EclinicId      string
	Password       string
	HospitalNumber string
	HospitalName   string
	EclinicBaseUrl string

	HTTPClient  HTTPClient
	Logger      Logger
	RetryPolicy RetryPolicy
	Timeout     time.Duration
}

func (c *Config) Validate() error {
	if c.ApiBaseUrl == "" {
		return ErrApiBaseURLRequired
	}

	if c.EclinicId == "" {
		return ErrEclinicIDRequired
	}

	if c.Password == "" {
		return ErrEcloudPasswordRequired
	}

	if c.HospitalNumber == "" {
		return ErrHospitalNumberRequired
	}

	if c.HospitalName == "" {
		return ErrHospitalNameRequired
	}

	if c.EclinicBaseUrl == "" {
		return ErrEclinicBaseURL
	}

	// Set default timeout if not provided
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}

	if c.RetryPolicy == nil {
		c.RetryPolicy = &DefaultRetryPolicy{3}
	}
	return nil
}
