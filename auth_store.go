package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type sqliteStore struct {
	db  *sql.DB
	key []byte
}

func deriveKeyFromEnv(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("SESSION_DB_KEY is required")
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) >= 16 {
		if len(decoded) == 32 {
			return decoded, nil
		}
		h := sha256.Sum256(decoded)
		return h[:], nil
	}
	h := sha256.Sum256([]byte(raw))
	return h[:], nil
}

func encryptBlob(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func decryptBlob(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	n := gcm.NonceSize()
	if len(blob) < n {
		return nil, errors.New("ciphertext too short")
	}
	nonce := blob[:n]
	ct := blob[n:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, err
	}
	return pt, nil
}

func NewSQLiteStore(dbPath string, key []byte) (*sqliteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, err
	}
	schema := []string{`CREATE TABLE IF NOT EXISTS sessions(
						session_id TEXT PRIMARY KEY,
						did TEXT,
						data BLOB,
						updated_at INTEGER
					);`, `CREATE TABLE IF NOT EXISTS auth_requests(
						state TEXT PRIMARY KEY,
						data BLOB,
						updated_at INTEGER
					);`}
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &sqliteStore{db: db, key: key}, nil
}

func (s *sqliteStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	enc, err := encryptBlob(s.key, data)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(session_id, did, data, updated_at) VALUES (?, ?, ?, ?) ON CONFLICT(session_id) DO UPDATE SET did=excluded.did, data=excluded.data, updated_at=excluded.updated_at`, sess.SessionID, sess.AccountDID.String(), enc, time.Now().Unix())
	return err
}

func (s *sqliteStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*oauth.ClientSessionData, error) {
	row := s.db.QueryRowContext(ctx, `SELECT data FROM sessions WHERE session_id = ? AND did = ?`, sessionID, did.String())
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session not found")
		}
		return nil, err
	}
	pt, err := decryptBlob(s.key, blob)
	if err != nil {
		return nil, err
	}
	var out oauth.ClientSessionData
	if err := json.Unmarshal(pt, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *sqliteStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ? AND did = ?`, sessionID, did.String())
	return err
}

func (s *sqliteStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	enc, err := encryptBlob(s.key, data)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO auth_requests(state, data, updated_at) VALUES (?, ?, ?) ON CONFLICT(state) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`, info.State, enc, time.Now().Unix())
	return err
}

func (s *sqliteStore) GetAuthRequestInfo(ctx context.Context, state string) (*oauth.AuthRequestData, error) {
	row := s.db.QueryRowContext(ctx, `SELECT data FROM auth_requests WHERE state = ?`, state)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auth request not found")
		}
		return nil, err
	}
	pt, err := decryptBlob(s.key, blob)
	if err != nil {
		return nil, err
	}
	var out oauth.AuthRequestData
	if err := json.Unmarshal(pt, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *sqliteStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_requests WHERE state = ?`, state)
	return err
}
