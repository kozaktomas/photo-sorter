package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Pool manages a MariaDB connection pool.
type Pool struct {
	db *sql.DB
}

// NewPool creates a new MariaDB connection pool.
func NewPool(dsn string) (*Pool, error) {
	if dsn == "" {
		return nil, errors.New("MariaDB DSN is required")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open MariaDB: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping MariaDB: %w", err)
	}

	return &Pool{db: db}, nil
}

// Close closes the connection pool.
func (p *Pool) Close() error {
	if p.db != nil {
		if err := p.db.Close(); err != nil {
			return fmt.Errorf("closing database connection: %w", err)
		}
	}
	return nil
}
