package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vitorfulll/android-pessoal/internal/auth"
	"github.com/vitorfulll/android-pessoal/internal/config"
	"github.com/vitorfulll/android-pessoal/internal/signal"
	"github.com/vitorfulll/android-pessoal/internal/store"
)

type contextKey string

const usernameKey contextKey = "username"

type Server struct {
	cfg      config.Config
	store    *store.Store
	tokens   *auth.TokenManager
	limiter  *loginLimiter
	hub      *signal.Hub
	log      *slog.Logger
	upgrader websocket.Upgrader
}

func New(cfg config.Config, database *store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg: cfg, store: database, tokens: auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTTL),
		limiter: newLoginLimiter(), hub: signal.NewHub(), log: logger,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 8 * time.Second,
			CheckOrigin:      func(r *http.Request) bool { return r.Header.Get("Origin") == "" },
		},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/login", s.login)
	mux.HandleFunc("POST /v1/refresh", s.refresh)
	mux.Handle("POST /v1/logout", s.authenticated(http.HandlerFunc(s.logout)))
	mux.Handle("GET /v1/device", s.authenticated(http.HandlerFunc(s.device)))
	mux.Handle("POST /v1/sessions", s.authenticated(http.HandlerFunc(s.createSession)))
	mux.Handle("DELETE /v1/sessions/{id}", s.authenticated(http.HandlerFunc(s.deleteSession)))
	mux.Handle("GET /v1/sessions/{id}/signal/client", s.authenticated(http.HandlerFunc(s.clientSignal)))
	mux.HandleFunc("GET /v1/executor/session", s.executorSession)
	mux.HandleFunc("GET /v1/sessions/{id}/signal/gateway", s.gatewaySignal)
	return securityHeaders(s.requestLog(mux))
}

func (s *Server) Bootstrap(ctx context.Context) error {
	count, err := s.store.UserCount(ctx)
	if err != nil || count > 0 {
		return err
	}
	if s.cfg.BootstrapPassword == "" {
		return errors.New("ANDROID_BOOTSTRAP_PASSWORD is required for the first start")
	}
	hash, err := auth.HashPassword(s.cfg.BootstrapPassword)
	if err != nil {
		return fmt.Errorf("bootstrap password: %w", err)
	}
	if err := s.store.CreateUser(ctx, s.cfg.BootstrapUser, hash, time.Now()); err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}
	s.log.Info("bootstrap user created", "username", s.cfg.BootstrapUser)
	return nil
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type tokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type tokenResponse struct {
	AccessToken           string `json:"accessToken"`
	AccessTokenExpiresAt  string `json:"accessTokenExpiresAt"`
	RefreshToken          string `json:"refreshToken"`
	RefreshTokenExpiresAt string `json:"refreshTokenExpiresAt"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input credentials
	if !decodeJSON(w, r, &input) || input.Username == "" || input.Password == "" {
		return
	}
	key := clientIP(r) + "|" + strings.ToLower(input.Username)
	if !s.limiter.Allow(key, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "muitas tentativas; aguarde alguns minutos")
		return
	}
	hash, err := s.store.PasswordHash(r.Context(), input.Username)
	if err != nil || !auth.VerifyPassword(hash, input.Password) {
		writeError(w, http.StatusUnauthorized, "usuário ou senha inválidos")
		return
	}
	s.limiter.Success(key)
	s.issueTokens(w, r, input.Username)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var input tokenRequest
	if !decodeJSON(w, r, &input) || input.RefreshToken == "" {
		return
	}
	username, err := s.store.ConsumeRefreshToken(r.Context(), auth.DigestRefreshToken(input.RefreshToken), time.Now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "refresh token inválido")
		return
	}
	s.issueTokens(w, r, username)
}

func (s *Server) issueTokens(w http.ResponseWriter, r *http.Request, username string) {
	now := time.Now()
	access, accessExpiry, err := s.tokens.Access(username, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "não foi possível criar a sessão")
		return
	}
	refresh, digest, err := auth.NewRefreshToken()
	refreshExpiry := now.Add(s.cfg.RefreshTTL)
	if err != nil || s.store.SaveRefreshToken(r.Context(), username, digest, refreshExpiry, now) != nil {
		writeError(w, http.StatusInternalServerError, "não foi possível criar a sessão")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{access, accessExpiry.UTC().Format(time.RFC3339), refresh, refreshExpiry.UTC().Format(time.RFC3339)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	var input tokenRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.RefreshToken != "" {
		_ = s.store.RevokeRefreshToken(r.Context(), auth.DigestRefreshToken(input.RefreshToken))
	}
	username := r.Context().Value(usernameKey).(string)
	_ = s.store.RevokeAllRefreshTokens(r.Context(), username)
	if session, err := s.store.ActiveSession(r.Context(), time.Now()); err == nil && session.Username == username {
		_, _ = s.store.RevokeSession(r.Context(), session.ID, username, time.Now())
		s.hub.CloseSession(session.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) device(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": "android-personal", "status": "unavailable", "detail": "executor ainda não conectado"})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(usernameKey).(string)
	input := struct {
		Quality string `json:"quality"`
	}{Quality: "balanced"}
	if r.ContentLength != 0 && !decodeJSON(w, r, &input) {
		return
	}
	if input.Quality != "economy" && input.Quality != "balanced" && input.Quality != "quality" {
		writeError(w, http.StatusBadRequest, "perfil de qualidade inválido")
		return
	}
	previous, _ := s.store.ActiveSession(r.Context(), time.Now())
	session, err := s.store.CreateSession(r.Context(), username, input.Quality, 2*time.Hour, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "não foi possível criar a sessão remota")
		return
	}
	if previous.Username == username {
		s.hub.CloseSession(previous.ID)
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(usernameKey).(string)
	revoked, err := s.store.RevokeSession(r.Context(), r.PathValue("id"), username, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "não foi possível encerrar a sessão")
		return
	}
	if !revoked {
		writeError(w, http.StatusNotFound, "sessão não encontrada")
		return
	}
	s.hub.CloseSession(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) executorSession(w http.ResponseWriter, r *http.Request) {
	if !s.executorAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "executor não autorizado")
		return
	}
	session, err := s.store.ActiveSession(r.Context(), time.Now())
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) clientSignal(w http.ResponseWriter, r *http.Request) {
	session, err := s.store.Session(r.Context(), r.PathValue("id"), time.Now())
	if err != nil || session.Username != r.Context().Value(usernameKey).(string) {
		writeError(w, http.StatusNotFound, "sessão não encontrada")
		return
	}
	s.websocket(w, r, session.ID, signal.Client)
}

func (s *Server) gatewaySignal(w http.ResponseWriter, r *http.Request) {
	if !s.executorAuthorized(r) {
		writeError(w, http.StatusUnauthorized, "executor não autorizado")
		return
	}
	session, err := s.store.Session(r.Context(), r.PathValue("id"), time.Now())
	if err != nil {
		writeError(w, http.StatusNotFound, "sessão não encontrada")
		return
	}
	s.websocket(w, r, session.ID, signal.Gateway)
}

func (s *Server) executorAuthorized(r *http.Request) bool {
	want := []byte("Bearer " + s.cfg.ExecutorToken)
	got := []byte(r.Header.Get("Authorization"))
	return len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request, sessionID string, role signal.Role) {
	peer, err := s.hub.Join(sessionID, role)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.hub.Leave(sessionID, peer)
		return
	}
	defer func() {
		conn.Close()
		s.hub.Leave(sessionID, peer)
	}()
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case message := <-peer.Send:
				_ = conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
				if err := conn.WriteMessage(message.Type, message.Data); err != nil {
					_ = conn.Close()
					return
				}
			case <-peer.Done():
				_ = conn.Close()
				return
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(8 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		_ = s.hub.Forward(sessionID, peer, signal.Message{Type: messageType, Data: data})
	}
}

func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			writeError(w, http.StatusUnauthorized, "autenticação necessária")
			return
		}
		claims, err := s.tokens.ParseAccess(strings.TrimPrefix(header, prefix))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "sessão inválida ou expirada")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), usernameKey, claims.Username)))
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido")
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
