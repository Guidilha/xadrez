package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"code/internal/delivery"
	"code/internal/repository"
	"code/internal/service"
	"code/pkg/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Erro ao carregar configuração:", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatal("Erro inicial de conexão com o MongoDB:", err)
	}

	database := client.Database(cfg.DatabaseName)
	userRepository := repository.NewMongoUserRepository(database.Collection(cfg.UsersCollection))
	matchRepository := repository.NewMongoMatchRepository(database.Collection(cfg.MatchesCollection))
	tokenService := service.NewTokenService(cfg.JWTSecret, cfg.JWTTTL)
	authService := service.NewAuthService(userRepository, tokenService)
	roomManager := service.NewRoomManager()

	server := delivery.NewServer(authService, matchRepository, roomManager)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	fmt.Println("Conectado ao MongoDB Atlas com sucesso!")
	fmt.Println("Servidor rodando na porta :" + cfg.Port + "...")
	log.Fatal(http.ListenAndServe(cfg.Address(), mux))
}
