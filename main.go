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
	http.HandleFunc("/api/rooms", getRoomsHandler)

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
// 1. Adicione o PlayerCount no struct
type WSResponse struct {
	FEN         string `json:"fen"`
	Turn        string `json:"turn"`
	Status      string `json:"status"`
	PlayerCount int    `json:"player_count"` // O Flutter usará isso para liberar o jogo
}

func playWsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// 2. Captura o código da sala da URL enviada pelo Flutter
	roomID := r.URL.Query().Get("room")
	if roomID == "" {
		return // Rejeita conexão sem sala
	}

	if _, exists := rooms[roomID]; !exists {
		rooms[roomID] = &Room{
			Game:    chess.NewGame(),
			Clients: make(map[*websocket.Conn]bool),
		}
	}
	
	room := rooms[roomID]

	// 3. Trava de segurança: Se já tem 2 pessoas, não deixa entrar mais ninguém
	if len(room.Clients) >= 2 {
		conn.WriteJSON(map[string]string{"error": "Sala cheia"})
		return
	}

	room.Clients[conn] = true

	// 4. Avisa a todos na sala que alguém entrou/saiu
	enviarEstado(room)

	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			delete(room.Clients, conn) // Remove o jogador se a conexão cair
			enviarEstado(room)         // Avisa o outro que ele ficou sozinho
			break
		}

		move, err := chess.UCINotation{}.Decode(room.Game.Position(), msg.Move)
		if err == nil {
			err = room.Game.Move(move) 
			if err == nil {
				salvarPartidaNoMongo(roomID, room.Game.FEN())
				enviarEstado(room)
			}
		}
	}
}

func enviarEstado(room *Room) {
	resp := WSResponse{
		FEN:         room.Game.FEN(),
		Turn:        room.Game.Position().Turn().Name(),
		Status:      room.Game.Outcome().String(),
		PlayerCount: len(room.Clients), // Conta quantos WebSockets estão ativos
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

class _JoinRoomScreenState extends State<JoinRoomScreen> {
  // 1. A lista agora começa vazia
  List<Map<String, dynamic>> _salasDisponiveis = [];
  
  // 2. Variável para mostrar um "Carregando..." enquanto busca os dados
  bool _isLoading = true;

  // 3. O initState faz a busca assim que a tela abre
  @override
  void initState() {
    super.initState();
    _buscarSalas();
  }

  // 4. A função que bate no seu servidor Go
  Future<void> _buscarSalas() async {
    try {
      // ATENÇÃO: Coloque aqui o link do seu backend Go.
      // Se estiver rodando no PC, use localhost (ou o IP local se estiver testando no celular).
      // Se já publicou no Render, use a URL do Render: 'https://seu-app.onrender.com/api/rooms'
      final response = await http.get(Uri.parse('http://localhost:8080/api/rooms'));

      if (response.statusCode == 200) {
        List<dynamic> dadosJson = jsonDecode(response.body);
        
        setState(() {
          // Mapeia o JSON que veio do Go para o formato que o Flutter espera
          _salasDisponiveis = dadosJson.map((sala) => {
            'id': sala['id'].toString(),
            'nome': sala['nome'].toString(),
            'jogadores': sala['jogadores'],
          }).toList();
          _isLoading = false; // Terminou de carregar
        });
      }
    } catch (e) {
      print("Erro ao buscar salas: $e");
      setState(() {
        _isLoading = false; // Termina de carregar mesmo se der erro
      });
    }
  }
