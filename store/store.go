package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// Store wraps SQLite with AES-256-GCM encryption for sensitive fields.
type Store struct {
	mu  sync.RWMutex
	db  *sql.DB
	gcm cipher.AEAD
}

// TokenData holds Codex OAuth tokens.
type TokenData struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	LastRefresh  string // RFC3339Nano
}

// New opens (or creates) the SQLite database at dir/data.db.
// The encryption key is derived from SECRET_KEY env var, or loaded/generated
// from dir/secret.key.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	key, err := loadOrCreateKey(dir)
	if err != nil {
		return nil, fmt.Errorf("encryption key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "data.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer

	s := &Store{db: db, gcm: gcm}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func loadOrCreateKey(dir string) ([]byte, error) {
	if raw := os.Getenv("SECRET_KEY"); raw != "" {
		h := sha256.Sum256([]byte(raw))
		return h[:], nil
	}
	keyFile := filepath.Join(dir, "secret.key")
	if b, err := os.ReadFile(keyFile); err == nil && len(b) == 64 {
		key, err := hex.DecodeString(string(b))
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(key)), 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kv (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS tokens (
			id            INTEGER PRIMARY KEY,
			access_token  TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			account_id    TEXT NOT NULL,
			last_refresh  TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS api_keys (
			key TEXT PRIMARY KEY
		);
	`)
	return err
}

// ── Encryption helpers ────────────────────────────────────────────────────────

func (s *Store) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ct), nil
}

func (s *Store) decrypt(cipherHex string) (string, error) {
	ct, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	ns := s.gcm.NonceSize()
	if len(ct) < ns {
		return "", errors.New("ciphertext too short")
	}
	plain, err := s.gcm.Open(nil, ct[:ns], ct[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ── KV helpers (plain text, for non-sensitive config) ────────────────────────

func (s *Store) kvGet(key string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM kv WHERE key=?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) kvSet(key, value string) error {
	_, err := s.db.Exec("INSERT OR REPLACE INTO kv(key,value) VALUES(?,?)", key, value)
	return err
}

// ── Admin password ────────────────────────────────────────────────────────────

func HashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:])
}

func (s *Store) SetAdminPassword(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kvSet("admin_password_hash", HashPassword(password))
}

func (s *Store) HasAdminPassword() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, _ := s.kvGet("admin_password_hash")
	return v != ""
}

func (s *Store) CheckAdminPassword(password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, _ := s.kvGet("admin_password_hash")
	return v != "" && v == HashPassword(password)
}

// ── Config ────────────────────────────────────────────────────────────────────

func (s *Store) GetConfig(key, defaultVal string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, _ := s.kvGet("cfg:" + key)
	if v == "" {
		return defaultVal
	}
	return v
}

func (s *Store) SetConfig(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kvSet("cfg:"+key, value)
}

// ── Tokens (encrypted) ───────────────────────────────────────────────────────

func (s *Store) GetTokens() *TokenData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var at, rt, aid, lr string
	err := s.db.QueryRow(
		"SELECT access_token, refresh_token, account_id, last_refresh FROM tokens LIMIT 1",
	).Scan(&at, &rt, &aid, &lr)
	if err != nil {
		return nil
	}

	decAt, err1 := s.decrypt(at)
	decRt, err2 := s.decrypt(rt)
	if err1 != nil || err2 != nil {
		return nil
	}
	return &TokenData{
		AccessToken:  decAt,
		RefreshToken: decRt,
		AccountID:    aid,
		LastRefresh:  lr,
	}
}

func (s *Store) SetTokens(t *TokenData) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	encAt, err := s.encrypt(t.AccessToken)
	if err != nil {
		return err
	}
	encRt, err := s.encrypt(t.RefreshToken)
	if err != nil {
		return err
	}

	_, err = s.db.Exec("DELETE FROM tokens")
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT INTO tokens(access_token, refresh_token, account_id, last_refresh) VALUES(?,?,?,?)",
		encAt, encRt, t.AccountID, t.LastRefresh,
	)
	return err
}

// ── API keys ──────────────────────────────────────────────────────────────────

func (s *Store) ValidateAPIKey(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var k string
	err := s.db.QueryRow("SELECT key FROM api_keys WHERE key=?", key).Scan(&k)
	return err == nil
}

func (s *Store) AddAPIKey(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("INSERT OR IGNORE INTO api_keys(key) VALUES(?)", key)
	return err
}

func (s *Store) ListAPIKeys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query("SELECT key FROM api_keys")
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *Store) DeleteAPIKey(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM api_keys WHERE key=?", key)
	return err
}
