package local

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jtarchie/ci/secrets"
	_ "modernc.org/sqlite"
)

// Local is a secrets backend that stores encrypted secrets in SQLite.
type Local struct {
	db        *sql.DB
	encryptor *secrets.Encryptor
	logger    *slog.Logger
}

func init() {
	secrets.Register("local", New)
}

// New creates a new Local secrets manager.
// The DSN format is: "local://<sqlite-path>?key=<encryption-passphrase>"
// For in-memory: "local://:memory:?key=<encryption-passphrase>"
func New(dsn string, logger *slog.Logger) (secrets.Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}

	logger = logger.WithGroup("secrets.local")

	// Parse DSN: expected format "local://<path>?key=<passphrase>"
	// or just the passphrase with a separate DB path
	dbPath, passphrase, err := parseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid secrets DSN: %w", err)
	}

	key := secrets.DeriveKey(passphrase)

	encryptor, err := secrets.NewEncryptor(key)
	if err != nil {
		return nil, fmt.Errorf("could not create encryptor: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("could not open secrets database: %w", err)
	}

	//nolint: noctx
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS secrets (
			scope TEXT NOT NULL,
			key TEXT NOT NULL,
			encrypted_value BLOB NOT NULL,
			version TEXT NOT NULL DEFAULT 'v1',
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (scope, key)
		) STRICT;
	`)
	if err != nil {
		return nil, fmt.Errorf("could not create secrets table: %w", err)
	}

	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(1)

	logger.Info("secrets.local.initialized", "db", dbPath)

	return &Local{
		db:        db,
		encryptor: encryptor,
		logger:    logger,
	}, nil
}

func (l *Local) Get(ctx context.Context, scope string, key string) (string, error) {
	var encryptedValue []byte

	err := l.db.QueryRowContext(ctx, `
		SELECT encrypted_value FROM secrets WHERE scope = ? AND key = ?
	`, scope, key).Scan(&encryptedValue)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", secrets.ErrNotFound
		}

		return "", fmt.Errorf("could not query secret: %w", err)
	}

	plaintext, err := l.encryptor.Decrypt(encryptedValue)
	if err != nil {
		return "", fmt.Errorf("could not decrypt secret %q in scope %q: %w", key, scope, err)
	}

	return string(plaintext), nil
}

func (l *Local) Set(ctx context.Context, scope string, key string, value string) error {
	encrypted, err := l.encryptor.Encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("could not encrypt secret: %w", err)
	}

	// Determine next version
	var currentVersion string

	err = l.db.QueryRowContext(ctx, `
		SELECT version FROM secrets WHERE scope = ? AND key = ?
	`, scope, key).Scan(&currentVersion)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("could not check existing secret: %w", err)
	}

	nextVersion := "v1"
	if currentVersion != "" {
		nextVersion = incrementVersion(currentVersion)
	}

	_, err = l.db.ExecContext(ctx, `
		INSERT INTO secrets (scope, key, encrypted_value, version, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(scope, key) DO UPDATE SET
			encrypted_value = excluded.encrypted_value,
			version = excluded.version,
			updated_at = excluded.updated_at
	`, scope, key, encrypted, nextVersion, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("could not store secret: %w", err)
	}

	l.logger.Info("secret.set", "scope", scope, "key", key, "version", nextVersion)

	return nil
}

func (l *Local) Delete(ctx context.Context, scope string, key string) error {
	result, err := l.db.ExecContext(ctx, `
		DELETE FROM secrets WHERE scope = ? AND key = ?
	`, scope, key)
	if err != nil {
		return fmt.Errorf("could not delete secret: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not check delete result: %w", err)
	}

	if rows == 0 {
		return secrets.ErrNotFound
	}

	l.logger.Info("secret.deleted", "scope", scope, "key", key)

	return nil
}

func (l *Local) Close() error {
	return l.db.Close()
}

// parseDSN parses a secrets DSN string.
// Format: "local://<db-path>?key=<passphrase>"
// Simplified: "<db-path>?key=<passphrase>"
func parseDSN(dsn string) (dbPath string, passphrase string, err error) {
	// Strip "local://" prefix if present
	dsn = strings.TrimPrefix(dsn, "local://")

	// Split on "?key="
	parts := strings.SplitN(dsn, "?key=", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", fmt.Errorf("DSN must contain '?key=<passphrase>': got %q", dsn)
	}

	dbPath = parts[0]
	if dbPath == "" {
		dbPath = ":memory:"
	}

	return dbPath, parts[1], nil
}

// incrementVersion increments fa version string like "v1" -> "v2".
func incrementVersion(version string) string {
	var num int

	_, err := fmt.Sscanf(version, "v%d", &num)
	if err != nil {
		return "v1"
	}

	return fmt.Sprintf("v%d", num+1)
}
