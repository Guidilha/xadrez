package repository

import (
	"context"
	"fmt"

	"code/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type MongoUserRepository struct {
	collection *mongo.Collection
}

var _ domain.UserRepository = (*MongoUserRepository)(nil)

type userDocument struct {
	Username string `bson:"username"`
	Password string `bson:"password"`
	Salt     string `bson:"salt,omitempty"`
}

func NewMongoUserRepository(collection *mongo.Collection) *MongoUserRepository {
	return &MongoUserRepository{collection: collection}
}

func (repository *MongoUserRepository) FindByUsername(ctx context.Context, username string) (domain.User, bool, error) {
	var document userDocument
	err := repository.collection.FindOne(ctx, bson.M{"username": username}).Decode(&document)
	if err == mongo.ErrNoDocuments {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, fmt.Errorf("falha ao buscar usuário por username: %w", err)
	}

	return domain.User{
		Username: document.Username,
		Password: document.Password,
		Salt:     document.Salt,
	}, true, nil
}

func (repository *MongoUserRepository) Create(ctx context.Context, user domain.User) error {
	document := userDocument{
		Username: user.Username,
		Password: user.Password,
		Salt:     user.Salt,
	}

	if _, err := repository.collection.InsertOne(ctx, document); err != nil {
		return fmt.Errorf("falha ao inserir usuário: %w", err)
	}
	return nil
}
