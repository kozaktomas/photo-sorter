package config

import (
	_ "embed"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

//go:embed prices.yaml
var pricesYAML []byte

// Config holds all application configuration loaded from environment variables.
type Config struct {
	PhotoPrism PhotoPrismConfig
	OpenAI     OpenAIConfig
	Gemini     GeminiConfig
	Ollama     OllamaConfig
	LlamaCpp   LlamaCppConfig
	Embedding  EmbeddingConfig
	Database   DatabaseConfig
	Storage    StorageConfig
	Duplicate  DuplicateConfig
	Prices     PricesConfig
}

// PhotoPrismConfig holds PhotoPrism API connection settings.
type PhotoPrismConfig struct {
	URL         string
	Username    string
	password    string
	Domain      string // public domain for generating photo links (e.g., https://photos.example.com)
	DatabaseURL string // MariaDB DSN for direct database access (e.g., photoprism:photoprism@tcp(mariadb:3306)/photoprism)
}

// GetPassword returns the PhotoPrism password.
func (c *PhotoPrismConfig) GetPassword() string { return c.password }

// PhotoURL returns an OSC 8 hyperlink for terminal emulators (iTerm2, etc.).
// Displays the UID but makes it clickable to open the photo in PhotoPrism.
// Returns empty string if Domain is not set.
func (c *PhotoPrismConfig) PhotoURL(uid string) string {
	if c.Domain == "" {
		return ""
	}
	url := c.Domain + "/library/browse?view=cards&order=oldest&q=uid:" + uid
	// OSC 8 hyperlink format: \e]8;;URL\e\\TEXT\e]8;;\e\\.
	return "\x1b]8;;" + url + "\x1b\\" + uid + "\x1b]8;;\x1b\\"
}

// OpenAIConfig holds OpenAI API credentials.
type OpenAIConfig struct {
	Token string
}

// GeminiConfig holds Google Gemini API credentials.
type GeminiConfig struct {
	apiKey string
}

// NewGeminiConfig creates a GeminiConfig with the given API key.
func NewGeminiConfig(apiKey string) GeminiConfig { return GeminiConfig{apiKey: apiKey} }

// GetAPIKey returns the Gemini API key.
func (c *GeminiConfig) GetAPIKey() string { return c.apiKey }

// OllamaConfig holds Ollama server connection settings.
type OllamaConfig struct {
	URL   string // defaults to http://localhost:11434
	Model string // defaults to llama3.2-vision:11b
}

// LlamaCppConfig holds llama.cpp server connection settings.
type LlamaCppConfig struct {
	URL   string // defaults to http://localhost:8080
	Model string // defaults to llava
}

// EmbeddingConfig holds embeddings service connection settings.
type EmbeddingConfig struct {
	URL string // defaults to http://localhost:8000
	Dim int    // defaults to 768
}

// DatabaseConfig holds PostgreSQL database connection settings.
type DatabaseConfig struct {
	URL                    string // PostgreSQL connection URL
	MaxOpenConns           int    // Maximum open connections (default 25)
	MaxIdleConns           int    // Maximum idle connections (default 5)
	HNSWIndexPath          string // Path to persist face HNSW index (optional, if empty index is rebuilt on startup)
	HNSWEmbeddingIndexPath string // Path to persist embedding HNSW index (optional, if empty index is rebuilt on startup)
}

// StorageConfig holds the on-disk locations for photo originals and the
// thumbnail cache (mirroring the PhotoPrism layout).
type StorageConfig struct {
	OriginalsPath string // Root directory for original photo files (default: /data/originals).
	CachePath     string // Root directory for the cache (default: /data/cache). Thumbnails live in <CachePath>/thumb/.
}

// DuplicateConfig holds tuning for the upload-time near-duplicate detector.
// PHashMaxDiff is the max hamming distance between two 64-bit pHashes
// (0..64). EmbeddingMaxDistance is the max cosine distance between CLIP
// image embeddings (0..2; 0 = identical, 2 = opposite). Enabled gates the
// whole detector — when false, uploads skip the pHash + embedding scan
// even if the per-request flag is set.
type DuplicateConfig struct {
	Enabled              bool
	PHashMaxDiff         int
	EmbeddingMaxDistance float64
}

// PricesConfig holds model pricing data loaded from prices.yaml.
type PricesConfig struct {
	Models map[string]ModelPricing `yaml:"models"`
}

// ModelPricing holds standard and batch pricing for a single AI model.
type ModelPricing struct {
	Standard RequestPricing `yaml:"standard"`
	Batch    RequestPricing `yaml:"batch"`
}

// RequestPricing holds per-token pricing for input and output.
type RequestPricing struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

// envInt reads an environment variable and parses it as a positive integer.
// Returns the default value if the env var is unset, empty, or invalid.
func envInt(key string, defaultVal int) int {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return defaultVal
}

// envString returns the value of key or defaultVal if unset/empty.
func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envBool returns the boolean value of key or defaultVal if unset/empty/
// unparseable. Accepts the strconv.ParseBool aliases: 1/0, t/f, T/F, true/
// false, TRUE/FALSE, True/False.
func envBool(key string, defaultVal bool) bool {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	if v, err := strconv.ParseBool(s); err == nil {
		return v
	}
	return defaultVal
}

// envFloat returns the float value of key or defaultVal if unset/empty/
// unparseable or non-positive.
func envFloat(key string, defaultVal float64) float64 {
	s := os.Getenv(key)
	if s == "" {
		return defaultVal
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
		return v
	}
	return defaultVal
}

// Load reads all configuration from environment variables and returns a Config.
func Load() *Config {
	var prices PricesConfig
	if err := yaml.Unmarshal(pricesYAML, &prices); err != nil {
		// Log error but continue - prices will be zero which is safe.
		// This is an embedded file so this error should never happen in practice.
		panic("failed to unmarshal embedded prices.yaml: " + err.Error())
	}

	return &Config{
		PhotoPrism: PhotoPrismConfig{
			URL:         os.Getenv("PHOTOPRISM_URL"),
			Username:    os.Getenv("PHOTOPRISM_USERNAME"),
			password:    os.Getenv("PHOTOPRISM_PASSWORD"),
			Domain:      os.Getenv("PHOTOPRISM_DOMAIN"),
			DatabaseURL: os.Getenv("PHOTOPRISM_DATABASE_URL"),
		},
		OpenAI: OpenAIConfig{
			Token: os.Getenv("OPENAI_TOKEN"),
		},
		Gemini: GeminiConfig{
			apiKey: os.Getenv("GEMINI_API_KEY"),
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
			Dim: envInt("EMBEDDING_DIM", 768),
		},
		Database: DatabaseConfig{
			URL:                    os.Getenv("DATABASE_URL"),
			MaxOpenConns:           envInt("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:           envInt("DATABASE_MAX_IDLE_CONNS", 5),
			HNSWIndexPath:          os.Getenv("HNSW_INDEX_PATH"),
			HNSWEmbeddingIndexPath: os.Getenv("HNSW_EMBEDDING_INDEX_PATH"),
		},
		Storage: StorageConfig{
			OriginalsPath: envString("STORAGE_ORIGINALS_PATH", "/data/originals"),
			CachePath:     envString("STORAGE_CACHE_PATH", "/data/cache"),
		},
		Duplicate: DuplicateConfig{
			Enabled:              envBool("DUPLICATE_CHECK_ENABLED", true),
			PHashMaxDiff:         envInt("DUPLICATE_PHASH_MAX_DIFF", 8),
			EmbeddingMaxDistance: envFloat("DUPLICATE_EMBEDDING_MAX_DIST", 0.05),
		},
		Prices: prices,
	}
}

// GetModelPricing returns pricing for a specific model, with fallback defaults.
func (c *Config) GetModelPricing(modelName string) ModelPricing {
	if pricing, ok := c.Prices.Models[modelName]; ok {
		return pricing
	}
	// Return zero pricing if model not found.
	return ModelPricing{}
}
