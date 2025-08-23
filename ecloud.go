package ecloudsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"time"
)

// AuthProvider handles authentication and token management
type AuthProvider interface {
	Login(ctx context.Context) (*LoginResponse, error)
	GetToken() string
	GetUser() (*User, error)
	IsAuthenticated() bool
	Refresh(ctx context.Context) error
}

// HTTPClient abstracts HTTP operations for easier testing and customization
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// BillingService handles all billing-related operations
type BillingService interface {
	GetBill(ctx context.Context) (*Bill, error)
}

// SubscriptionService handles subscription management
type SubscriptionService interface {
	Subscribe(ctx context.Context, req *SubscribeRequest) (*Subscriber, error)
	GetSubscriber(ctx context.Context, subscriberID uint) (*Subscriber, error)
	GetPatientSubscription(ctx context.Context, patientID uint) (*Subscriber, error)
	GetHospitalSubscribers(ctx context.Context) ([]*Subscriber, error)
	GetPendingSubscribers(ctx context.Context) ([]*Subscriber, error)
}

// PaymentService handles payment operations
type PaymentService interface {
	CreatePayment(ctx context.Context, subscriberID uint, amountToPay float64, registeredBy string) (*Payment, error)
	GetSubscriberPayments(ctx context.Context, subscriberID uint) ([]*Payment, error)
}

// RecordsService handles medical records synchronization
type RecordsService interface {
	SyncMedicalRecords(ctx context.Context, patientRecord *PatientRecord) error
}

// Logger interface for pluggable logging
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// RetryPolicy defines retry behavior
type RetryPolicy interface {
	ShouldRetry(attempt int, err error, resp *http.Response) bool
	BackoffDuration(attempt int) time.Duration
	MaxRetries() int
}

// DefaultRetryPolicy implements exponential backoff
type DefaultRetryPolicy struct {
	maxRetries int
}

func (p *DefaultRetryPolicy) ShouldRetry(attempt int, err error, resp *http.Response) bool {
	if attempt >= p.maxRetries {
		return false
	}

	// Retry on network errors or 5xx status codes
	if err != nil || (resp != nil && resp.StatusCode >= 500) {
		return true
	}

	// Retry on 401 for token refresh
	if resp != nil && resp.StatusCode == 401 {
		return true
	}

	return false
}

func (p *DefaultRetryPolicy) BackoffDuration(attempt int) time.Duration {
	return time.Duration(attempt*attempt) * time.Second
}

func (p *DefaultRetryPolicy) MaxRetries() int {
	return p.maxRetries
}

// Main client interface that composes all services
type EcloudClient interface {
	AuthProvider
	BillingService
	SubscriptionService
	PaymentService
	RecordsService
}

// DefaultEcloudClient implements all interfaces
type DefaultEcloudClient struct {
	config      *Config
	httpClient  HTTPClient
	logger      Logger
	retryPolicy RetryPolicy

	// Authentication state
	jwtToken      string
	user          User
	authenticated bool
}

func NewEcloudClient(config *Config) (EcloudClient, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	client := &DefaultEcloudClient{config: config}

	// Set defaults if not provided
	if config.HTTPClient != nil {
		client.httpClient = config.HTTPClient
	} else {
		client.httpClient = &http.Client{Timeout: config.Timeout}
	}

	if config.Logger != nil {
		client.logger = config.Logger
	} else {
		client.logger = &NoOpLogger{}
	}

	if config.RetryPolicy != nil {
		client.retryPolicy = config.RetryPolicy
	} else {
		client.retryPolicy = &DefaultRetryPolicy{maxRetries: 3}
	}

	return client, nil
}

// Authentication implementation
func (c *DefaultEcloudClient) Login(ctx context.Context) (*LoginResponse, error) {
	loginReq := LoginRequest{
		EclinicID: c.config.EclinicId,
		Password:  c.config.Password,
	}

	body, err := json.Marshal(loginReq)
	if err != nil {
		return nil, err
	}

	url := c.config.ApiBaseUrl + "/api/auth/login"
	resp, err := c.performRequest(ctx, "POST", url, bytes.NewReader(body), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("error decoding login response: %w", err)
	}

	if loginResp.Token == "" {
		return nil, ErrEmptyToken
	}

	// Update client state
	c.jwtToken = loginResp.Token
	c.user = loginResp.User
	c.authenticated = true

	c.logger.Info("successfully authenticated user: %s\n", loginResp.User.EclinicID)
	return &loginResp, nil
}

func (c *DefaultEcloudClient) GetToken() string {
	return c.jwtToken
}

func (c *DefaultEcloudClient) GetUser() (*User, error) {
	if !c.authenticated {
		return nil, ErrNotAuthenticated
	}
	return &c.user, nil
}

func (c *DefaultEcloudClient) IsAuthenticated() bool {
	return c.authenticated && c.jwtToken != ""
}

func (c *DefaultEcloudClient) Refresh(ctx context.Context) error {
	_, err := c.Login(ctx)
	return err
}

// Billing implementation
func (c *DefaultEcloudClient) GetBill(ctx context.Context) (*Bill, error) {
	url := c.config.ApiBaseUrl + "/api/billing/get_bill"
	resp, err := c.performRequest(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	subscription := &Bill{}
	err = json.NewDecoder(resp.Body).Decode(subscription)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json for bill: %w", err)
	}
	return subscription, nil
}

// Subscription implementation
func (c *DefaultEcloudClient) Subscribe(ctx context.Context, req *SubscribeRequest) (*Subscriber, error) {
	sub := &Subscriber{
		PatientID:      req.PatientID,
		PatientName:    req.PatientName,
		Email:          req.Email,
		RegisteredBy:   req.RegisteredBy,
		HospitalNumber: c.config.HospitalNumber,
		HospitalName:   c.config.HospitalName,
	}

	url := c.config.ApiBaseUrl + "/api/subscriptions"

	data, _ := json.Marshal(sub)
	resp, err := c.performRequest(ctx, http.MethodPost, url, bytes.NewReader(data), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to subscribe patient: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	// Decode subscription into same struct
	err = json.NewDecoder(resp.Body).Decode(sub)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json: %w", err)
	}
	return sub, nil
}

func (c *DefaultEcloudClient) GetSubscriber(ctx context.Context, subscriberID uint) (*Subscriber, error) {
	url := fmt.Sprintf("%s/api/subscriptions/%d", c.config.ApiBaseUrl, subscriberID)

	resp, err := c.performRequest(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch subscriber: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	subscriber := &Subscriber{}
	err = json.NewDecoder(resp.Body).Decode(subscriber)
	if err != nil {
		return nil, fmt.Errorf("unable to decode subscriber json: %w", err)
	}
	return subscriber, nil
}

func (c *DefaultEcloudClient) GetPatientSubscription(ctx context.Context, patientID uint) (*Subscriber, error) {
	url := fmt.Sprintf("%s/api/subscriptions/check_subscription/%s/%d",
		c.config.ApiBaseUrl, c.config.HospitalNumber, patientID)

	resp, err := c.performRequest(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch subscriber: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	subscriber := new(Subscriber)
	err = json.NewDecoder(resp.Body).Decode(subscriber)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json: %w", err)
	}
	return subscriber, nil
}

func (c *DefaultEcloudClient) GetHospitalSubscribers(ctx context.Context) ([]*Subscriber, error) {
	target := c.config.ApiBaseUrl + "/api/subscriptions?hospital_number=" + c.config.HospitalNumber
	resp, err := c.performRequest(ctx, http.MethodGet, target, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	var subscribers []*Subscriber
	err = json.NewDecoder(resp.Body).Decode(&subscribers)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json: %w", err)
	}
	return subscribers, nil
}

func (c *DefaultEcloudClient) GetPendingSubscribers(ctx context.Context) ([]*Subscriber, error) {
	url := fmt.Sprintf("%s/api/subscriptions/pending/%s", c.config.ApiBaseUrl, c.config.HospitalNumber)

	resp, err := c.performRequest(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch pending subscribers: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	var subscribers []*Subscriber
	err = json.NewDecoder(resp.Body).Decode(&subscribers)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json: %w", err)
	}
	return subscribers, nil
}

// Create or renew payment.
func (c *DefaultEcloudClient) CreatePayment(ctx context.Context, subscriberID uint, amountToPay float64, registeredBy string) (*Payment, error) {
	// validate the parameters
	if subscriberID == 0 {
		return nil, fmt.Errorf("subscriber id must not be zero")
	}
	if amountToPay < 0 {
		return nil, fmt.Errorf("amount to be paid must be greater then zero")
	}
	if registeredBy == "" {
		return nil, fmt.Errorf("eclinic user making the payment (registered_by) must not be empty")
	}

	// Initialize a valid payment
	payment := &Payment{
		SubscriberID: subscriberID,
		Amount:       amountToPay,
		RegisteredBy: registeredBy,
	}

	url := fmt.Sprintf("%s/api/payments", c.config.ApiBaseUrl)

	data, err := json.Marshal(payment)
	if err != nil {
		return nil, fmt.Errorf("json.Marshal error: %w", err)
	}

	resp, err := c.performRequest(ctx, http.MethodPost, url, bytes.NewReader(data), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to subscribe patient: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	// Decode subscription into same struct
	err = json.NewDecoder(resp.Body).Decode(payment)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json: %w", err)
	}
	return payment, nil
}

func (c *DefaultEcloudClient) GetSubscriberPayments(ctx context.Context, subscriberID uint) ([]*Payment, error) {
	url := fmt.Sprintf("%s/api/payments/list/%d", c.config.ApiBaseUrl, subscriberID)

	resp, err := c.performRequest(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch payments: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.decodeError(resp)
	}

	payments := []*Payment{}
	err = json.NewDecoder(resp.Body).Decode(&payments)
	if err != nil {
		return nil, fmt.Errorf("unable to decode json: %w", err)
	}
	return payments, nil
}

// Compile regex patterns once at package level
var (
	pdfHeaderPattern = regexp.MustCompile(`^%PDF-1\.\d`)
	pdfFooterPattern = regexp.MustCompile(`%%EOF\s*$`)
)

// isValidPDF checks if the provided byte slice contains a valid PDF file
func isValidPDF(data []byte) bool {
	// Check if data is empty
	if len(data) == 0 {
		return false
	}

	// Check PDF header (first 8 bytes should contain "%PDF-1." followed by a digit)
	if len(data) < 8 {
		return false
	}

	// PDF header pattern: %PDF-1.\d
	if !pdfHeaderPattern.Match(data[:8]) {
		return false
	}

	// Check PDF footer (should contain "%%EOF" somewhere at the end)
	// Check the last 1024 bytes to find the EOF marker
	endBytes := data[max(0, len(data)-1024):]
	if !pdfFooterPattern.Match(endBytes) {
		return false
	}

	// Check for startxref and cross-reference table
	if !bytes.Contains(data, []byte("startxref")) {
		return false
	}
	return true
}

const (
	labReportFieldName = "lab_report"
	labReportFileName  = "lab_report.pdf"

	medicalReportFieldName = "medical_report"
	medicalReportFileName  = "medical_report.pdf"
)

// Records implementation
func (c *DefaultEcloudClient) SyncMedicalRecords(ctx context.Context, patientRecord *PatientRecord) error {
	if err := patientRecord.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	var buffer bytes.Buffer
	var part io.Writer
	var err error

	// Create a new multipart request
	writer := multipart.NewWriter(&buffer)

	// If a medical report exists, add it to multipart request.
	if patientRecord.MedicalReport != nil {
		if !isValidPDF(patientRecord.LabReport) {
			return ErrInvalidMedicalReportPDF
		}
		part, err = writer.CreateFormFile(medicalReportFieldName, medicalReportFileName)
		if err != nil {
			return fmt.Errorf("error creating form file: %w", err)
		}

		_, err = part.Write(patientRecord.MedicalReport)
		if err != nil {
			return fmt.Errorf("error writing form file: %w", err)
		}
	}

	// If a lab report exists, add it to multipart request.
	if patientRecord.LabReport != nil {
		if !isValidPDF(patientRecord.LabReport) {
			return ErrInvalidLabReportPDF
		}

		part, err = writer.CreateFormFile(labReportFieldName, labReportFileName)
		if err != nil {
			return fmt.Errorf("error creating form file: %w", err)
		}

		_, err = part.Write(patientRecord.LabReport)
		if err != nil {
			return fmt.Errorf("error writing form file: %w", err)
		}
	}

	// We don't expect any errors here.
	_ = writer.WriteField("hospital_number", c.config.HospitalNumber)
	_ = writer.WriteField("visit_id", fmt.Sprintf("%d", patientRecord.VisitID))
	_ = writer.WriteField("subscriber_id", fmt.Sprintf("%d", patientRecord.SubscriberID))
	_ = writer.WriteField("visit_timestamp", patientRecord.VisitTimestamp.Format(time.RFC3339))
	_ = writer.WriteField("title", patientRecord.Title)

	// Close the multipart writer to flush.
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("error closing multipart writer: %w", err)
	}

	// Get content type header.
	contentType := writer.FormDataContentType()

	// Create custom headers to set content type for the form-data.
	headers := map[string]string{"Content-Type": contentType}

	// Construct upload url.
	url := c.config.ApiBaseUrl + "/api/records"

	// Perform the request
	resp, err := c.performRequest(ctx, http.MethodPost, url, bytes.NewReader(buffer.Bytes()), headers)
	if err != nil {
		return fmt.Errorf("unable to sync medical records: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return c.decodeError(resp)
	}
	return nil
}
