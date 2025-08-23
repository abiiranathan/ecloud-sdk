package ecloudsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (c *DefaultEcloudClient) performRequest(ctx context.Context, method, url string,
	body io.Reader, headers map[string]string) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response
	var maxRetries = c.retryPolicy.MaxRetries()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Create new request for each attempt
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return nil, err
		}

		// Add authentication header if available
		if c.jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.jwtToken)
		}

		// Add custom headers first
		if len(headers) > 0 {
			for key, value := range headers {
				req.Header.Set(key, value)
			}
		}

		// Set default headers if not provided
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}

		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}

		// Execute request
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			lastResp = resp

			if !c.retryPolicy.ShouldRetry(attempt, err, resp) {
				break
			}

			c.logger.Debug("request failed, retrying: %v", err)
			time.Sleep(c.retryPolicy.BackoffDuration(attempt))
			continue
		}

		// Handle authentication errors with token refresh
		if resp.StatusCode == http.StatusUnauthorized && c.authenticated {
			c.logger.Debug("received 401, attempting token refresh")
			if refreshErr := c.Refresh(ctx); refreshErr != nil {
				c.logger.Error("token refresh failed: %v", refreshErr)
				return resp, nil // Return the 401 response
			}

			// Retry with new token if we should retry
			if c.retryPolicy.ShouldRetry(attempt, nil, resp) {
				resp.Body.Close() // Close previous response body
				time.Sleep(c.retryPolicy.BackoffDuration(attempt))
				continue
			}
		}

		// Success or non-retryable error
		return resp, nil
	}

	// All retries exhausted
	if lastResp != nil {
		return lastResp, nil
	}
	return nil, lastErr
}

// JSONRespError encodes the response body returned by the API when there is an error.
type JSONRespError struct {
	Error string `json:"error"`
}

func (c *DefaultEcloudClient) decodeError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("statusCode=%d: failed to read response body: %w", resp.StatusCode, err)
	}

	var jsonErr JSONRespError
	if err := json.Unmarshal(body, &jsonErr); err == nil && jsonErr.Error != "" {
		return fmt.Errorf("statusCode=%d remote error: %s", resp.StatusCode, jsonErr.Error)
	}

	// fallback: plain text or unknown structure
	if len(body) == 0 {
		return fmt.Errorf("statusCode=%d remote error: empty response body", resp.StatusCode)
	}

	return fmt.Errorf("statusCode=%d remote error: %s", resp.StatusCode, string(body))
}
