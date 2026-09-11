package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "android-pessoal"

type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret    []byte
	accessTTL time.Duration
}

func NewTokenManager(secret []byte, accessTTL time.Duration) *TokenManager {
	return &TokenManager{secret: secret, accessTTL: accessTTL}
}

func (m *TokenManager) Access(username string, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(m.accessTTL)
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   username,
			Audience:  jwt.ClaimStrings{"android-pessoal-ios"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	return signed, expiresAt, err
}

func (m *TokenManager) ParseAccess(raw string) (Claims, error) {
	token, err := jwt.ParseWithClaims(raw, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected token signing method")
		}
		return m.secret, nil
	}, jwt.WithAudience("android-pessoal-ios"), jwt.WithIssuer(issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return Claims{}, errors.New("invalid access token")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || claims.Username == "" {
		return Claims{}, errors.New("invalid access token claims")
	}
	return *claims, nil
}

func NewRefreshToken() (plain string, digest []byte, err error) {
	data := make([]byte, 32)
	if _, err = rand.Read(data); err != nil {
		return "", nil, err
	}
	plain = base64.RawURLEncoding.EncodeToString(data)
	hash := sha256.Sum256([]byte(plain))
	return plain, hash[:], nil
}

func DigestRefreshToken(plain string) []byte {
	hash := sha256.Sum256([]byte(plain))
	return hash[:]
}
