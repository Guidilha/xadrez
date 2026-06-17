package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"code/internal/domain"
	"code/internal/service"
	"github.com/gorilla/websocket"
)

func newTestAuthService(users domain.UserRepository) *service.AuthService {
	return service.NewAuthService(users, service.NewTokenService("secret", time.Hour))
}

type fakeUserRepository struct {
	users map[string]domain.User
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[string]domain.User)}
}

func (repository *fakeUserRepository) FindByUsername(ctx context.Context, username string) (domain.User, bool, error) {
	user, exists := repository.users[username]
	return user, exists, nil
}

func (repository *fakeUserRepository) Create(ctx context.Context, user domain.User) error {
	repository.users[user.Username] = user
	return nil
}

type fakeMatchRepository struct {
	saved   []domain.MatchRecord
	matches []domain.MatchRecord
}

func (repository *fakeMatchRepository) Save(ctx context.Context, match domain.MatchRecord) error {
	repository.saved = append(repository.saved, match)
	return nil
}

func (repository *fakeMatchRepository) FindByPlayer(ctx context.Context, username string) ([]domain.MatchRecord, error) {
	return repository.matches, nil
}

func TestRegisterAndLoginRoutes(t *testing.T) {
	userRepository := newFakeUserRepository()
	matchRepository := &fakeMatchRepository{}
	server := NewServer(newTestAuthService(userRepository), matchRepository, service.NewRoomManager())
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	registerBody := bytes.NewBufferString(`{"username":"ana","password":"segredo"}`)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/register", registerBody)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected register status 201, got %d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/register", bytes.NewBufferString(`{"username":"ana","password":"segredo"}`))
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected duplicate register status 409, got %d", response.Code)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"ana","password":"segredo"}`))
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d body=%s", response.Code, response.Body.String())
	}

	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode login response failed: %v", err)
	}
	if payload["message"] != "Login autorizado" || payload["token"] == "" {
		t.Fatalf("unexpected login payload: %#v", payload)
	}
}

func TestHistoryAndRoomsRoutes(t *testing.T) {
	userRepository := newFakeUserRepository()
	matchRepository := &fakeMatchRepository{
		matches: []domain.MatchRecord{{
			ID:        "sala-1",
			Mode:      "1v1",
			WhiteName: "ana",
			BlackName: "bia",
			Moves:     []string{"e2e4"},
		}},
	}
	roomManager := service.NewRoomManager()
	_, err := roomManager.JoinRoom("sala-1", "ana", "2v2", "", "client-1")
	if err != nil {
		t.Fatalf("join room failed: %v", err)
	}

	server := NewServer(newTestAuthService(userRepository), matchRepository, roomManager)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/history?user=ana", nil)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected history status 200, got %d", response.Code)
	}

	var matches []domain.MatchRecord
	if err := json.NewDecoder(response.Body).Decode(&matches); err != nil {
		t.Fatalf("decode history response failed: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != "sala-1" {
		t.Fatalf("unexpected history response: %#v", matches)
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected rooms status 200, got %d", response.Code)
	}

	var rooms []domain.RoomInfo
	if err := json.NewDecoder(response.Body).Decode(&rooms); err != nil {
		t.Fatalf("decode rooms response failed: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != "sala-1" || rooms[0].Jogadores != 1 {
		t.Fatalf("unexpected rooms response: %#v", rooms)
	}
}

func TestCORSPreflight(t *testing.T) {
	server := NewServer(newTestAuthService(newFakeUserRepository()), &fakeMatchRepository{}, service.NewRoomManager())
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/api/login", nil)
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected preflight status 200, got %d", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing CORS header: %#v", response.Header())
	}
}

func TestPlayWebSocketBroadcastsStateAndPersistsMove(t *testing.T) {
	matchRepository := &fakeMatchRepository{}
	server := NewServer(newTestAuthService(newFakeUserRepository()), matchRepository, service.NewRoomManager())
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listener unavailable in this environment: %v", err)
	}
	httpServer := httptest.NewUnstartedServer(mux)
	httpServer.Listener = listener
	httpServer.Start()
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws/play?room=sala-1&user=ana&mode=1v1"
	whiteConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial white websocket failed: %v", err)
	}
	defer whiteConn.Close()

	var whiteState domain.WSResponse
	if err := whiteConn.ReadJSON(&whiteState); err != nil {
		t.Fatalf("read initial white state failed: %v", err)
	}
	if whiteState.PlayerCount != 1 || whiteState.Players["w1"] != "ana" {
		t.Fatalf("unexpected initial white state: %#v", whiteState)
	}

	wsURL = "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws/play?room=sala-1&user=bia&mode=1v1"
	blackConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial black websocket failed: %v", err)
	}
	defer blackConn.Close()

	if err := whiteConn.ReadJSON(&whiteState); err != nil {
		t.Fatalf("read broadcast white state failed: %v", err)
	}
	var blackState domain.WSResponse
	if err := blackConn.ReadJSON(&blackState); err != nil {
		t.Fatalf("read initial black state failed: %v", err)
	}
	if whiteState.PlayerCount != 2 || blackState.PlayerCount != 2 {
		t.Fatalf("expected both clients to see two players, white=%d black=%d", whiteState.PlayerCount, blackState.PlayerCount)
	}
	if blackState.Players["b1"] != "bia" {
		t.Fatalf("expected black player assignment, got %#v", blackState.Players)
	}

	if err := whiteConn.WriteJSON(domain.WSMessage{Move: "e2e4"}); err != nil {
		t.Fatalf("write move failed: %v", err)
	}
	if err := whiteConn.ReadJSON(&whiteState); err != nil {
		t.Fatalf("read white move broadcast failed: %v", err)
	}
	if err := blackConn.ReadJSON(&blackState); err != nil {
		t.Fatalf("read black move broadcast failed: %v", err)
	}
	if whiteState.FEN != blackState.FEN || whiteState.Turn != "Black" {
		t.Fatalf("unexpected state after move: white=%#v black=%#v", whiteState, blackState)
	}
	if len(matchRepository.saved) != 1 || len(matchRepository.saved[0].Moves) != 1 || matchRepository.saved[0].Moves[0] != "e2e4" {
		t.Fatalf("expected move to be persisted once, got %#v", matchRepository.saved)
	}
}
