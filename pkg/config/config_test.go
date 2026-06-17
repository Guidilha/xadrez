package config

import "testing"

func TestLoadRequiresMongoURI(t *testing.T) {
	t.Setenv("MONGO_URI", "")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("PORT", "")
	t.Setenv("MONGO_DATABASE", "")
	t.Setenv("MONGO_USERS_COLLECTION", "")
	t.Setenv("MONGO_MATCHES_COLLECTION", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected missing MONGO_URI to return an error")
	}
}

func TestLoadRequiresJWTSecret(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://example")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("JWT_TTL_MINUTES", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected missing JWT_SECRET to return an error")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://example")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("PORT", "")
	t.Setenv("MONGO_DATABASE", "")
	t.Setenv("MONGO_USERS_COLLECTION", "")
	t.Setenv("MONGO_MATCHES_COLLECTION", "")
	t.Setenv("JWT_TTL_MINUTES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.Port != defaultPort {
		t.Fatalf("expected default port %q, got %q", defaultPort, cfg.Port)
	}
	if cfg.DatabaseName != defaultDatabaseName {
		t.Fatalf("expected default database %q, got %q", defaultDatabaseName, cfg.DatabaseName)
	}
	if cfg.UsersCollection != defaultUsersCollection {
		t.Fatalf("expected default users collection %q, got %q", defaultUsersCollection, cfg.UsersCollection)
	}
	if cfg.MatchesCollection != defaultMatchesCollection {
		t.Fatalf("expected default matches collection %q, got %q", defaultMatchesCollection, cfg.MatchesCollection)
	}
	if cfg.Address() != ":8080" {
		t.Fatalf("expected address :8080, got %q", cfg.Address())
	}
	if cfg.JWTSecret != "secret" {
		t.Fatalf("expected JWT secret to be loaded")
	}
	if cfg.JWTTTL.String() != "1h0m0s" {
		t.Fatalf("expected default JWT TTL 1h, got %s", cfg.JWTTTL)
	}
}

func TestLoadAllowsOverrides(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://example")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("PORT", "9090")
	t.Setenv("MONGO_DATABASE", "xadrez")
	t.Setenv("MONGO_USERS_COLLECTION", "players")
	t.Setenv("MONGO_MATCHES_COLLECTION", "games")
	t.Setenv("JWT_TTL_MINUTES", "15")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	if cfg.Port != "9090" || cfg.Address() != ":9090" {
		t.Fatalf("unexpected port/address: %#v", cfg)
	}
	if cfg.DatabaseName != "xadrez" || cfg.UsersCollection != "players" || cfg.MatchesCollection != "games" {
		t.Fatalf("unexpected mongo config: %#v", cfg)
	}
	if cfg.JWTTTL.String() != "15m0s" {
		t.Fatalf("expected JWT TTL 15m, got %s", cfg.JWTTTL)
	}
}
