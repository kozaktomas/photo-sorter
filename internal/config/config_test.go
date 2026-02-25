package config

import (
	"os"
	"testing"
)

func TestPhotoURL_EmptyDomain(t *testing.T) {
	cfg := PhotoPrismConfig{
		Domain: "",
	}

	result := cfg.PhotoURL("photo123")

	if result != "" {
		t.Errorf("expected empty string for empty domain, got '%s'", result)
	}
}

func TestPhotoURL_WithDomain(t *testing.T) {
	cfg := PhotoPrismConfig{
		Domain: "https://photos.example.com",
	}

	result := cfg.PhotoURL("photo123")

	// Should contain the UID
	if result == "" {
		t.Error("expected non-empty result")
	}

	// Should contain OSC 8 escape sequences
	if result[0] != 0x1b {
		t.Error("expected result to start with escape sequence")
	}

	// Should contain the URL
	expectedURL := "https://photos.example.com/library/browse?view=cards&order=oldest&q=uid:photo123"
	if len(result) < len(expectedURL) {
		t.Errorf("result too short, expected to contain URL")
	}
}

func TestPhotoURL_ContainsUID(t *testing.T) {
	cfg := PhotoPrismConfig{
		Domain: "https://photos.example.com",
	}

	uid := "pt8abc123xyz"
	result := cfg.PhotoURL(uid)

	// The visible text should be just the UID
	// OSC 8 format: \e]8;;URL\e\\TEXT\e]8;;\e\\
	// So the UID should appear between the two escape sequences
	found := false
	for i := range len(result) - len(uid) {
		if result[i:i+len(uid)] == uid {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected result to contain UID '%s'", uid)
	}
}

func TestPhotoURL_CorrectFormat(t *testing.T) {
	cfg := PhotoPrismConfig{
		Domain: "https://photos.example.com",
	}

	result := cfg.PhotoURL("test123")

	// Verify OSC 8 start sequence exists: \x1b]8;;
	startSeq := "\x1b]8;;"
	if len(result) < len(startSeq) || result[:len(startSeq)] != startSeq {
		t.Error("expected result to start with OSC 8 sequence '\\x1b]8;;'")
	}

	// Verify end sequence exists: \x1b]8;;\x1b\\
	endSeq := "\x1b]8;;\x1b\\"
	if len(result) < len(endSeq) || result[len(result)-len(endSeq):] != endSeq {
		t.Error("expected result to end with OSC 8 close sequence")
	}
}

func TestGetModelPricing_KnownModel(t *testing.T) {
	cfg := Load() // Load actual config with embedded prices

	pricing := cfg.GetModelPricing("gpt-4.1-mini")

	// Should have non-zero pricing for standard mode
	if pricing.Standard.Input == 0 && pricing.Standard.Output == 0 {
		t.Error("expected non-zero pricing for gpt-4.1-mini standard mode")
	}

	// Verify expected values from prices.yaml
	if pricing.Standard.Input != 0.40 {
		t.Errorf("expected standard input price 0.40, got %f", pricing.Standard.Input)
	}

	if pricing.Standard.Output != 1.60 {
		t.Errorf("expected standard output price 1.60, got %f", pricing.Standard.Output)
	}
}

func TestGetModelPricing_BatchPricing(t *testing.T) {
	cfg := Load()

	pricing := cfg.GetModelPricing("gpt-4.1-mini")

	// Batch pricing should be 50% of standard
	if pricing.Batch.Input != 0.20 {
		t.Errorf("expected batch input price 0.20, got %f", pricing.Batch.Input)
	}

	if pricing.Batch.Output != 0.80 {
		t.Errorf("expected batch output price 0.80, got %f", pricing.Batch.Output)
	}
}

func TestGetModelPricing_GeminiModel(t *testing.T) {
	cfg := Load()

	pricing := cfg.GetModelPricing("gemini-2.5-flash")

	if pricing.Standard.Input != 0.30 {
		t.Errorf("expected gemini standard input 0.30, got %f", pricing.Standard.Input)
	}

	if pricing.Standard.Output != 2.50 {
		t.Errorf("expected gemini standard output 2.50, got %f", pricing.Standard.Output)
	}
}

func TestGetModelPricing_LocalModel(t *testing.T) {
	cfg := Load()

	pricing := cfg.GetModelPricing("llama3.2-vision")

	// Local models should have zero pricing
	if pricing.Standard.Input != 0 {
		t.Errorf("expected llama local model to have zero input price, got %f", pricing.Standard.Input)
	}

	if pricing.Standard.Output != 0 {
		t.Errorf("expected llama local model to have zero output price, got %f", pricing.Standard.Output)
	}
}

func TestGetModelPricing_UnknownModel(t *testing.T) {
	cfg := Load()

	pricing := cfg.GetModelPricing("unknown-model-xyz")

	// Unknown model should return zero pricing
	if pricing.Standard.Input != 0 || pricing.Standard.Output != 0 {
		t.Errorf("expected zero pricing for unknown model, got input=%f output=%f",
			pricing.Standard.Input, pricing.Standard.Output)
	}

	if pricing.Batch.Input != 0 || pricing.Batch.Output != 0 {
		t.Errorf("expected zero batch pricing for unknown model, got input=%f output=%f",
			pricing.Batch.Input, pricing.Batch.Output)
	}
}

func TestLoad_DefaultEmbeddingDim(t *testing.T) {
	// Clear any existing EMBEDDING_DIM
	os.Unsetenv("EMBEDDING_DIM")

	cfg := Load()

	if cfg.Embedding.Dim != 768 {
		t.Errorf("expected default embedding dim 768, got %d", cfg.Embedding.Dim)
	}
}

func TestLoad_CustomEmbeddingDim(t *testing.T) {
	// Set custom embedding dimension
	t.Setenv("EMBEDDING_DIM", "512")

	cfg := Load()

	if cfg.Embedding.Dim != 512 {
		t.Errorf("expected embedding dim 512, got %d", cfg.Embedding.Dim)
	}
}

func TestLoad_InvalidEmbeddingDim(t *testing.T) {
	// Set invalid embedding dimension (non-numeric)
	t.Setenv("EMBEDDING_DIM", "invalid")

	cfg := Load()

	// Should fall back to default
	if cfg.Embedding.Dim != 768 {
		t.Errorf("expected default embedding dim 768 for invalid input, got %d", cfg.Embedding.Dim)
	}
}

func TestLoad_NegativeEmbeddingDim(t *testing.T) {
	// Set negative embedding dimension
	t.Setenv("EMBEDDING_DIM", "-100")

	cfg := Load()

	// Should fall back to default (negative is invalid)
	if cfg.Embedding.Dim != 768 {
		t.Errorf("expected default embedding dim 768 for negative input, got %d", cfg.Embedding.Dim)
	}
}

func TestLoad_ZeroEmbeddingDim(t *testing.T) {
	// Set zero embedding dimension
	t.Setenv("EMBEDDING_DIM", "0")

	cfg := Load()

	// Should fall back to default (zero is invalid)
	if cfg.Embedding.Dim != 768 {
		t.Errorf("expected default embedding dim 768 for zero input, got %d", cfg.Embedding.Dim)
	}
}

func TestLoad_PhotoPrismConfig(t *testing.T) {
	t.Setenv("PHOTOPRISM_URL", "https://photos.test.com")
	t.Setenv("PHOTOPRISM_USERNAME", "testuser")
	t.Setenv("PHOTOPRISM_PASSWORD", "testpass")
	t.Setenv("PHOTOPRISM_DOMAIN", "https://public.photos.com")

	cfg := Load()

	if cfg.PhotoPrism.URL != "https://photos.test.com" {
		t.Errorf("expected URL 'https://photos.test.com', got '%s'", cfg.PhotoPrism.URL)
	}

	if cfg.PhotoPrism.Username != "testuser" {
		t.Errorf("expected username 'testuser', got '%s'", cfg.PhotoPrism.Username)
	}

	if cfg.PhotoPrism.GetPassword() != "testpass" {
		t.Errorf("expected password 'testpass', got '%s'", cfg.PhotoPrism.GetPassword())
	}

	if cfg.PhotoPrism.Domain != "https://public.photos.com" {
		t.Errorf("expected domain 'https://public.photos.com', got '%s'", cfg.PhotoPrism.Domain)
	}
}

func TestLoad_OpenAIConfig(t *testing.T) {
	t.Setenv("OPENAI_TOKEN", "sk-test-token-123")

	cfg := Load()

	if cfg.OpenAI.Token != "sk-test-token-123" {
		t.Errorf("expected OpenAI token 'sk-test-token-123', got '%s'", cfg.OpenAI.Token)
	}
}

func TestLoad_GeminiConfig(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gemini-api-key-456")

	cfg := Load()

	if cfg.Gemini.GetAPIKey() != "gemini-api-key-456" {
		t.Errorf("expected Gemini API key 'gemini-api-key-456', got '%s'", cfg.Gemini.GetAPIKey())
	}
}

func TestLoad_OllamaConfig(t *testing.T) {
	t.Setenv("OLLAMA_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_MODEL", "llava:13b")

	cfg := Load()

	if cfg.Ollama.URL != "http://localhost:11434" {
		t.Errorf("expected Ollama URL 'http://localhost:11434', got '%s'", cfg.Ollama.URL)
	}

	if cfg.Ollama.Model != "llava:13b" {
		t.Errorf("expected Ollama model 'llava:13b', got '%s'", cfg.Ollama.Model)
	}
}

func TestLoad_EmbeddingConfig(t *testing.T) {
	t.Setenv("EMBEDDING_URL", "http://localhost:8000")
	t.Setenv("EMBEDDING_DIM", "1024")

	cfg := Load()

	if cfg.Embedding.URL != "http://localhost:8000" {
		t.Errorf("expected Embedding URL 'http://localhost:8000', got '%s'", cfg.Embedding.URL)
	}

	if cfg.Embedding.Dim != 1024 {
		t.Errorf("expected Embedding dim 1024, got %d", cfg.Embedding.Dim)
	}
}

func TestLoad_PricesLoaded(t *testing.T) {
	cfg := Load()

	// Verify prices were loaded from embedded YAML
	if len(cfg.Prices.Models) == 0 {
		t.Error("expected prices to be loaded from embedded YAML")
	}

	// Should have at least the known models
	expectedModels := []string{"gpt-4.1-mini", "gemini-2.5-flash", "llama3.2-vision", "llava"}
	for _, model := range expectedModels {
		if _, ok := cfg.Prices.Models[model]; !ok {
			t.Errorf("expected model '%s' to be in prices", model)
		}
	}
}

func TestLoad_EmptyEnvVars(t *testing.T) {
	// Clear all relevant env vars
	os.Unsetenv("PHOTOPRISM_URL")
	os.Unsetenv("PHOTOPRISM_USERNAME")
	os.Unsetenv("OPENAI_TOKEN")
	os.Unsetenv("GEMINI_API_KEY")

	cfg := Load()

	// Should not panic, should return empty strings
	if cfg.PhotoPrism.URL != "" {
		t.Errorf("expected empty PhotoPrism URL, got '%s'", cfg.PhotoPrism.URL)
	}

	if cfg.OpenAI.Token != "" {
		t.Errorf("expected empty OpenAI token, got '%s'", cfg.OpenAI.Token)
	}
}
