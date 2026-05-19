package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os" // <-- IMPORTE O PACOTE OS
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

var collection *mongo.Collection

type User struct {
	Username string `json:"username" bson:"username"`
	Password string `json:"password" bson:"password"`
}

func main() {
	// 1. Pega a URI do banco das variáveis de ambiente do Render
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("ERRO: A variável MONGO_URI não foi definida!")
	}

	// 2. Pega a porta do Render (ou usa 8080 se estiver rodando local)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatal("Erro inicial de conexão com o MongoDB:", err)
	}

	collection = client.Database("auth_db").Collection("users")
	fmt.Println("Conectado ao MongoDB Atlas com sucesso!")

	http.HandleFunc("/api/register", enableCORS(registerHandler))
	http.HandleFunc("/api/login", enableCORS(loginHandler))

	// Usa a porta dinâmica obtida do sistema
	fmt.Println("Servidor rodando na porta :" + port + "...")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// O restante do código (enableCORS, registerHandler, loginHandler) continua EXATAMENTE O MESMO.

// Middleware de CORS para permitir a comunicação com o Frontend separado
func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json") // Força resposta sempre em JSON
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// Handler para registrar novos usuários
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"message": "Método não permitido"})
		return
	}

	var user User
	err := json.NewDecoder(r.Body).Decode(&user)
	if err != nil || user.Username == "" || user.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Dados inválidos"})
		return
	}

	// Criptografia da senha
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Erro ao processar senha"})
		return
	}
	user.Password = string(hashedPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verifica se usuário já existe
	var existingUser User
	err = collection.FindOne(ctx, bson.M{"username": user.Username}).Decode(&existingUser)
	if err == nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"message": "Este usuário já está cadastrado"})
		return
	}

	// Insere no banco Atlas
	_, err = collection.InsertOne(ctx, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Erro no MongoDB: " + err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "Usuário criado com sucesso!"})
}

// Handler para realizar o login
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"message": "Método não permitido"})
		return
	}

	var credentials User
	err := json.NewDecoder(r.Body).Decode(&credentials)
	if err != nil || credentials.Username == "" || credentials.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"message": "Dados inválidos"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Busca o usuário no banco
	var dbUser User
	err = collection.FindOne(ctx, bson.M{"username": credentials.Username}).Decode(&dbUser)
	if err != nil {
		log.Println("Tentativa de login: Usuário não encontrado ->", credentials.Username)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Usuário não encontrado"})
		return
	}

	// 2. Valida a senha criptografada
	err = bcrypt.CompareHashAndPassword([]byte(dbUser.Password), []byte(credentials.Password))
	if err != nil {
		log.Println("Tentativa de login: Senha incorreta para ->", credentials.Username)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Senha incorreta"})
		return
	}

	// Login válido
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Login autorizado", 
		"token": "fake-jwt-token-para-exemplo",
	})
}
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// Representa uma sala ativa na memória
type Room struct {
	Game    *chess.Game
	Clients map[*websocket.Conn]bool
}

var rooms = make(map[string]*Room)

// Estruturas de entrada e saída
type WSMessage struct {
	Move string `json:"move"` // Recebe do Flutter. Ex: "e2e4"
}

type WSResponse struct {
	FEN    string `json:"fen"`
	Turn   string `json:"turn"`
	Status string `json:"status"`
}

func playWsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Simplificação: usando uma sala estática. No futuro, você pega o ID da sala pela URL.
	roomID := "sala_1" 
	if _, exists := rooms[roomID]; !exists {
		rooms[roomID] = &Room{
			Game:    chess.NewGame(),
			Clients: make(map[*websocket.Conn]bool),
		}
	}
	
	room := rooms[roomID]
	room.Clients[conn] = true

	// Atualiza o jogador recém-conectado com as posições atuais
	enviarEstado(room)

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			delete(room.Clients, conn)
			break
		}

		// 1. O Backend tenta decodificar a intenção bruta (ex: "e2e4")
		move, err := chess.UCINotation{}.Decode(room.Game.Position(), msg.Move)
		
		// 2. Se a notação for válida, tenta executar o lance (valida xeque, turno, física, etc)
		if err == nil {
			err = room.Game.Move(move) 
			if err == nil {
				// 3. Lance aceito! Salva no MongoDB e manda o tabuleiro novo pro Flutter desenhar
				salvarPartidaNoMongo(roomID, room.Game.FEN())
				enviarEstado(room)
			}
		}
		// OBS: Se o movimento for ilegal, o err será != nil. 
		// Simplesmente não fazemos nada. O Flutter continuará com o tabuleiro antigo.
	}
}

func enviarEstado(room *Room) {
	resp := WSResponse{
		FEN:    room.Game.FEN(),
		Turn:   room.Game.Position().Turn().Name(), // Retorna "White" ou "Black"
		Status: room.Game.Outcome().String(),       // Retorna "1/2-1/2", "1-0", etc no fim do jogo
	}
	for client := range room.Clients {
		client.WriteJSON(resp)
	}
}

func salvarPartidaNoMongo(roomID, fen string) {
	// Atualiza silenciosamente no banco
	matchesCollection.UpdateOne(
		context.Background(),
		bson.M{"_id": roomID},
		bson.M{"$set": bson.M{"current_fen": fen}},
	)
}
