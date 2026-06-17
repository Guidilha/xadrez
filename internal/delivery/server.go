package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"code/internal/domain"
	"code/internal/service"
	"github.com/gorilla/websocket"
)

type Server struct {
	auth    *service.AuthService
	matches domain.MatchRepository
	rooms   *service.RoomManager

	upgrader websocket.Upgrader
	clients  map[string]*websocket.Conn
	mu       sync.RWMutex
	nextID   uint64
}

func NewServer(auth *service.AuthService, matches domain.MatchRepository, rooms *service.RoomManager) *Server {
	return &Server{
		auth:    auth,
		matches: matches,
		rooms:   rooms,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		clients: make(map[string]*websocket.Conn),
	}
}

func (server *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/register", server.enableCORS(server.registerHandler))
	mux.HandleFunc("/api/login", server.enableCORS(server.loginHandler))
	mux.HandleFunc("/api/rooms", server.enableCORS(server.getRoomsHandler))
	mux.HandleFunc("/api/history", server.enableCORS(server.getHistoryHandler))
	mux.HandleFunc("/ws/play", server.playWSHandler)
}

func (server *Server) enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func (server *Server) registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "Método não permitido"})
		return
	}

	var user domain.User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Dados inválidos"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	created, err := server.auth.Register(ctx, user)
	if errors.Is(err, service.ErrInvalidCredentials) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Dados inválidos"})
		return
	}
	if err != nil {
		log.Println("Erro ao registrar usuário:", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Erro ao criar usuário"})
		return
	}
	if !created {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Este usuário já está cadastrado"})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"message": "Usuário criado com sucesso!"})
}

func (server *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"message": "Método não permitido"})
		return
	}

	var credentials domain.User
	if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Dados inválidos"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	token, err := server.auth.Login(ctx, credentials)
	if errors.Is(err, service.ErrInvalidCredentials) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Dados inválidos"})
		return
	}
	if errors.Is(err, service.ErrUserNotFound) {
		log.Println("Tentativa de login: Usuário não encontrado ->", credentials.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Usuário não encontrado"})
		return
	}
	if errors.Is(err, service.ErrInvalidPassword) {
		log.Println("Tentativa de login: Senha incorreta para ->", credentials.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Senha incorreta"})
		return
	}
	if err != nil {
		log.Println("Erro ao autenticar usuário:", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "Erro ao buscar usuário"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Login autorizado",
		"token":   token,
	})
}

func (server *Server) getRoomsHandler(w http.ResponseWriter, r *http.Request) {
	activeRooms := server.rooms.AvailableRooms()
	if activeRooms == nil {
		activeRooms = []domain.RoomInfo{}
	}
	writeJSON(w, http.StatusOK, activeRooms)
}

func (server *Server) getHistoryHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("user")
	if username == "" {
		writeJSON(w, http.StatusOK, []domain.MatchRecord{})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	matches, err := server.matches.FindByPlayer(ctx, username)
	if err != nil {
		log.Println("Erro ao buscar histórico:", err)
		writeJSON(w, http.StatusOK, []domain.MatchRecord{})
		return
	}
	if matches == nil {
		matches = []domain.MatchRecord{}
	}
	writeJSON(w, http.StatusOK, matches)
}

func (server *Server) playWSHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := server.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	roomID := r.URL.Query().Get("room")
	if roomID == "" {
		return
	}

	username := r.URL.Query().Get("user")
	if username == "" {
		username = "Anônimo"
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "1v1"
	}

	clientID := server.registerClient(conn)
	defer server.unregisterClient(clientID)

	room, err := server.rooms.JoinRoom(roomID, username, mode, r.URL.Query().Get("team"), clientID)
	if errors.Is(err, service.ErrRoomFull) {
		writeWSJSON(conn, map[string]string{"error": "Sala cheia"})
		return
	}
	if errors.Is(err, service.ErrTeamFull) {
		writeWSJSON(conn, map[string]string{"error": "Equipe lotada"})
		return
	}
	if err != nil {
		log.Println("Erro ao entrar na sala:", err)
		return
	}

	server.broadcastState(room)
	for {
		var message domain.WSMessage
		if err := conn.ReadJSON(&message); err != nil {
			if shouldBroadcast := server.rooms.RemoveClient(roomID, room, clientID); shouldBroadcast {
				server.broadcastState(room)
			}
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		shouldBroadcast, err := room.HandleMessage(ctx, server.matches, roomID, clientID, message)
		cancel()
		if errors.Is(err, service.ErrClientNotFound) {
			continue
		}
		if err != nil {
			log.Println("Erro ao processar mensagem WS:", err)
		}
		if shouldBroadcast {
			server.broadcastState(room)
		}
	}
}

func (server *Server) broadcastState(room *service.Room) {
	for clientID, response := range room.Responses() {
		client := server.client(clientID)
		if client == nil {
			continue
		}
		writeWSJSON(client, response)
	}
}

func (server *Server) registerClient(conn *websocket.Conn) string {
	clientID := fmt.Sprintf("client-%d", atomic.AddUint64(&server.nextID, 1))

	server.mu.Lock()
	defer server.mu.Unlock()
	server.clients[clientID] = conn
	return clientID
}

func (server *Server) unregisterClient(clientID string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	delete(server.clients, clientID)
}

func (server *Server) client(clientID string) *websocket.Conn {
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.clients[clientID]
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Println("Erro ao escrever resposta JSON:", err)
	}
}

func writeWSJSON(conn *websocket.Conn, payload any) {
	if err := conn.WriteJSON(payload); err != nil {
		log.Println("Erro ao escrever mensagem WS:", err)
	}
}
