package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/vitorfulll/android-pessoal/internal/config"
	"github.com/vitorfulll/android-pessoal/internal/store"
)

func TestLoginAndSingleActiveSession(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg := config.Config{
		JWTSecret: []byte("01234567890123456789012345678901"), ExecutorToken: "executor-token-01234567890123456789",
		BootstrapUser: "vitorfulll", BootstrapPassword: "senha-de-teste-comprida", AccessTTL: 10 * time.Minute, RefreshTTL: 24 * time.Hour, SessionTTL: 12 * time.Hour,
	}
	server := New(cfg, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	tokens := postJSON[tokenResponse](t, httpServer.URL+"/v1/login", credentials{Username: "vitorfulll", Password: "senha-de-teste-comprida"}, "")
	first := postJSON[store.Session](t, httpServer.URL+"/v1/sessions", map[string]string{}, tokens.AccessToken)
	second := postJSON[store.Session](t, httpServer.URL+"/v1/sessions", map[string]string{}, tokens.AccessToken)
	if first.ID == second.ID {
		t.Fatal("expected unique session ids")
	}
	if _, err := database.Session(context.Background(), first.ID, time.Now()); err == nil {
		t.Fatal("expected first session to be revoked")
	}
	if _, err := database.Session(context.Background(), second.ID, time.Now()); err != nil {
		t.Fatalf("expected second session to remain active: %v", err)
	}
}

func TestWrongPasswordIsRejected(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg := config.Config{
		JWTSecret: []byte("01234567890123456789012345678901"), ExecutorToken: "executor-token-01234567890123456789",
		BootstrapUser: "vitorfulll", BootstrapPassword: "senha-de-teste-comprida", AccessTTL: 10 * time.Minute, RefreshTTL: 24 * time.Hour, SessionTTL: 12 * time.Hour,
	}
	server := New(cfg, database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	body, _ := json.Marshal(credentials{Username: "vitorfulll", Password: "errada"})
	response, err := http.Post(httpServer.URL+"/v1/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got status %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func postJSON[T any](t *testing.T, url string, input any, accessToken string) T {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected status %d: %s", response.StatusCode, data)
	}
	var value T
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
