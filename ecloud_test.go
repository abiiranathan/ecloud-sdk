// ecloud_test.go
package ecloudsdk

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockHTTPClient is a mock implementation of the HTTPClient interface.
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	// Default behavior if DoFunc is not set
	return nil, fmt.Errorf("mockHTTPClient.DoFunc is not set")
}

// newTestClient creates a new EcloudClient with a mock HTTP client for testing.
func newTestClient(doFunc func(req *http.Request) (*http.Response, error)) (EcloudClient, error) {
	config := &Config{
		ApiBaseUrl:     "http://testhost",
		EclinicId:      "test-id",
		Password:       "test-password",
		HospitalNumber: "HOS-123",
		HospitalName:   "Test Hospital",
		HTTPClient: &mockHTTPClient{
			DoFunc: doFunc,
		},
		Logger: &NoOpLogger{}, // Use a no-op logger to keep test output clean
	}
	return NewEcloudClient(config)
}

// helper function to create a valid response
func newJSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// A minimal valid PDF byte slice to pass the isValidPDF check.
var validPDFBytes = []byte("%PDF-1.7\n" +
	"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n" +
	"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n" +
	"3 0 obj << /Type /Page /MediaBox [0 0 612 792] >> endobj\n" +
	"xref\n0 4\n0000000000 65535 f \n0000000010 00000 n \n0000000059 00000 n \n0000000112 00000 n \n" +
	"trailer << /Size 4 /Root 1 0 R >>\n" +
	"startxref\n178\n" +
	"%%EOF")

func TestNewEcloudClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		config := &Config{
			ApiBaseUrl:     "http://testhost",
			EclinicId:      "test-id",
			Password:       "test-password",
			HospitalNumber: "HOS-123",
			HospitalName:   "Test Hospital",
		}
		client, err := NewEcloudClient(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if client == nil {
			t.Fatal("expected client to be non-nil")
		}
	})

	t.Run("Failure on Invalid Config", func(t *testing.T) {
		config := &Config{} // Empty config
		_, err := NewEcloudClient(config)
		if err == nil {
			t.Fatal("expected an error for invalid config, got nil")
		}
		if err != ErrApiBaseURLRequired {
			t.Fatalf("expected error %v, got %v", ErrApiBaseURLRequired, err)
		}
	})
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		mockResponse := `{"token": "fake-jwt-token", "user": {"id": 1, "eclinic_id": "test-id"}}`
		client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/auth/login" {
				return nil, fmt.Errorf("expected path /api/auth/login, got %s", req.URL.Path)
			}
			if req.Method != http.MethodPost {
				return nil, fmt.Errorf("expected method POST, got %s", req.Method)
			}
			return newJSONResponse(http.StatusOK, mockResponse), nil
		})

		resp, err := client.Login(ctx)
		if err != nil {
			t.Fatalf("Login() failed: %v", err)
		}

		if !client.IsAuthenticated() {
			t.Error("expected client to be authenticated")
		}
		if token := client.GetToken(); token != "fake-jwt-token" {
			t.Errorf("expected token 'fake-jwt-token', got '%s'", token)
		}
		if resp.Token != "fake-jwt-token" {
			t.Errorf("expected response token 'fake-jwt-token', got '%s'", resp.Token)
		}
		if resp.User.ID != 1 {
			t.Errorf("expected user ID 1, got %d", resp.User.ID)
		}
	})

	t.Run("Failure on 401 Unauthorized", func(t *testing.T) {
		mockResponse := `{"error": "invalid credentials"}`
		client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
			return newJSONResponse(http.StatusUnauthorized, mockResponse), nil
		})

		_, err := client.Login(ctx)
		if err == nil {
			t.Fatal("Login() should have failed but did not")
		}

		if client.IsAuthenticated() {
			t.Error("client should not be authenticated after a failed login")
		}
	})
}

func TestGetBill(t *testing.T) {
	ctx := context.Background()
	mockResponse := `{"Amount": 5000.0, "Duration": 2592000000000000}` // 30 days in nanoseconds

	client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/billing/get_bill" {
			return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
		}
		if req.Header.Get("Authorization") == "" {
			return newJSONResponse(http.StatusUnauthorized, `{"error":"missing token"}`), nil
		}
		return newJSONResponse(http.StatusOK, mockResponse), nil
	})

	// Manually set auth state to test protected endpoint
	if c, ok := client.(*DefaultEcloudClient); ok {
		c.jwtToken = "test-token"
		c.authenticated = true
	}

	bill, err := client.GetBill(ctx)
	if err != nil {
		t.Fatalf("GetBill() failed: %v", err)
	}

	if bill.Amount != 5000.0 {
		t.Errorf("expected bill amount 5000.0, got %f", bill.Amount)
	}
	expectedDuration := 30 * 24 * time.Hour
	if bill.Duration != expectedDuration {
		t.Errorf("expected bill duration %v, got %v", expectedDuration, bill.Duration)
	}
}

func TestSubscription(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
		// Mock a generic successful subscription response
		respBody := `{"id": 101, "patient_id": 12345, "patient_name": "John Doe", "hospital_number": "HOS-123"}`
		return newJSONResponse(http.StatusOK, respBody), nil
	})

	// Manually set auth state
	if c, ok := client.(*DefaultEcloudClient); ok {
		c.jwtToken = "test-token"
		c.authenticated = true
	}

	t.Run("Subscribe Patient", func(t *testing.T) {
		req := &SubscribeRequest{
			PatientID:    12345,
			PatientName:  "John Doe",
			RegisteredBy: "clerk01",
		}
		subscriber, err := client.Subscribe(ctx, req)
		if err != nil {
			t.Fatalf("Subscribe() failed: %v", err)
		}
		if subscriber.ID != 101 {
			t.Errorf("expected subscriber ID 101, got %d", subscriber.ID)
		}
		if subscriber.PatientName != "John Doe" {
			t.Errorf("expected patient name 'John Doe', got '%s'", subscriber.PatientName)
		}
	})
}

func TestPayment(t *testing.T) {
	ctx := context.Background()

	t.Run("CreatePayment Success", func(t *testing.T) {
		mockResponse := `{"id": 202, "subscriber_id": 101, "amount": 5000, "registered_by": "clerk01"}`
		client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/payments" {
				return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
			}
			return newJSONResponse(http.StatusOK, mockResponse), nil
		})

		if c, ok := client.(*DefaultEcloudClient); ok {
			c.jwtToken = "test-token"
		}

		payment, err := client.CreatePayment(ctx, 101, 5000, "clerk01")
		if err != nil {
			t.Fatalf("CreatePayment() failed: %v", err)
		}
		if payment.ID != 202 {
			t.Errorf("expected payment ID 202, got %d", payment.ID)
		}
	})

	t.Run("CreatePayment Client Side Validation", func(t *testing.T) {
		client, _ := newTestClient(nil) // HTTP client won't be called
		_, err := client.CreatePayment(ctx, 0, 5000, "clerk01")
		if err == nil {
			t.Error("expected error for zero subscriber ID, got nil")
		}
		_, err = client.CreatePayment(ctx, 101, -100, "clerk01")
		if err == nil {
			t.Error("expected error for negative amount, got nil")
		}
		_, err = client.CreatePayment(ctx, 101, 5000, "")
		if err == nil {
			t.Error("expected error for empty registered_by, got nil")
		}
	})
}

func TestSyncMedicalRecords(t *testing.T) {
	ctx := context.Background()

	t.Run("Success with both reports", func(t *testing.T) {
		visitTime := time.Now()
		patientRecord := &PatientRecord{
			VisitID:        999,
			SubscriberID:   101,
			Title:          "Annual Checkup",
			VisitTimestamp: visitTime,
			MedicalReport:  validPDFBytes,
			LabReport:      validPDFBytes,
		}

		client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/api/records" {
				return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
			}
			if !strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data") {
				return nil, fmt.Errorf("expected multipart/form-data content type, got %s", req.Header.Get("Content-Type"))
			}

			err := req.ParseMultipartForm(10 << 20) // 10MB
			if err != nil {
				return nil, fmt.Errorf("failed to parse multipart form: %w", err)
			}

			if v := req.FormValue("visit_id"); v != "999" {
				t.Errorf("expected visit_id '999', got '%s'", v)
			}
			if v := req.FormValue("subscriber_id"); v != "101" {
				t.Errorf("expected subscriber_id '101', got '%s'", v)
			}
			if v := req.FormValue("title"); v != "Annual Checkup" {
				t.Errorf("expected title 'Annual Checkup', got '%s'", v)
			}
			if v := req.FormValue("hospital_number"); v != "HOS-123" {
				t.Errorf("expected hospital_number 'HOS-123', got '%s'", v)
			}

			// Check lab report file
			labFile, _, err := req.FormFile(labReportFieldName)
			if err != nil {
				t.Fatalf("expected file '%s', but not found: %v", labReportFieldName, err)
			}
			defer labFile.Close()
			labData, _ := io.ReadAll(labFile)
			if !bytes.Equal(labData, validPDFBytes) {
				t.Error("lab report content mismatch")
			}

			// Check medical report file
			medFile, _, err := req.FormFile(medicalReportFieldName)
			if err != nil {
				t.Fatalf("expected file '%s', but not found: %v", medicalReportFieldName, err)
			}
			defer medFile.Close()
			medData, _ := io.ReadAll(medFile)
			if !bytes.Equal(medData, validPDFBytes) {
				t.Error("medical report content mismatch")
			}

			return newJSONResponse(http.StatusOK, `{"status": "ok"}`), nil
		})

		// Set auth state
		if c, ok := client.(*DefaultEcloudClient); ok {
			c.jwtToken = "test-token"
		}

		err := client.SyncMedicalRecords(ctx, patientRecord)
		if err != nil {
			t.Fatalf("SyncMedicalRecords() failed: %v", err)
		}
	})

	t.Run("Failure on invalid PDF data", func(t *testing.T) {
		patientRecord := &PatientRecord{
			VisitID:        999,
			SubscriberID:   101,
			Title:          "Annual Checkup",
			VisitTimestamp: time.Now(),
			LabReport:      []byte("this is not a pdf"),
		}

		// Mock client's DoFunc should not be called due to client-side validation
		client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
			t.Fatal("http.Do should not have been called for client-side validation failure")
			return nil, nil
		})

		err := client.SyncMedicalRecords(ctx, patientRecord)
		if err == nil {
			t.Fatal("expected an error for invalid PDF, but got nil")
		}

		if err != ErrInvalidLabReportPDF {
			t.Errorf("expected error %v, got %v", ErrInvalidLabReportPDF, err)
		}
	})

	t.Run("Failure on server error", func(t *testing.T) {
		patientRecord := &PatientRecord{
			VisitID:        999,
			SubscriberID:   101,
			Title:          "Annual Checkup",
			VisitTimestamp: time.Now(),
			LabReport:      validPDFBytes,
		}
		client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
			return newJSONResponse(http.StatusInternalServerError, `{"error":"server processing failed"}`), nil
		})
		if c, ok := client.(*DefaultEcloudClient); ok {
			c.jwtToken = "test-token"
		}

		err := client.SyncMedicalRecords(ctx, patientRecord)
		if err == nil {
			t.Fatal("expected an error for server failure, but got nil")
		}
	})
}

// Test for one of the getter methods to ensure the pattern works
func TestGetHospitalSubscribers(t *testing.T) {
	ctx := context.Background()
	mockResponse := `[
		{"id": 1, "patient_name": "Alice"},
		{"id": 2, "patient_name": "Bob"}
	]`
	client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
		expectedPath := "/api/subscriptions"
		if req.URL.Path != expectedPath {
			return nil, fmt.Errorf("expected path '%s', got '%s'", expectedPath, req.URL.Path)
		}
		if q := req.URL.Query().Get("hospital_number"); q != "HOS-123" {
			return nil, fmt.Errorf("expected query param 'hospital_number=HOS-123', got '%s'", q)
		}
		return newJSONResponse(http.StatusOK, mockResponse), nil
	})
	if c, ok := client.(*DefaultEcloudClient); ok {
		c.jwtToken = "test-token"
	}

	subscribers, err := client.GetHospitalSubscribers(ctx)
	if err != nil {
		t.Fatalf("GetHospitalSubscribers() failed: %v", err)
	}

	if len(subscribers) != 2 {
		t.Fatalf("expected 2 subscribers, got %d", len(subscribers))
	}
	if subscribers[0].PatientName != "Alice" {
		t.Errorf("expected first subscriber name 'Alice', got '%s'", subscribers[0].PatientName)
	}
	if subscribers[1].PatientName != "Bob" {
		t.Errorf("expected second subscriber name 'Bob', got '%s'", subscribers[1].PatientName)
	}
}

func TestGetHospitalSubscribersGZIP(t *testing.T) {
	ctx := context.Background()
	mockResponse := `[
		{"id": 1, "patient_name": "Alice"},
		{"id": 2, "patient_name": "Bob"}
	]`

	// Compress the mock response to GZIP
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte(mockResponse))
	if err != nil {
		t.Fatalf("failed to write gzip data: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	client, _ := newTestClient(func(req *http.Request) (*http.Response, error) {
		expectedPath := "/api/subscriptions"
		if req.URL.Path != expectedPath {
			return nil, fmt.Errorf("expected path '%s', got '%s'", expectedPath, req.URL.Path)
		}
		if q := req.URL.Query().Get("hospital_number"); q != "HOS-123" {
			return nil, fmt.Errorf("expected query param 'hospital_number=HOS-123', got '%s'", q)
		}
		req.Header.Set("Accept-Encoding", "gzip")

		// Return gzipped response
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Encoding": []string{"gzip"}},
			Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
		}, nil
	})

	if c, ok := client.(*DefaultEcloudClient); ok {
		c.jwtToken = "test-token"
	}

	subscribers, err := client.GetHospitalSubscribers(ctx)
	if err != nil {
		t.Fatalf("GetHospitalSubscribers() failed: %v", err)
	}

	if len(subscribers) != 2 {
		t.Fatalf("expected 2 subscribers, got %d", len(subscribers))
	}
	if subscribers[0].PatientName != "Alice" {
		t.Errorf("expected first subscriber name 'Alice', got '%s'", subscribers[0].PatientName)
	}
	if subscribers[1].PatientName != "Bob" {
		t.Errorf("expected second subscriber name 'Bob', got '%s'", subscribers[1].PatientName)
	}
}
