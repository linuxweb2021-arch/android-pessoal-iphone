package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type Session struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Generation int64     `json:"generation"`
	Quality    string    `json:"quality"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	RevokedAt  time.Time `json:"-"`
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS users (
    username TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS refresh_tokens (
    digest BLOB PRIMARY KEY,
    username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS refresh_tokens_expiry ON refresh_tokens(expires_at);
CREATE TABLE IF NOT EXISTS remote_sessions (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL REFERENCES users(username) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    quality TEXT NOT NULL DEFAULT 'balanced',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX IF NOT EXISTS sessions_owner ON remote_sessions(username, revoked_at);
`)
	return err
}

func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,created_at) VALUES(?,?,?)`, username, passwordHash, now.Unix())
	return err
}

func (s *Store) PasswordHash(ctx context.Context, username string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE username=?`, username).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

func (s *Store) SaveRefreshToken(ctx context.Context, username string, digest []byte, expiresAt, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO refresh_tokens(digest,username,expires_at,created_at) VALUES(?,?,?,?)`, digest, username, expiresAt.Unix(), now.Unix())
	return err
}

func (s *Store) ConsumeRefreshToken(ctx context.Context, digest []byte, now time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var username string
	err = tx.QueryRowContext(ctx, `SELECT username FROM refresh_tokens WHERE digest=? AND expires_at>?`, digest, now.Unix()).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE digest=?`, digest); err != nil {
		return "", err
	}
	return username, tx.Commit()
}

func (s *Store) RevokeRefreshToken(ctx context.Context, digest []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE digest=?`, digest)
	return err
}

func (s *Store) RevokeAllRefreshTokens(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM refresh_tokens WHERE username=?`, username)
	return err
}

func (s *Store) CreateSession(ctx context.Context, username, quality string, ttl time.Duration, now time.Time) (Session, error) {
	id, err := randomID()
	if err != nil {
		return Session{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE remote_sessions SET revoked_at=? WHERE username=? AND revoked_at IS NULL`, now.Unix(), username); err != nil {
		return Session{}, err
	}
	session := Session{ID: id, Username: username, Generation: now.UnixNano(), Quality: quality, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	_, err = tx.ExecContext(ctx, `INSERT INTO remote_sessions(id,username,generation,quality,created_at,expires_at) VALUES(?,?,?,?,?,?)`, session.ID, session.Username, session.Generation, session.Quality, session.CreatedAt.Unix(), session.ExpiresAt.Unix())
	if err != nil {
		return Session{}, err
	}
	return session, tx.Commit()
}

func (s *Store) Session(ctx context.Context, id string, now time.Time) (Session, error) {
	var value Session
	var createdAt, expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id,username,generation,quality,created_at,expires_at FROM remote_sessions WHERE id=? AND revoked_at IS NULL AND expires_at>?`, id, now.Unix()).Scan(&value.ID, &value.Username, &value.Generation, &value.Quality, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	value.CreatedAt = time.Unix(createdAt, 0)
	value.ExpiresAt = time.Unix(expiresAt, 0)
	return value, err
}

func (s *Store) ActiveSession(ctx context.Context, now time.Time) (Session, error) {
	var value Session
	var createdAt, expiresAt int64
	err := s.db.QueryRowContext(ctx, `SELECT id,username,generation,quality,created_at,expires_at
FROM remote_sessions
WHERE revoked_at IS NULL AND expires_at>?
ORDER BY created_at DESC LIMIT 1`, now.Unix()).Scan(
		&value.ID, &value.Username, &value.Generation, &value.Quality, &createdAt, &expiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	value.CreatedAt = time.Unix(createdAt, 0)
	value.ExpiresAt = time.Unix(expiresAt, 0)
	return value, err
}

func (s *Store) RevokeSession(ctx context.Context, id, username string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE remote_sessions SET revoked_at=? WHERE id=? AND username=? AND revoked_at IS NULL`, now.Unix(), id, username)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func randomID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
