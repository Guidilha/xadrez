package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"code/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

const passwordSaltSize = 16

type AuthService struct {
	users  domain.UserRepository
	tokens *TokenService
}

func NewAuthService(users domain.UserRepository, tokens *TokenService) *AuthService {
	return &AuthService{
		users:  users,
		tokens: tokens,
	}
}

func (service *AuthService) Register(ctx context.Context, user domain.User) (bool, error) {
	if user.Username == "" || user.Password == "" {
		return false, ErrInvalidCredentials
	}

	_, exists, err := service.users.FindByUsername(ctx, user.Username)
	if err != nil {
		return false, fmt.Errorf("falha ao buscar usuário: %w", err)
	}
	if exists {
		return false, nil
	}

	salt, err := generatePasswordSalt()
	if err != nil {
		return false, fmt.Errorf("falha ao gerar salt da senha: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword(passwordWithSalt(user.Password, salt), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("falha ao processar senha: %w", err)
	}

	user.Password = string(hashedPassword)
	user.Salt = salt
	if err := service.users.Create(ctx, user); err != nil {
		return false, fmt.Errorf("falha ao criar usuário: %w", err)
	}
	return true, nil
}

func (service *AuthService) Login(ctx context.Context, credentials domain.User) (string, error) {
	if credentials.Username == "" || credentials.Password == "" {
		return "", ErrInvalidCredentials
	}

	user, exists, err := service.users.FindByUsername(ctx, credentials.Username)
	if err != nil {
		return "", fmt.Errorf("falha ao buscar usuário: %w", err)
	}
	if !exists {
		return "", ErrUserNotFound
	}

	password := []byte(credentials.Password)
	if user.Salt != "" {
		password = passwordWithSalt(credentials.Password, user.Salt)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), password); err != nil {
		return "", ErrInvalidPassword
	}
	token, err := service.tokens.Generate(user.Username)
	if err != nil {
		return "", fmt.Errorf("falha ao gerar token: %w", err)
	}
	return token, nil
}

func generatePasswordSalt() (string, error) {
	salt := make([]byte, passwordSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(salt), nil
}

func passwordWithSalt(password string, salt string) []byte {
	return []byte(salt + password)
}
