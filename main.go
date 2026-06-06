package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
	"github.com/gorilla/websocket"
	"github.com/corentings/chess"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

var collection *mongo.Collection
var matchesCollection *mongo.Collection

type User struct {
	Username string `json:"username" bson:"username"`
	Password string `json:"password" bson:"password"`
}

type ClientInfo struct {
	Username string
	IsWhite  bool
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
	matchesCollection = client.Database("auth_db").Collection("matches")
	fmt.Println("Conectado ao MongoDB Atlas com sucesso!")

	http.HandleFunc("/api/register", enableCORS(registerHandler))
	http.HandleFunc("/api/login", enableCORS(loginHandler))
	http.HandleFunc("/api/rooms", getRoomsHandler)
	http.HandleFunc("/api/history", enableCORS(getHistoryHandler))
	http.HandleFunc("/ws/play", playWsHandler)
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
type Room struct {
	Game         *chess.Game
	Clients      map[*websocket.Conn]*ClientInfo // Agora guarda o ClientInfo
	RematchVotes map[*websocket.Conn]bool        // Conta quem votou na revanche
	Moves        []string 	
}

var rooms = make(map[string]*Room)
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// Estruturas de entrada e saída
type WSMessage struct {
	Move string `json:"move"` // Recebe do Flutter. Ex: "e2e4"
}
type WSResponse struct {
	FEN          string   `json:"fen"`
	Turn         string   `json:"turn"`
	Status       string   `json:"status"`
	PlayerCount  int      `json:"player_count"`
	ValidMoves   []string `json:"valid_moves"`
	WhiteName    string   `json:"white_name"`    // NOVO: Nome das Brancas
	BlackName    string   `json:"black_name"`    // NOVO: Nome das Pretas
	RematchVotes int      `json:"rematch_votes"` // NOVO: Quantos pediram revanche
}	

func playWsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	roomID := r.URL.Query().Get("room")
	username := r.URL.Query().Get("user") // O Flutter vai mandar o nome por aqui!
	
	if roomID == "" { return }
	if username == "" { username = "Anônimo" }

	if _, exists := rooms[roomID]; !exists {
		rooms[roomID] = &Room{
			Game:         chess.NewGame(),
			Clients:      make(map[*websocket.Conn]*ClientInfo),
			RematchVotes: make(map[*websocket.Conn]bool),
		}
	}
	
	room := rooms[roomID]

	if len(room.Clients) >= 2 {
		conn.WriteJSON(map[string]string{"error": "Sala cheia"})
		return
	}

	// Define a cor: Se já tem alguém e é branco, o novo é preto.
	isWhite := true
	for _, info := range room.Clients {
		if info.IsWhite { isWhite = false }
	}

	room.Clients[conn] = &ClientInfo{Username: username, IsWhite: isWhite}
	enviarEstado(room)

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			delete(room.Clients, conn) 
			delete(room.RematchVotes, conn) // Tira o voto se o cara sair
			
			if len(room.Clients) == 0 {
				delete(rooms, roomID) 
			} else {
				room.Game = chess.NewGame() 
				room.RematchVotes = make(map[*websocket.Conn]bool)
				enviarEstado(room) 
			}
			break
		}

		// LÓGICA DE CONSENSO DA REVANCHE
		if msg.Move == "rematch" {
			room.RematchVotes[conn] = true
			if len(room.RematchVotes) == 2 { // Se os DOIS votaram
				room.Game = chess.NewGame() 
				room.RematchVotes = make(map[*websocket.Conn]bool) 
			}
			enviarEstado(room) 
			continue
		}

		move, err := chess.UCINotation{}.Decode(room.Game.Position(), msg.Move)
		if err == nil {
			err = room.Game.Move(move) 
			if err == nil {
				room.Moves = append(room.Moves, msg.Move)
				salvarPartidaNoMongo(roomID, room)
				enviarEstado(room)
			}
		}
	}
}
func enviarEstado(room *Room) {
	var validMovesStr []string
	for _, move := range room.Game.ValidMoves() {
		validMovesStr = append(validMovesStr, move.String())
	}

	// Descobre quem é quem para mandar para o Flutter
	whiteName, blackName := "Aguardando...", "Aguardando..."
	for _, info := range room.Clients {
		if info.IsWhite {
			whiteName = info.Username
		} else {
			blackName = info.Username
		}
	}

	resp := WSResponse{
		FEN:          room.Game.FEN(),
		Turn:         room.Game.Position().Turn().Name(),
		Status:       room.Game.Outcome().String(),
		PlayerCount:  len(room.Clients),
		ValidMoves:   validMovesStr,
		WhiteName:    whiteName,
		BlackName:    blackName,
		RematchVotes: len(room.RematchVotes), // Manda quantos já pediram revanche
	}
	
	for client := range room.Clients {
		client.WriteJSON(resp)
	}
}

func salvarPartidaNoMongo(roomID string, room *Room) {
	whiteName, blackName := "Desconhecido", "Desconhecido"
	for _, info := range room.Clients {
		if info.IsWhite { whiteName = info.Username } else { blackName = info.Username }
	}

	opts := options.Update().SetUpsert(true) // Cria o registro se não existir
	matchesCollection.UpdateOne(
		context.Background(),
		bson.M{"_id": roomID},
		bson.M{"$set": bson.M{
			"current_fen": room.Game.FEN(),
			"white_name":  whiteName,
			"black_name":  blackName,
			"status":      room.Game.Outcome().String(),
			"date":        time.Now().Format("02/01/2006"), // Salva a data atual
			"moves":       room.Moves,
		}},
		opts,
	)
}
// Crie esta struct para formatar a resposta JSON (Pode colocar junto com as outras structs)
type RoomInfo struct {
	ID        string `json:"id"`
	Nome      string `json:"nome"`
	Jogadores int    `json:"jogadores"`
}

// Handler para listar as salas disponíveis
func getRoomsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") 

	var activeRooms []RoomInfo

	for id, room := range rooms {
		if len(room.Clients) < 2 {
			activeRooms = append(activeRooms, RoomInfo{
				ID:        id,
				Nome:      "Sala " + id, 
				Jogadores: len(room.Clients),
			})
		}
	}

	if activeRooms == nil {
		activeRooms = []RoomInfo{}
	}

	json.NewEncoder(w).Encode(activeRooms)
}
// Handler que busca no MongoDB as partidas que o usuário jogou
func getHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	username := r.URL.Query().Get("user")
	if username == "" {
		json.NewEncoder(w).Encode([]bson.M{})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Busca no banco onde o nome for igual ao das Brancas OU das Pretas
	filter := bson.M{
		"$or": []bson.M{
			{"white_name": username},
			{"black_name": username},
		},
	}

	cursor, err := matchesCollection.Find(ctx, filter)
	if err != nil {
		json.NewEncoder(w).Encode([]bson.M{})
		return
	}
	
	var matches []bson.M
	if err = cursor.All(ctx, &matches); err != nil {
		json.NewEncoder(w).Encode([]bson.M{})
		return
	}

	if matches == nil { matches = []bson.M{} }
	json.NewEncoder(w).Encode(matches)
}
