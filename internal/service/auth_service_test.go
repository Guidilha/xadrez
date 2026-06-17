package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"code/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type fakeUserRepository struct {
	users map[string]domain.User
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[string]domain.User)}
}

func (repository *fakeUserRepository) FindByUsername(ctx context.Context, username string) (domain.User, bool, error) {
	user, exists := repository.users[username]
	return user, exists, nil
}

func (repository *fakeUserRepository) Create(ctx context.Context, user domain.User) error {
	repository.users[user.Username] = user
	return nil
}

func TestAuthServiceRegisterHashesPassword(t *testing.T) {
	repository := newFakeUserRepository()
	auth := NewAuthService(repository, NewTokenService("secret", time.Hour))

	created, err := auth.Register(context.Background(), domain.User{
		Username: "ana",
		Password: "segredo",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if !created {
		t.Fatal("expected user to be created")
	}

	stored := repository.users["ana"]
	if stored.Salt == "" {
		t.Fatal("expected explicit password salt to be stored")
	}
	if stored.Password == "segredo" {
		t.Fatal("expected password to be hashed")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.Password), []byte("segredo")); err == nil {
		t.Fatal("expected unsalted password not to match stored hash")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.Password), passwordWithSalt("segredo", stored.Salt)); err != nil {
		t.Fatalf("stored hash does not match salted password: %v", err)
	}
}

func TestAuthServiceLoginFailures(t *testing.T) {
	repository := newFakeUserRepository()
	auth := NewAuthService(repository, NewTokenService("secret", time.Hour))

	_, err := auth.Login(context.Background(), domain.User{Username: "ana", Password: "segredo"})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	_, err = auth.Register(context.Background(), domain.User{Username: "ana", Password: "segredo"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = auth.Login(context.Background(), domain.User{Username: "ana", Password: "errada"})
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestAuthServiceLoginReturnsValidToken(t *testing.T) {
	repository := newFakeUserRepository()
	tokenService := NewTokenService("secret", time.Hour)
	auth := NewAuthService(repository, tokenService)

	_, err := auth.Register(context.Background(), domain.User{Username: "ana", Password: "segredo"})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	token, err := auth.Login(context.Background(), domain.User{Username: "ana", Password: "segredo"})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected login to return a token")
	}

	claims, err := tokenService.Validate(token)
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}
	if claims.Subject != "ana" {
		t.Fatalf("expected subject ana, got %q", claims.Subject)
	}
}

func TestAuthServiceLoginSupportsLegacyHashWithoutExplicitSalt(t *testing.T) {
	repository := newFakeUserRepository()
	tokenService := NewTokenService("secret", time.Hour)
	auth := NewAuthService(repository, tokenService)

	legacyHash, err := bcrypt.GenerateFromPassword([]byte("segredo"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("legacy hash failed: %v", err)
	}
	repository.users["ana"] = domain.User{
		Username: "ana",
		Password: string(legacyHash),
	}

	token, err := auth.Login(context.Background(), domain.User{Username: "ana", Password: "segredo"})
	if err != nil {
		t.Fatalf("legacy login failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected legacy login to return token")
	}
}
