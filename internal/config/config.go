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
	Postgres   PostgresConfig
	Embedding  EmbeddingConfig
	Prices     PricesConfig
}

type PhotoPrismConfig struct {
	URL      string
	Username string
	Password string
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

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

type EmbeddingConfig struct {
	URL string // defaults to http://100.94.61.29:8000
	Dim int    // defaults to 768
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
	_ = yaml.Unmarshal(pricesYAML, &prices)

	// Parse embedding dimension with default
	embeddingDim := 768
	if dimStr := os.Getenv("EMBEDDING_DIM"); dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil && d > 0 {
			embeddingDim = d
		}
	}

	return &Config{
		PhotoPrism: PhotoPrismConfig{
			URL:      os.Getenv("PHOTOPRISM_URL"),
			Username: os.Getenv("PHOTOPRISM_USERNAME"),
			Password: os.Getenv("PHOTOPRISM_PASSWORD"),
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
		Postgres: PostgresConfig{
			Host:     os.Getenv("POSTGRES_HOST"),
			Port:     os.Getenv("POSTGRES_PORT"),
			User:     os.Getenv("POSTGRES_USER"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Database: os.Getenv("POSTGRES_DB"),
		},
		Embedding: EmbeddingConfig{
			URL: os.Getenv("EMBEDDING_URL"),
			Dim: embeddingDim,
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
