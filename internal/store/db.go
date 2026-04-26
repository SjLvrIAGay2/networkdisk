package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Store struct {
	DB *sql.DB
}

func New(dsn string, maxOpen, maxIdle int, connMaxLifetime time.Duration) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(connMaxLifetime)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) RunMigrations(dir string) error {
	if _, err := s.DB.ExecContext(context.Background(), "CREATE TABLE IF NOT EXISTS schema_versions (version INT NOT NULL PRIMARY KEY, filename VARCHAR(255) NOT NULL, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, checksum VARCHAR(64) NOT NULL, UNIQUE KEY uq_filename (filename)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"); err != nil {
		return fmt.Errorf("bootstrap schema_versions: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	type migration struct {
		version  int
		filename string
	}
	var files []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(e.Name(), "_", 2)
		if len(parts) < 2 {
			return fmt.Errorf("invalid migration filename %q: must use NNN_name.sql format", e.Name())
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration version in %q: %w", e.Name(), err)
		}
		files = append(files, migration{version: v, filename: e.Name()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].version < files[j].version
	})
	appliedRows, err := s.DB.QueryContext(context.Background(), "SELECT version, checksum FROM schema_versions ORDER BY version")
	if err != nil {
		return fmt.Errorf("query schema_versions: %w", err)
	}
	defer appliedRows.Close()
	applied := map[int]string{}
	for appliedRows.Next() {
		var v int
		var cs string
		if err := appliedRows.Scan(&v, &cs); err != nil {
			return fmt.Errorf("scan schema_versions: %w", err)
		}
		applied[v] = cs
	}
	if err := appliedRows.Err(); err != nil {
		return fmt.Errorf("iterate schema_versions: %w", err)
	}
	for _, f := range files {
		content, err := os.ReadFile(dir + "/" + f.filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f.filename, err)
		}
		contentStr := strings.TrimSpace(string(content))
		sum := fmt.Sprintf("%x", sha256.Sum256([]byte(contentStr)))
		if existingCS, ok := applied[f.version]; ok {
			if existingCS != sum {
				return fmt.Errorf("checksum mismatch for migration %s: stored=%s current=%s", f.filename, existingCS, sum)
			}
			continue
		}
		tx, err := s.DB.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", f.filename, err)
		}
		committed := false
		defer func() {
			if !committed {
				if err := tx.Rollback(); err != nil {
					fmt.Fprintf(os.Stderr, "rollback migration %s failed: %v\n", f.filename, err)
				}
			}
		}()
		for _, stmt := range strings.Split(contentStr, ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := tx.ExecContext(context.Background(), stmt); err != nil {
				return fmt.Errorf("exec %s: %w", f.filename, err)
			}
		}
		if _, err := tx.ExecContext(context.Background(), "INSERT INTO schema_versions (version, filename, checksum) VALUES (?, ?, ?)", f.version, f.filename, sum); err != nil {
			return fmt.Errorf("record migration %s: %w", f.filename, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", f.filename, err)
		}
		committed = true
	}
	return nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}
