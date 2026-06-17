package domain

import "context"

type UserRepository interface {
	FindByUsername(ctx context.Context, username string) (User, bool, error)
	Create(ctx context.Context, user User) error
}

type MatchRepository interface {
	Save(ctx context.Context, match MatchRecord) error
	FindByPlayer(ctx context.Context, username string) ([]MatchRecord, error)
}
