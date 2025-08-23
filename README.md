# Ecloud Go SDK

[![Go Report Card](https://goreportcard.com/badge/github.com/abiiranathan/ecloud-sdk)](https://goreportcard.com/report/github.com/abiiranathan/ecloud-sdk)
[![Go.Dev reference](https://img.shields.io/badge/go.dev-reference-blue?logo=go&logoColor=white)](https://pkg.go.dev/github.com/abiiranathan/ecloud-sdk)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

The official Go SDK for the Ecloud API. It provides a convenient, type-safe way to interact with Ecloud services for managing patient subscriptions, payments, billing, and medical records synchronization from your Eclinic HMS application.

## Table of Contents

- [Ecloud Go SDK](#ecloud-go-sdk)
  - [Table of Contents](#table-of-contents)
  - [Features](#features)
  - [Installation](#installation)
  - [Getting Started](#getting-started)
    - [1. Initialize the Client](#1-initialize-the-client)
    - [2. Authenticate](#2-authenticate)
  - [Usage Examples](#usage-examples)
    - [Subscription Management](#subscription-management)
      - [Subscribe a New Patient](#subscribe-a-new-patient)
      - [Get Subscriber Details](#get-subscriber-details)
    - [Payment Processing](#payment-processing)
      - [Create a Payment for a Subscription](#create-a-payment-for-a-subscription)
    - [Syncing Medical Records](#syncing-medical-records)
    - [Billing](#billing)
      - [Get Current Bill](#get-current-bill)
  - [Advanced Configuration](#advanced-configuration)
    - [Custom HTTP Client](#custom-http-client)
    - [Custom Logger](#custom-logger)
    - [Custom Retry Policy](#custom-retry-policy)
  - [Error Handling](#error-handling)
  - [Contributing](#contributing)
  - [License](#license)

## Features

- **Authentication**: Simple login and automatic JWT token handling for all authenticated requests.
- **Subscription Management**: Create, retrieve, and manage patient subscriptions.
- **Payment Processing**: Create payments for subscriptions and fetch payment history.
- **Medical Records Sync**: Securely upload patient medical and lab reports (PDFs) via multipart/form-data requests.
- **Billing**: Fetch current billing information.
- **Extensible**:
  - Pluggable `HTTPClient` for custom transport, timeouts, or middleware.
  - Pluggable `Logger` interface to integrate with your application's logging solution (e.g., `slog`, `logrus`).
  - Configurable `RetryPolicy` with exponential backoff for handling transient network errors and 401 token refreshes.

## Installation

To install the Ecloud Go SDK, use `go get`:

```sh
go get github.com/abiiranathan/ecloud-sdk
```

## Getting Started

### 1. Initialize the Client

First, create a `Config` object with your Ecloud credentials and initialize the client.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/abiiranathan/ecloud-sdk"
)

func main() {
	config := &ecloudsdk.Config{
		ApiBaseUrl:     "https://api.ecloud.com", // Or the appropriate staging URL
		EclinicId:      "YOUR_ECLINIC_ID",
		Password:       "YOUR_PASSWORD",
		HospitalNumber: "YOUR_HOSPITAL_NUMBER",
		HospitalName:   "Your Hospital Name",
	}

	client, err := ecloudsdk.NewEcloudClient(config)
	if err != nil {
		log.Fatalf("Failed to create Ecloud client: %v", err)
	}
	
	// Client is ready to use
}
```

### 2. Authenticate

Before making calls to protected endpoints, you must authenticate by calling `Login`. The client will automatically store the JWT token and use it for subsequent requests.

```go
	ctx := context.Background()

	loginResponse, err := client.Login(ctx)
	if err != nil {
		log.Fatalf("Login failed: %v", err)
	}

	fmt.Printf("Successfully logged in as user: %s\n", loginResponse.User.EclinicID)

	// You can now access authenticated methods
	user, _ := client.GetUser()
	fmt.Printf("Retrieved user ID from client state: %d\n", user.ID)
```

## Usage Examples

All examples assume you have an initialized and authenticated `client`.

### Subscription Management

#### Subscribe a New Patient

```go
subReq := &ecloudsdk.SubscribeRequest{
	PatientID:    12345,
	PatientName:  "Jane Doe",
	Email:        "jane.doe@example.com",
	RegisteredBy: "clerk_username",
}

subscriber, err := client.Subscribe(ctx, subReq)
if err != nil {
	log.Fatalf("Failed to subscribe patient: %v", err)
}

fmt.Printf("Successfully subscribed patient. Subscriber ID: %d\n", subscriber.ID)
```

#### Get Subscriber Details

```go
subscriberID := uint(101)
subscriber, err := client.GetSubscriber(ctx, subscriberID)
if err != nil {
	log.Fatalf("Failed to get subscriber: %v", err)
}
fmt.Printf("Fetched subscriber: %s\n", subscriber.PatientName)
```

### Payment Processing

#### Create a Payment for a Subscription

```go
subscriberID := uint(101)
amount := 5000.00
registeredBy := "finance_user"

payment, err := client.CreatePayment(ctx, subscriberID, amount, registeredBy)
if err != nil {
	log.Fatalf("Failed to create payment: %v", err)
}

fmt.Printf("Payment created successfully. Payment ID: %d, Valid Until: %s\n", payment.ID, payment.ValidTo)
```

### Syncing Medical Records

The `SyncMedicalRecords` method uploads one or both of a medical report and a lab report. The files must be valid PDFs provided as byte slices (`[]byte`).

```go
import "os"

// Read your PDF files into byte slices
medicalReportBytes, err := os.ReadFile("path/to/medical_report.pdf")
if err != nil {
    log.Fatalf("Failed to read medical report: %v", err)
}

labReportBytes, err := os.ReadFile("path/to/lab_report.pdf")
if err != nil {
    log.Fatalf("Failed to read lab report: %v", err)
}

patientRecord := &ecloudsdk.PatientRecord{
	VisitID:        909,
	SubscriberID:   101,
	Title:          "Annual Physical Examination",
	VisitTimestamp: time.Now(),
	MedicalReport:  medicalReportBytes, // Can be nil if not available
	LabReport:      labReportBytes,     // Can be nil if not available but both can't be nil
}

err = client.SyncMedicalRecords(ctx, patientRecord)
if err != nil {
	log.Fatalf("Failed to sync medical records: %v", err)
}

fmt.Println("Medical records synced successfully!")
```

### Billing

#### Get Current Bill

```go
bill, err := client.GetBill(ctx)
if err != nil {
	log.Fatalf("Failed to get bill: %v", err)
}
fmt.Printf("Current subscription amount: %.2f for duration: %v\n", bill.Amount, bill.Duration)
```

## Advanced Configuration

The SDK is designed to be flexible. You can customize its behavior by providing your own implementations for HTTP, logging, and retries.

### Custom HTTP Client

You can provide your own `http.Client` to control transports, proxies, or add middleware.

```go
customHttpClient := &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		// Custom transport settings
	},
}

config := &ecloudsdk.Config{
    HTTPClient: customHttpClient,
}

client, _ := ecloudsdk.NewEcloudClient(config)
```

### Custom Logger

The SDK uses a `Logger` interface. You can provide your own implementation to integrate with your application's logging framework (e.g., `slog`, `logrus`, `zap`).

```go
import "log/slog"

type SlogAdapter struct {
	logger *slog.Logger
}

func (s *SlogAdapter) Debug(msg string, args ...any) { s.logger.Debug(msg, args...) }
func (s *SlogAdapter) Info(msg string, args ...any)  { s.logger.Info(msg, args...) }
func (s *SlogAdapter) Error(msg string, args ...any) { s.logger.Error(msg, args...) }

config := &ecloudsdk.Config{
    Logger: &SlogAdapter{logger: slog.Default()},
}
```

### Custom Retry Policy

Implement the `RetryPolicy` interface to define custom logic for when and how to retry failed requests.

```go
type MyConstantBackoffPolicy struct{}
func (p *MyConstantBackoffPolicy) ShouldRetry(attempt int, err error, resp *http.Response) bool { /* ... */ }
func (p *MyConstantBackoffPolicy) BackoffDuration(attempt int) time.Duration { return 2 * time.Second }
func (p *MyConstantBackoffPolicy) MaxRetries() int { return 5 }

config := &ecloudsdk.Config{
    // ... other fields
    RetryPolicy: &MyConstantBackoffPolicy{},
}
```

## Error Handling

Methods in the SDK return an `error` as the second return value.

- **Validation Errors**: Client-side validation errors (e.g., missing required fields) are returned before an HTTP request is made.
- **API Errors**: If the Ecloud API returns an error, the SDK decodes the JSON error message and wraps it in a Go error. The error message will typically include the HTTP status code and the error message from the server.

Example:
`statusCode=401 remote error: invalid credentials`

- **Pre-defined Errors**: The SDK includes several pre-defined errors for common states:
  - `ecloudsdk.ErrNotAuthenticated`
  - `ecloudsdk.ErrInvalidConfig`
  - `ecloudsdk.ErrInvalidMedicalReportPDF`

## Contributing

Contributions are welcome! Please feel free to submit a pull request.

1.  Fork the repository.
2.  Create a new feature branch (`git checkout -b feature/my-new-feature`).
3.  Commit your changes (`git commit -am 'Add some feature'`).
4.  Push to the branch (`git push origin feature/my-new-feature`).
5.  Create a new Pull Request.

## License

This SDK is distributed under the MIT license. See the [LICENSE](LICENSE) file for more information.
