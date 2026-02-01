package config

import (
	_ "embed"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

//go:embed prices.yaml
var pricesYAML []byte

type Config struct {
	PhotoPrism PhotoPrismConfig
	OpenAI     OpenAIConfig
	Gemini     GeminiConfig
	Ollama     OllamaConfig
	LlamaCpp   LlamaCppConfig
	Embedding  EmbeddingConfig
	Database   DatabaseConfig
	Prices     PricesConfig
}

type PhotoPrismConfig struct {
	URL      string
	Username string
	Password string
	Domain   string // public domain for generating photo links (e.g., https://photos.example.com)
}

// PhotoURL returns an OSC 8 hyperlink for terminal emulators (iTerm2, etc.)
// Displays the UID but makes it clickable to open the photo in PhotoPrism
// Returns empty string if Domain is not set
func (c *PhotoPrismConfig) PhotoURL(uid string) string {
	if c.Domain == "" {
		return ""
	}
	url := c.Domain + "/library/browse?view=cards&order=oldest&q=uid:" + uid
	// OSC 8 hyperlink format: \e]8;;URL\e\\TEXT\e]8;;\e\\
	return "\x1b]8;;" + url + "\x1b\\" + uid + "\x1b]8;;\x1b\\"
}

type OpenAIConfig struct {
	Token string
}

type GeminiConfig struct {
	APIKey string
}

type OllamaConfig struct {
	URL   string // defaults to http://localhost:11434
	Model string // defaults to llama3.2-vision:11b
}

type LlamaCppConfig struct {
	URL   string // defaults to http://localhost:8080
	Model string // defaults to llava
}

type EmbeddingConfig struct {
	URL string // defaults to http://localhost:8000
	Dim int    // defaults to 768
}

type DatabaseConfig struct {
	URL                    string // PostgreSQL connection URL
	MaxOpenConns           int    // Maximum open connections (default 25)
	MaxIdleConns           int    // Maximum idle connections (default 5)
	HNSWIndexPath          string // Path to persist face HNSW index (optional, if empty index is rebuilt on startup)
	HNSWEmbeddingIndexPath string // Path to persist embedding HNSW index (optional, if empty index is rebuilt on startup)
}

type PricesConfig struct {
	Models map[string]ModelPricing `yaml:"models"`
}

type ModelPricing struct {
	Standard RequestPricing `yaml:"standard"`
	Batch    RequestPricing `yaml:"batch"`
}

type RequestPricing struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

func Load() *Config {
	var prices PricesConfig
	if err := yaml.Unmarshal(pricesYAML, &prices); err != nil {
		// Log error but continue - prices will be zero which is safe
		// This is an embedded file so this error should never happen in practice
		panic("failed to unmarshal embedded prices.yaml: " + err.Error())
	}

	// Parse embedding dimension with default
	embeddingDim := 768
	if dimStr := os.Getenv("EMBEDDING_DIM"); dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
			embeddingDim = d
		}
	}

	// Parse database pool settings with defaults
	dbMaxOpen := 25
	if s := os.Getenv("DATABASE_MAX_OPEN_CONNS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			dbMaxOpen = n
		}
	}
	dbMaxIdle := 5
	if s := os.Getenv("DATABASE_MAX_IDLE_CONNS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			dbMaxIdle = n
		}
	}

	return &Config{
		PhotoPrism: PhotoPrismConfig{
			URL:      os.Getenv("PHOTOPRISM_URL"),
			Username: os.Getenv("PHOTOPRISM_USERNAME"),
			Password: os.Getenv("PHOTOPRISM_PASSWORD"),
			Domain:   os.Getenv("PHOTOPRISM_DOMAIN"),
		},
		OpenAI: OpenAIConfig{
			Token: os.Getenv("OPENAI_TOKEN"),
		},
		Gemini: GeminiConfig{
			APIKey: os.Getenv("GEMINI_API_KEY"),
		},
		Ollama: OllamaConfig{
			URL:   os.Getenv("OLLAMA_URL"),
			Model: os.Getenv("OLLAMA_MODEL"),
		},
		LlamaCpp: LlamaCppConfig{
			URL:   os.Getenv("LLAMACPP_URL"),
			Model: os.Getenv("LLAMACPP_MODEL"),
		},
		Embedding: EmbeddingConfig{
			URL: os.Getenv("EMBEDDING_URL"),
			Dim: embeddingDim,
		},
		Database: DatabaseConfig{
			URL:                    os.Getenv("DATABASE_URL"),
			MaxOpenConns:           dbMaxOpen,
			MaxIdleConns:           dbMaxIdle,
			HNSWIndexPath:          os.Getenv("HNSW_INDEX_PATH"),
			HNSWEmbeddingIndexPath: os.Getenv("HNSW_EMBEDDING_INDEX_PATH"),
		},
		Prices: prices,
	}
}

// GetModelPricing returns pricing for a specific model, with fallback defaults
func (c *Config) GetModelPricing(modelName string) ModelPricing {
	if pricing, ok := c.Prices.Models[modelName]; ok {
		return pricing
	}
	// Return zero pricing if model not found
	return ModelPricing{}
}
