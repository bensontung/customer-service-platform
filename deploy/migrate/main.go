package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

func main() {
	dir := flag.String("dir", "deploy/migrations", "migration directory")
	flag.Parse()
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		panic("MYSQL_DSN is required")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		panic(err)
	}
	config.MultiStatements = true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		panic(err)
	}
	if err = applyMigrations(db, *dir, os.Stdout); err != nil {
		panic(err)
	}
}

func applyMigrations(db *sql.DB, dir string, output io.Writer) error {
	connection, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	var locked bool
	if err = connection.QueryRowContext(context.Background(), `SELECT GET_LOCK('customer_service_schema_migration', 30)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("acquire schema migration lock")
	}
	defer func() {
		_, _ = connection.ExecContext(context.Background(), `SELECT RELEASE_LOCK('customer_service_schema_migration')`)
	}()
	if _, err = connection.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS schema_migration(version INT PRIMARY KEY,applied_at DATETIME(3) NOT NULL)`); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		parts := strings.SplitN(filepath.Base(file), "_", 2)
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration filename %s: %w", file, err)
		}
		var exists int
		if err = connection.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_migration WHERE version=?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		// #nosec G304 -- filenames are constrained to *.up.sql beneath the configured migration directory.
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err = connection.ExecContext(context.Background(), string(content)); err != nil {
			return fmt.Errorf("migration %s: %w", file, err)
		}
		if _, err = connection.ExecContext(context.Background(), `INSERT INTO schema_migration(version,applied_at) VALUES(?,?)`, version, time.Now().UTC()); err != nil {
			return err
		}
		if _, err = fmt.Fprintf(output, "applied migration %d\n", version); err != nil {
			return err
		}
	}
	return nil
}
