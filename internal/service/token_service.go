package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("token inválido")
	ErrExpiredToken = errors.New("token expirado")
)

type TokenService struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

type TokenClaims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{
		secret: []byte(secret),
		ttl:    ttl,
		now:    time.Now,
	}
}

func (service *TokenService) Generate(subject string) (string, error) {
	if subject == "" {
		return "", ErrInvalidCredentials
	}

	now := service.now().UTC()
	claims := TokenClaims{
		Subject:   subject,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(service.ttl).Unix(),
	}

	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}

	encodedHeader, err := encodeJSON(header)
	if err != nil {
		return "", fmt.Errorf("falha ao codificar header JWT: %w", err)
	}
	encodedClaims, err := encodeJSON(claims)
	if err != nil {
		return "", fmt.Errorf("falha ao codificar claims JWT: %w", err)
	}

	signingInput := encodedHeader + "." + encodedClaims
	signature := service.sign(signingInput)
	return signingInput + "." + signature, nil
}

func (service *TokenService) Validate(token string) (TokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return TokenClaims{}, ErrInvalidToken
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSignature := service.sign(signingInput)
	if !hmac.Equal([]byte(expectedSignature), []byte(parts[2])) {
		return TokenClaims{}, ErrInvalidToken
	}

	var claims TokenClaims
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return TokenClaims{}, ErrInvalidToken
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return TokenClaims{}, ErrInvalidToken
	}
	if claims.Subject == "" {
		return TokenClaims{}, ErrInvalidToken
	}
	if service.now().UTC().Unix() > claims.ExpiresAt {
		return TokenClaims{}, ErrExpiredToken
	}
	return claims, nil
}

func (service *TokenService) sign(input string) string {
	mac := hmac.New(sha256.New, service.secret)
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
