package photoprism

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// doGetJSON performs a GET request and unmarshals the JSON response into the result type.
// The endpoint should be the path after the base API URL (e.g., "albums/123").
func doGetJSON[T any](pp *PhotoPrism, endpoint string) (*T, error) {
	url := pp.resolveURL(endpoint)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+pp.token)

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL constructed from validated parsedURL via resolveURL
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(endpoint, body)

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &result, nil
}

// doPostJSON performs a POST request with a JSON body and unmarshals the JSON response.
func doPostJSON[T any](pp *PhotoPrism, endpoint string, requestBody any) (*T, error) {
	return doRequestJSON[T](pp, "POST", endpoint, requestBody, http.StatusOK)
}

// doPostJSONCreated performs a POST request that accepts either 200 OK or 201 Created.
// Useful for endpoints that may return either status code.
func doPostJSONCreated[T any](pp *PhotoPrism, endpoint string, requestBody any) (*T, error) {
	return doRequestJSON[T](pp, "POST", endpoint, requestBody, http.StatusOK, http.StatusCreated)
}

// doPutJSON performs a PUT request with a JSON body and unmarshals the JSON response.
func doPutJSON[T any](pp *PhotoPrism, endpoint string, requestBody any) (*T, error) {
	return doRequestJSON[T](pp, "PUT", endpoint, requestBody, http.StatusOK)
}

// doDeleteJSON performs a DELETE request with a JSON body and returns the unmarshaled response.
func doDeleteJSON[T any](pp *PhotoPrism, endpoint string, requestBody any) (*T, error) {
	return doRequestJSON[T](pp, "DELETE", endpoint, requestBody, http.StatusOK)
}

// doRequestJSON is the internal helper that performs HTTP requests with JSON body and response.
// It accepts one or more valid status codes. If the response status doesn't match any, an error is returned.
func doRequestJSON[T any](pp *PhotoPrism, method, endpoint string, requestBody any, expectedStatuses ...int) (*T, error) {
	url := pp.resolveURL(endpoint)

	var bodyReader io.Reader
	if requestBody != nil {
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("could not marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+pp.token)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL constructed from validated parsedURL via resolveURL
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if !isExpectedStatus(resp.StatusCode, expectedStatuses) {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	pp.captureResponse(endpoint, body)

	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response: %w", err)
	}

	return &result, nil
}

// doRequestRaw performs an HTTP request without JSON unmarshaling the response.
func doRequestRaw(pp *PhotoPrism, method, endpoint string, requestBody any) error {
	url := pp.resolveURL(endpoint)

	var bodyReader io.Reader
	if requestBody != nil {
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("could not marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+pp.token)
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // URL constructed from validated parsedURL via resolveURL
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	return nil
}

// isExpectedStatus checks if a status code is in the list of expected statuses.
func isExpectedStatus(code int, expected []int) bool {
	return slices.Contains(expected, code)
}

// IsNotFoundError returns true if the error indicates a 404 Not Found response.
func IsNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status 404")
}
