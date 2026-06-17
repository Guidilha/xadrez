package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort              = "8080"
	defaultDatabaseName      = "auth_db"
	defaultUsersCollection   = "users"
	defaultMatchesCollection = "matches"
	defaultJWTTTLMinutes     = 60
)

type Config struct {
	MongoURI          string
	Port              string
	DatabaseName      string
	UsersCollection   string
	MatchesCollection string
	JWTSecret         string
	JWTTTL            time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		MongoURI:          os.Getenv("MONGO_URI"),
		Port:              valueOrDefault(os.Getenv("PORT"), defaultPort),
		DatabaseName:      valueOrDefault(os.Getenv("MONGO_DATABASE"), defaultDatabaseName),
		UsersCollection:   valueOrDefault(os.Getenv("MONGO_USERS_COLLECTION"), defaultUsersCollection),
		MatchesCollection: valueOrDefault(os.Getenv("MONGO_MATCHES_COLLECTION"), defaultMatchesCollection),
		JWTSecret:         os.Getenv("JWT_SECRET"),
	}

	if cfg.MongoURI == "" {
		return Config{}, fmt.Errorf("variável MONGO_URI não foi definida")
	}
	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("variável JWT_SECRET não foi definida")
	}

	jwtTTL, err := jwtTTLFromEnv(os.Getenv("JWT_TTL_MINUTES"))
	if err != nil {
		return Config{}, err
	}
	cfg.JWTTTL = jwtTTL
	return cfg, nil
}

func (cfg Config) Address() string {
	return ":" + cfg.Port
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func jwtTTLFromEnv(value string) (time.Duration, error) {
	if value == "" {
		return time.Duration(defaultJWTTTLMinutes) * time.Minute, nil
	}

	minutes, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("JWT_TTL_MINUTES inválido: %w", err)
	}
	if minutes <= 0 {
		return 0, fmt.Errorf("JWT_TTL_MINUTES deve ser maior que zero")
	}
	return time.Duration(minutes) * time.Minute, nil
}
