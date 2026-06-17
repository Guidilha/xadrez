package service

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTokenServiceGenerateAndValidate(t *testing.T) {
	tokenService := NewTokenService("secret", time.Hour)

	token, err := tokenService.Generate("ana")
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}
	if parts := strings.Split(token, "."); len(parts) != 3 {
		t.Fatalf("expected JWT to have 3 parts, got %q", token)
	}

	claims, err := tokenService.Validate(token)
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}
	if claims.Subject != "ana" {
		t.Fatalf("expected subject ana, got %q", claims.Subject)
	}
	if claims.ExpiresAt <= claims.IssuedAt {
		t.Fatalf("expected expiration after issue time: %#v", claims)
	}
}

func TestTokenServiceRejectsTamperedToken(t *testing.T) {
	tokenService := NewTokenService("secret", time.Hour)

	token, err := tokenService.Generate("ana")
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	tampered := token[:len(token)-1] + "x"
	_, err = tokenService.Validate(tampered)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestTokenServiceRejectsExpiredToken(t *testing.T) {
	tokenService := NewTokenService("secret", time.Hour)
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	tokenService.now = func() time.Time { return now }

	token, err := tokenService.Generate("ana")
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	tokenService.now = func() time.Time { return now.Add(2 * time.Hour) }
	_, err = tokenService.Validate(token)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}
