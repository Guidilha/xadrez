package repository

import (
	"context"
	"fmt"

	"code/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoMatchRepository struct {
	collection *mongo.Collection
}

var _ domain.MatchRepository = (*MongoMatchRepository)(nil)

type matchDocument struct {
	ID         string   `bson:"_id"`
	Mode       string   `bson:"mode"`
	CurrentFEN string   `bson:"current_fen"`
	WhiteName  string   `bson:"white_name"`
	BlackName  string   `bson:"black_name"`
	W2Name     string   `bson:"w2_name"`
	B2Name     string   `bson:"b2_name"`
	W3Name     string   `bson:"w3_name"`
	B3Name     string   `bson:"b3_name"`
	Status     string   `bson:"status"`
	Date       string   `bson:"date"`
	Moves      []string `bson:"moves"`
}

func NewMongoMatchRepository(collection *mongo.Collection) *MongoMatchRepository {
	return &MongoMatchRepository{collection: collection}
}

func (repository *MongoMatchRepository) Save(ctx context.Context, match domain.MatchRecord) error {
	document := matchRecordToDocument(match)
	update := bson.M{"$set": bson.M{
		"mode":        document.Mode,
		"current_fen": document.CurrentFEN,
		"white_name":  document.WhiteName,
		"black_name":  document.BlackName,
		"w2_name":     document.W2Name,
		"b2_name":     document.B2Name,
		"w3_name":     document.W3Name,
		"b3_name":     document.B3Name,
		"status":      document.Status,
		"date":        document.Date,
		"moves":       document.Moves,
	}}

	opts := options.Update().SetUpsert(true)
	if _, err := repository.collection.UpdateOne(ctx, bson.M{"_id": document.ID}, update, opts); err != nil {
		return fmt.Errorf("falha ao salvar partida: %w", err)
	}
	return nil
}

func (repository *MongoMatchRepository) FindByPlayer(ctx context.Context, username string) ([]domain.MatchRecord, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"white_name": username},
			{"black_name": username},
			{"w2_name": username},
			{"b2_name": username},
			{"w3_name": username},
			{"b3_name": username},
		},
	}

	cursor, err := repository.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("falha ao buscar partidas do jogador: %w", err)
	}
	defer cursor.Close(ctx)

	var documents []matchDocument
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("falha ao decodificar partidas do jogador: %w", err)
	}

	matches := make([]domain.MatchRecord, 0, len(documents))
	for _, document := range documents {
		matches = append(matches, matchDocumentToRecord(document))
	}
	return matches, nil
}

func matchRecordToDocument(match domain.MatchRecord) matchDocument {
	return matchDocument{
		ID:         match.ID,
		Mode:       match.Mode,
		CurrentFEN: match.CurrentFEN,
		WhiteName:  match.WhiteName,
		BlackName:  match.BlackName,
		W2Name:     match.W2Name,
		B2Name:     match.B2Name,
		W3Name:     match.W3Name,
		B3Name:     match.B3Name,
		Status:     match.Status,
		Date:       match.Date,
		Moves:      match.Moves,
	}
}

func matchDocumentToRecord(document matchDocument) domain.MatchRecord {
	return domain.MatchRecord{
		ID:         document.ID,
		Mode:       document.Mode,
		CurrentFEN: document.CurrentFEN,
		WhiteName:  document.WhiteName,
		BlackName:  document.BlackName,
		W2Name:     document.W2Name,
		B2Name:     document.B2Name,
		W3Name:     document.W3Name,
		B3Name:     document.B3Name,
		Status:     document.Status,
		Date:       document.Date,
		Moves:      document.Moves,
	}
}
