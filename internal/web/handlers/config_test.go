package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozaktomas/photo-sorter/internal/config"
)

func TestNewConfigHandler(t *testing.T) {
	cfg := &config.Config{}

	handler := NewConfigHandler(cfg)

	if handler == nil {
		t.Fatal("expected non-nil handler")
		return
	}

	if handler.config != cfg {
		t.Error("expected handler to hold reference to config")
	}
}

func TestConfigHandler_Get_ReturnsJSON(t *testing.T) {
	cfg := &config.Config{}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	contentType := recorder.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestConfigHandler_Get_ReturnsOK(t *testing.T) {
	cfg := &config.Config{}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestConfigHandler_Get_ReturnsProviders(t *testing.T) {
	cfg := &config.Config{}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(result.Providers) != 4 {
		t.Errorf("expected 4 providers, got %d", len(result.Providers))
	}
}

func TestConfigHandler_Get_OpenAIAvailable(t *testing.T) {
	cfg := &config.Config{
		OpenAI: config.OpenAIConfig{
			Token: "sk-test-token",
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Find OpenAI provider
	var openaiProvider *ProviderInfo
	for i := range result.Providers {
		if result.Providers[i].Name == "openai" {
			openaiProvider = &result.Providers[i]
			break
		}
	}

	if openaiProvider == nil {
		t.Fatal("expected openai provider in response")
		return
	}

	if !openaiProvider.Available {
		t.Error("expected openai to be available when token is set")
	}
}

func TestConfigHandler_Get_OpenAINotAvailable(t *testing.T) {
	cfg := &config.Config{
		OpenAI: config.OpenAIConfig{
			Token: "", // Empty token
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	var openaiProvider *ProviderInfo
	for i := range result.Providers {
		if result.Providers[i].Name == "openai" {
			openaiProvider = &result.Providers[i]
			break
		}
	}

	if openaiProvider == nil {
		t.Fatal("expected openai provider in response")
		return
	}

	if openaiProvider.Available {
		t.Error("expected openai to be unavailable when token is empty")
	}
}

func TestConfigHandler_Get_GeminiAvailable(t *testing.T) {
	cfg := &config.Config{
		Gemini: config.GeminiConfig{
			APIKey: "gemini-api-key",
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	var geminiProvider *ProviderInfo
	for i := range result.Providers {
		if result.Providers[i].Name == "gemini" {
			geminiProvider = &result.Providers[i]
			break
		}
	}

	if geminiProvider == nil {
		t.Fatal("expected gemini provider in response")
		return
	}

	if !geminiProvider.Available {
		t.Error("expected gemini to be available when API key is set")
	}
}

func TestConfigHandler_Get_GeminiNotAvailable(t *testing.T) {
	cfg := &config.Config{
		Gemini: config.GeminiConfig{
			APIKey: "",
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	var geminiProvider *ProviderInfo
	for i := range result.Providers {
		if result.Providers[i].Name == "gemini" {
			geminiProvider = &result.Providers[i]
			break
		}
	}

	if geminiProvider == nil {
		t.Fatal("expected gemini provider in response")
		return
	}

	if geminiProvider.Available {
		t.Error("expected gemini to be unavailable when API key is empty")
	}
}

func TestConfigHandler_Get_LocalProvidersAlwaysAvailable(t *testing.T) {
	cfg := &config.Config{} // Empty config
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	localProviders := []string{"ollama", "llamacpp"}
	for _, name := range localProviders {
		var found *ProviderInfo
		for i := range result.Providers {
			if result.Providers[i].Name == name {
				found = &result.Providers[i]
				break
			}
		}

		if found == nil {
			t.Errorf("expected %s provider in response", name)
			continue
		}

		if !found.Available {
			t.Errorf("expected %s to always be available", name)
		}
	}
}

func TestConfigHandler_Get_PhotoPrismDomain(t *testing.T) {
	cfg := &config.Config{
		PhotoPrism: config.PhotoPrismConfig{
			Domain: "https://photos.example.com",
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.PhotoPrismDomain != "https://photos.example.com" {
		t.Errorf("expected domain 'https://photos.example.com', got '%s'", result.PhotoPrismDomain)
	}
}

func TestConfigHandler_Get_EmptyPhotoPrismDomain(t *testing.T) {
	cfg := &config.Config{
		PhotoPrism: config.PhotoPrismConfig{
			Domain: "",
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.PhotoPrismDomain != "" {
		t.Errorf("expected empty domain, got '%s'", result.PhotoPrismDomain)
	}
}

func TestConfigHandler_Get_AllProvidersConfigured(t *testing.T) {
	cfg := &config.Config{
		OpenAI: config.OpenAIConfig{
			Token: "openai-token",
		},
		Gemini: config.GeminiConfig{
			APIKey: "gemini-key",
		},
	}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// All providers should be available
	for _, provider := range result.Providers {
		if !provider.Available {
			t.Errorf("expected provider %s to be available", provider.Name)
		}
	}
}

func TestConfigHandler_Get_ProviderOrder(t *testing.T) {
	cfg := &config.Config{}
	handler := NewConfigHandler(cfg)

	req := httptest.NewRequest("GET", "/api/v1/config", nil)
	recorder := httptest.NewRecorder()

	handler.Get(recorder, req)

	var result ConfigResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &result)
	if err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify expected order
	expectedOrder := []string{"openai", "gemini", "ollama", "llamacpp"}
	for i, expected := range expectedOrder {
		if i >= len(result.Providers) {
			t.Errorf("missing provider at index %d", i)
			continue
		}
		if result.Providers[i].Name != expected {
			t.Errorf("expected provider '%s' at index %d, got '%s'", expected, i, result.Providers[i].Name)
		}
	}
}
