package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/kozaktomas/photo-sorter/internal/config"
)

// Pool manages a PostgreSQL connection pool
type Pool struct {
	db   *sql.DB
	mu   sync.RWMutex
	once sync.Once
}

var (
	globalPool *Pool
	poolMu     sync.RWMutex
)

// NewPool creates a new PostgreSQL connection pool
func NewPool(cfg *config.DatabaseConfig) (*Pool, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)
	db.SetConnMaxIdleTime(10 * time.Minute)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Pool{db: db}, nil
}

// DB returns the underlying sql.DB for direct access
func (p *Pool) DB() *sql.DB {
	return p.db
}

// Close closes the connection pool
func (p *Pool) Close() error {
	if p.db != nil {
		return p.db.Close()
	}
	return nil
}

// SetGlobalPool sets the global pool instance and registers the backend
func SetGlobalPool(p *Pool) {
	poolMu.Lock()
	defer poolMu.Unlock()
	globalPool = p
}

// GetGlobalPool returns the global pool instance
func GetGlobalPool() *Pool {
	poolMu.RLock()
	defer poolMu.RUnlock()
	return globalPool
}

// IsAvailable returns true if a global pool is configured
func IsAvailable() bool {
	poolMu.RLock()
	defer poolMu.RUnlock()
	return globalPool != nil
}

// QueryRow executes a query that returns a single row
func (p *Pool) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return p.db.QueryRowContext(ctx, query, args...)
}

// Query executes a query that returns rows
func (p *Pool) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return p.db.QueryContext(ctx, query, args...)
}

// Exec executes a query that doesn't return rows
func (p *Pool) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return p.db.ExecContext(ctx, query, args...)
}

// BeginTx starts a transaction
func (p *Pool) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return p.db.BeginTx(ctx, opts)
}

// Initialize sets up the PostgreSQL backend with migrations and registers it
// as the active storage backend.
func Initialize(cfg *config.DatabaseConfig) error {
	if cfg == nil || cfg.URL == "" {
		return fmt.Errorf("database URL is required")
	}

	pool, err := NewPool(cfg)
	if err != nil {
		return fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	// Run migrations
	if err := pool.Migrate(context.Background()); err != nil {
		pool.Close()
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	SetGlobalPool(pool)
	return nil
}
