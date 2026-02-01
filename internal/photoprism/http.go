package photoprism

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// doGetJSON performs a GET request and unmarshals the JSON response into the result type.
// The endpoint should be the path after the base API URL (e.g., "albums/123").
func doGetJSON[T any](pp *PhotoPrism, endpoint string) (*T, error) {
	url := pp.Url + "/" + endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
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

// doGetRaw performs a GET request and returns the raw response body.
// Useful for non-JSON responses like file downloads.
func doGetRaw(pp *PhotoPrism, endpoint string) ([]byte, string, error) {
	url := pp.Url + "/" + endpoint

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("could not read response body: %w", err)
	}

	return body, resp.Header.Get("Content-Type"), nil
}

// doPostJSON performs a POST request with a JSON body and unmarshals the JSON response.
func doPostJSON[T any](pp *PhotoPrism, endpoint string, requestBody interface{}) (*T, error) {
	return doRequestJSON[T](pp, "POST", endpoint, requestBody, http.StatusOK)
}

// doPostJSONCreated performs a POST request that accepts either 200 OK or 201 Created.
// Useful for endpoints that may return either status code.
func doPostJSONCreated[T any](pp *PhotoPrism, endpoint string, requestBody interface{}) (*T, error) {
	return doRequestJSONMultiStatus[T](pp, "POST", endpoint, requestBody, []int{http.StatusOK, http.StatusCreated})
}

// doPutJSON performs a PUT request with a JSON body and unmarshals the JSON response.
func doPutJSON[T any](pp *PhotoPrism, endpoint string, requestBody interface{}) (*T, error) {
	return doRequestJSON[T](pp, "PUT", endpoint, requestBody, http.StatusOK)
}

// doDelete performs a DELETE request.
func doDelete(pp *PhotoPrism, endpoint string) error {
	return doDeleteWithStatus(pp, endpoint, http.StatusOK)
}

// doDeleteWithStatus performs a DELETE request and accepts a specific status code.
func doDeleteWithStatus(pp *PhotoPrism, endpoint string, expectedStatus int) error {
	url := pp.Url + "/" + endpoint

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	return nil
}

// doDeleteJSON performs a DELETE request with a JSON body and returns the unmarshaled response.
func doDeleteJSON[T any](pp *PhotoPrism, endpoint string, requestBody interface{}) (*T, error) {
	return doRequestJSON[T](pp, "DELETE", endpoint, requestBody, http.StatusOK)
}

// doRequestJSON is the internal helper that performs HTTP requests with JSON body and response.
func doRequestJSON[T any](pp *PhotoPrism, method, endpoint string, requestBody interface{}, expectedStatus int) (*T, error) {
	url := pp.Url + "/" + endpoint

	var bodyReader io.Reader
	if requestBody != nil {
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("could not marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
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

// doRequestJSONMultiStatus performs HTTP requests accepting multiple valid status codes.
func doRequestJSONMultiStatus[T any](pp *PhotoPrism, method, endpoint string, requestBody interface{}, validStatuses []int) (*T, error) {
	url := pp.Url + "/" + endpoint

	var bodyReader io.Reader
	if requestBody != nil {
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("could not marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	validStatus := false
	for _, s := range validStatuses {
		if resp.StatusCode == s {
			validStatus = true
			break
		}
	}
	if !validStatus {
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

// doRequestRaw performs an HTTP request and returns the raw response body without JSON unmarshaling.
func doRequestRaw(pp *PhotoPrism, method, endpoint string, requestBody interface{}, expectedStatus int) ([]byte, error) {
	url := pp.Url + "/" + endpoint

	var bodyReader io.Reader
	if requestBody != nil {
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("could not marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", pp.token))
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	return body, nil
}
