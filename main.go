package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/corentings/chess"
	"github.com/gorilla/websocket"
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

// 👉 ATUALIZADO: Agora usa Role ("w1", "b1", "w2", "b2") em vez de IsWhite
type ClientInfo struct {
	Username string
	Role     string 
}

// 👉 ATUALIZADO: Adicionado Mode e MaxPlayers
type Room struct {
	Mode         string
	MaxPlayers   int
	Game         *chess.Game
	Clients      map[*websocket.Conn]*ClientInfo 
	RematchVotes map[*websocket.Conn]bool        
	Moves        []string   
	ProposedMoves map[string]string
}

var rooms = make(map[string]*Room)
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func main() {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("ERRO: A variável MONGO_URI não foi definida!")
	}

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
	http.HandleFunc("/api/rooms", enableCORS(getRoomsHandler))
	http.HandleFunc("/api/history", enableCORS(getHistoryHandler))
	http.HandleFunc("/ws/play", playWsHandler)
	
	fmt.Println("Servidor rodando na porta :" + port + "...")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json") 
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

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

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Erro ao processar senha"})
		return
	}
	user.Password = string(hashedPassword)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var existingUser User
	err = collection.FindOne(ctx, bson.M{"username": user.Username}).Decode(&existingUser)
	if err == nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"message": "Este usuário já está cadastrado"})
		return
	}

	_, err = collection.InsertOne(ctx, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Erro no MongoDB: " + err.Error()})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "Usuário criado com sucesso!"})
}

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

	var dbUser User
	err = collection.FindOne(ctx, bson.M{"username": credentials.Username}).Decode(&dbUser)
	if err != nil {
		log.Println("Tentativa de login: Usuário não encontrado ->", credentials.Username)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Usuário não encontrado"})
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(dbUser.Password), []byte(credentials.Password))
	if err != nil {
		log.Println("Tentativa de login: Senha incorreta para ->", credentials.Username)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Senha incorreta"})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Login autorizado", 
		"token": "fake-jwt-token-para-exemplo",
	})
}

type WSMessage struct {
	Move string `json:"move"`
}

type WSResponse struct {
	FEN           string            `json:"fen"`
	Turn          string            `json:"turn"`
	Status        string            `json:"status"`
	PlayerCount   int               `json:"player_count"`
	MaxPlayers    int               `json:"max_players"`
	ValidMoves    []string          `json:"valid_moves"`
	Players       map[string]string `json:"players"`
	ActiveRoles   []string          `json:"active_roles"`
	ProposedMoves map[string]string `json:"proposed_moves"`
	RematchVotes  int               `json:"rematch_votes"`
	Mode          string            `json:"mode"`
}
func get3v3Roles(room *Room) (propA string, propB string, decider string) {
	turnIdx := len(room.Moves) / 2 
	deciderNum := (turnIdx % 3) + 1 

	if room.Game.Position().Turn().Name() == "White" {
		decider = fmt.Sprintf("w%d", deciderNum)
		if deciderNum == 1 { return "w2", "w3", "w1" }
		if deciderNum == 2 { return "w1", "w3", "w2" }
		return "w1", "w2", "w3"
	} else {
		decider = fmt.Sprintf("b%d", deciderNum)
		if deciderNum == 1 { return "b2", "b3", "b1" }
		if deciderNum == 2 { return "b1", "b3", "b2" }
		return "b1", "b2", "b3"
	}
}

func getActiveRole1v1And2v2(room *Room) string {
	moveCount := len(room.Moves)
	if room.Mode == "2v2" {
		return []string{"w1", "b1", "w2", "b2"}[moveCount%4]
	}
	return []string{"w1", "b1"}[moveCount%2]
}

// Distribui a próxima cadeira vazia disponível na sala
func assignRole(room *Room, preferredTeam string) string {
	taken := make(map[string]bool)
	for _, c := range room.Clients {
		taken[c.Role] = true
	}

	var order []string
	if preferredTeam == "w" {
		order = []string{"w1", "w2", "w3"}
	} else if preferredTeam == "b" {
		order = []string{"b1", "b2", "b3"}
	} else {
		order = []string{"w1", "b1", "w2", "b2", "w3", "b3"}
	}

	for _, r := range order {
		if !taken[r] {
			if room.Mode == "1v1" && (r == "w2" || r == "b2" || r == "w3" || r == "b3") { continue }
			if room.Mode == "2v2" && (r == "w3" || r == "b3") { continue }
			return r
		}
	}
	return ""
}

func playWsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	defer conn.Close()

	roomID := r.URL.Query().Get("room")
	username := r.URL.Query().Get("user")
	mode := r.URL.Query().Get("mode")
	team := r.URL.Query().Get("team")
	
	if roomID == "" { return }
	if username == "" { username = "Anônimo" }
	if mode == "" { mode = "1v1" }

	if _, exists := rooms[roomID]; !exists {
		max := 2
		if mode == "2v2" { max = 4 }
		if mode == "3v3" { max = 6 } // 👉 LIBERA ESPAÇO PARA 6 JOGADORES
		
		rooms[roomID] = &Room{
			Mode:          mode,
			MaxPlayers:    max,
			Game:          chess.NewGame(),
			Clients:       make(map[*websocket.Conn]*ClientInfo),
			RematchVotes:  make(map[*websocket.Conn]bool),
			Moves:         []string{},
			ProposedMoves: make(map[string]string),
		}
	}
	
	room := rooms[roomID]

	if len(room.Clients) >= room.MaxPlayers {
		conn.WriteJSON(map[string]string{"error": "Sala cheia"})
		return
	}

	assignedRole := assignRole(room, team)
	if assignedRole == "" {
		conn.WriteJSON(map[string]string{"error": "Equipe lotada"})
		return
	}

	room.Clients[conn] = &ClientInfo{Username: username, Role: assignedRole}
	enviarEstado(room)

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			delete(room.Clients, conn)
			delete(room.RematchVotes, conn)
			if len(room.Clients) == 0 {
				delete(rooms, roomID)
			} else {
				room.Game = chess.NewGame()
				room.Moves = []string{}
				room.ProposedMoves = make(map[string]string)
				room.RematchVotes = make(map[*websocket.Conn]bool)
				enviarEstado(room)
			}
			break
		}

		if msg.Move == "rematch" {
			room.RematchVotes[conn] = true
			if len(room.RematchVotes) == room.MaxPlayers {
				room.Game = chess.NewGame()
				room.Moves = []string{}
				room.ProposedMoves = make(map[string]string)
				room.RematchVotes = make(map[*websocket.Conn]bool)
			}
			enviarEstado(room)
			continue
		}

		// 👉 LÓGICA DE INTERCEÇÃO E PROCESSAMENTO DO MODO 3v3
		if room.Mode == "3v3" {
			role := room.Clients[conn].Role
			propA, propB, decider := get3v3Roles(room)

			// Se for um dos Proponentes da vez
			if role == propA || role == propB {
				room.ProposedMoves[role] = msg.Move

				moveA := room.ProposedMoves[propA]
				moveB := room.ProposedMoves[propB]

				// Se AMBOS já votaram
				if moveA != "" && moveB != "" {
					if moveA == moveB {
						// Unanimidade! Executa o movimento direto
						executarLanceFinal(room, roomID, moveA)
					} else {
						// Divergência: Não executa e notifica para o desempate começar
						enviarEstado(room)
					}
				} else {
					// Apenas um votou, atualiza para mostrar o indicador visual de clique
					enviarEstado(room)
				}
				continue
			}

			// Se for o Árbitro Desempacador da vez
			if role == decider {
				moveA := room.ProposedMoves[propA]
				moveB := room.ProposedMoves[propB]
				// Só aceita o clique se houver um impasse real de lances diferentes
				if moveA != "" && moveB != "" && moveA != moveB {
					if msg.Move == moveA || msg.Move == moveB {
						executarLanceFinal(room, roomID, msg.Move)
					}
				}
				continue
			}
			continue
		}

		// LÓGICA PADRÃO PARA MÓDOS 1v1 E 2v2
		if room.Clients[conn].Role != getActiveRole1v1And2v2(room) { continue }
		executarLanceFinal(room, roomID, msg.Move)
	}
}
func executarLanceFinal(room *Room, roomID string, uciMove string) {
	move, err := chess.UCINotation{}.Decode(room.Game.Position(), uciMove)
	if err == nil {
		err = room.Game.Move(move)
		if err == nil {
			room.Moves = append(room.Moves, uciMove)
			room.ProposedMoves = make(map[string]string) // Reseta propostas para o próximo turno
			salvarPartidaNoMongo(roomID, room)
			enviarEstado(room)
		}
	}
}
func enviarEstado(room *Room) {
	var validMovesStr []string
	for _, move := range room.Game.ValidMoves() {
		validMovesStr = append(validMovesStr, move.String())
	}

	players := make(map[string]string)
	for _, info := range room.Clients {
		players[info.Role] = info.Username
	}

	// 👉 CALCULA QUEM ESTÁ ATIVO NO MOMENTO EXACTO DO SEGUNDO
	var activeRoles []string
	if room.Mode == "3v3" {
		propA, propB, decider := get3v3Roles(room)
		moveA := room.ProposedMoves[propA]
		moveB := room.ProposedMoves[propB]

		if moveA != "" && moveB != "" && moveA != moveB {
			activeRoles = []string{decider} // Fase de desempate: Apenas o líder brilha
		} else {
			activeRoles = []string{propA, propB} // Fase de votação: Os dois construtores brilham
		}
	} else {
		activeRoles = []string{getActiveRole1v1And2v2(room)}
	}

	// 👉 CONSTRUÇÃO E ENVIO COM FILTRO DE PRIVACIDADE POR CLIENTE
	for client, info := range room.Clients {
		filteredMoves := make(map[string]string)
		if room.Mode == "3v3" {
			propA, propB, decider := get3v3Roles(room)
			// O Líder/Decisor vê os dois votos transparentemente
			if info.Role == decider {
				filteredMoves[propA] = room.ProposedMoves[propA]
				filteredMoves[propB] = room.ProposedMoves[propB]
			} else {
				// Os proponentes NÃO veem o voto um do outro, apenas sabem que votaram se houver texto
				if room.ProposedMoves[propA] != "" { filteredMoves[propA] = "voted" }
				if room.ProposedMoves[propB] != "" { filteredMoves[propB] = "voted" }
				// Mas ele consegue ver o texto limpo do seu próprio voto na tela
				filteredMoves[info.Role] = room.ProposedMoves[info.Role]
			}
		}

		resp := WSResponse{
			FEN:           room.Game.FEN(),
			Turn:          room.Game.Position().Turn().Name(),
			Status:        room.Game.Outcome().String(),
			PlayerCount:   len(room.Clients),
			MaxPlayers:    room.MaxPlayers,
			ValidMoves:    validMovesStr,
			Players:       players,
			ActiveRoles:   activeRoles,
			ProposedMoves: filteredMoves,
			RematchVotes:  len(room.RematchVotes),
			Mode:          room.Mode,
		}
		client.WriteJSON(resp)
	}
}

func salvarPartidaNoMongo(roomID string, room *Room) {
	players := make(map[string]string)
	for _, info := range room.Clients {
		players[info.Role] = info.Username
	}

	opts := options.Update().SetUpsert(true)
	matchesCollection.UpdateOne(
		context.Background(),
		bson.M{"_id": roomID},
		bson.M{"$set": bson.M{
			"mode":        room.Mode,
			"current_fen": room.Game.FEN(),
			"white_name":  players["w1"],
			"black_name":  players["b1"],
			"w2_name":     players["w2"],
			"b2_name":     players["b2"],
			"w3_name":     players["w3"], // 👉 ADICIONADO AO MONGO
			"b3_name":     players["b3"], // 👉 ADICIONADO AO MONGO
			"status":      room.Game.Outcome().String(),
			"date":        time.Now().Format("02/01/2006"),
			"moves":       room.Moves,
		}},
		opts,
	)
}

type RoomInfo struct {
	ID        string `json:"id"`
	Nome      string `json:"nome"`
	Jogadores int    `json:"jogadores"`
	Max       int    `json:"max"`
	Mode      string `json:"mode"`
}	

func getRoomsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var activeRooms []RoomInfo
	for id, room := range rooms {
		if len(room.Clients) < room.MaxPlayers {
			activeRooms = append(activeRooms, RoomInfo{
				ID:        id,
				Nome:      "Sala " + id,
				Jogadores: len(room.Clients),
				Max:       room.MaxPlayers,
				Mode:      room.Mode,
			})
		}
	}
	if activeRooms == nil { activeRooms = []RoomInfo{} }
	json.NewEncoder(w).Encode(activeRooms)
}
func getHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	username := r.URL.Query().Get("user")
	if username == "" {
		json.NewEncoder(w).Encode([]bson.M{})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 👉 BUSCA AMPLIADA PARA AS 6 CADEIRAS POSSÍVEIS NO MONGO
	filter := bson.M{
		"$or": []bson.M{
			{"white_name": username}, {"black_name": username},
			{"w2_name": username}, {"b2_name": username},
			{"w3_name": username}, {"b3_name": username},
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
