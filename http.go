package ecloudsdk

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (c *DefaultEcloudClient) performRequest(ctx context.Context, method, url string,
	body io.Reader, headers map[string]string, gzipCompress ...bool) (*http.Response, error) {

	var lastErr error
	var lastResp *http.Response
	var maxRetries = c.retryPolicy.MaxRetries()
	var compressBody = true
	var isMultipart = false

	if headers != nil {
		ct := strings.ToLower(headers["Content-Type"])
		isMultipart = strings.HasPrefix(ct, "multipart/form-data")

	}

	if len(gzipCompress) > 0 && !isMultipart {
		compressBody = gzipCompress[0]
	}

	var reqBody io.Reader
	if compressBody && body != nil && !isMultipart {
		var buf = &bytes.Buffer{}
		gz := gzip.NewWriter(buf)
		if _, err := io.Copy(gz, body); err != nil {
			return nil, fmt.Errorf("failed to compress request body: %w", err)
		}
		if err := gz.Close(); err != nil {
			return nil, fmt.Errorf("failed to close gzip writer: %w", err)
		}
		reqBody = buf
	} else {
		reqBody = body
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return nil, err
		}

		// Add authentication
		if c.jwtToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.jwtToken)
		}

		// Add custom headers
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		// Set default headers
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}

		// Gzip settings
		if compressBody {
			req.Header.Set("Content-Encoding", "gzip")
		}

		req.Header.Set("Accept-Encoding", "gzip")

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

		// Handle 401 with token refresh
		if resp.StatusCode == http.StatusUnauthorized && c.authenticated {
			c.logger.Debug("received 401, attempting token refresh")
			if refreshErr := c.Refresh(ctx); refreshErr != nil {
				c.logger.Error("token refresh failed: %v", refreshErr)
				return resp, nil
			}
			if c.retryPolicy.ShouldRetry(attempt, nil, resp) {
				resp.Body.Close()
				time.Sleep(c.retryPolicy.BackoffDuration(attempt))
				continue
			}
		}

		return resp, nil
	}

	if lastResp != nil {
		return lastResp, nil
	}
	return nil, lastErr
}

// JSONRespError encodes the response body returned by the API when there is an error.
type JSONRespError struct {
	Error string `json:"error"`
}

func (c *DefaultEcloudClient) decodeError(resp io.Reader) error {
	body, err := io.ReadAll(resp)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var jsonErr JSONRespError
	if err := json.Unmarshal(body, &jsonErr); err == nil && jsonErr.Error != "" {
		return fmt.Errorf("remote error: %s", jsonErr.Error)
	}

	// fallback: plain text or unknown structure
	if len(body) == 0 {
		return errors.New("empty response body")
	}
	return fmt.Errorf("remote error: %s", string(body))
}
