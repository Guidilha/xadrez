package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"code/internal/domain"
	"github.com/corentings/chess"
)

var (
	ErrInvalidCredentials = errors.New("credenciais inválidas")
	ErrUserNotFound       = errors.New("usuário não encontrado")
	ErrInvalidPassword    = errors.New("senha inválida")
	ErrRoomFull           = errors.New("sala cheia")
	ErrTeamFull           = errors.New("equipe lotada")
	ErrClientNotFound     = errors.New("cliente não encontrado na sala")
)

type Room struct {
	*domain.Room
	Clients      map[string]*domain.ClientInfo
	RematchVotes map[string]bool

	mu sync.RWMutex
}

type RoomManager struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		rooms: make(map[string]*Room),
	}
}

func NewRoom(mode string) *Room {
	return &Room{
		Room: &domain.Room{
			Mode:          mode,
			MaxPlayers:    MaxPlayersForMode(mode),
			Game:          chess.NewGame(),
			Moves:         []string{},
			ProposedMoves: make(map[string]string),
		},
		Clients:      make(map[string]*domain.ClientInfo),
		RematchVotes: make(map[string]bool),
	}
}

func MaxPlayersForMode(mode string) int {
	switch mode {
	case "2v2":
		return 4
	case "3v3":
		return 6
	default:
		return 2
	}
}

func (manager *RoomManager) GetOrCreate(roomID string, mode string) *Room {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	room, exists := manager.rooms[roomID]
	if exists {
		return room
	}

	room = NewRoom(mode)
	manager.rooms[roomID] = room
	return room
}

func (manager *RoomManager) JoinRoom(roomID string, username string, mode string, team string, clientID string) (*Room, error) {
	room := manager.GetOrCreate(roomID, mode)

	room.Lock()
	defer room.Unlock()

	if len(room.Clients) >= room.MaxPlayers {
		return nil, ErrRoomFull
	}

	assignedRole := room.AssignRole(team)
	if assignedRole == "" {
		return nil, ErrTeamFull
	}

	room.Clients[clientID] = &domain.ClientInfo{Username: username, Role: assignedRole}
	return room, nil
}

func (manager *RoomManager) RemoveClient(roomID string, room *Room, clientID string) bool {
	room.Lock()
	delete(room.Clients, clientID)
	delete(room.RematchVotes, clientID)
	if len(room.Clients) == 0 {
		room.Unlock()
		manager.Delete(roomID)
		return false
	}

	room.ResetGame()
	room.Unlock()
	return true
}

func (manager *RoomManager) Delete(roomID string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	delete(manager.rooms, roomID)
}

func (manager *RoomManager) AvailableRooms() []domain.RoomInfo {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	activeRooms := make([]domain.RoomInfo, 0, len(manager.rooms))
	for id, room := range manager.rooms {
		room.RLock()
		if len(room.Clients) < room.MaxPlayers {
			activeRooms = append(activeRooms, domain.RoomInfo{
				ID:        id,
				Nome:      "Sala " + id,
				Jogadores: len(room.Clients),
				Max:       room.MaxPlayers,
				Mode:      room.Mode,
			})
		}
		room.RUnlock()
	}
	return activeRooms
}

func (room *Room) Lock() {
	room.mu.Lock()
}

func (room *Room) Unlock() {
	room.mu.Unlock()
}

func (room *Room) RLock() {
	room.mu.RLock()
}

func (room *Room) RUnlock() {
	room.mu.RUnlock()
}

func (room *Room) AssignRole(preferredTeam string) string {
	taken := make(map[string]bool, len(room.Clients))
	for _, client := range room.Clients {
		taken[client.Role] = true
	}

	order := roleOrder(preferredTeam)
	for _, role := range order {
		if taken[role] || !roleAllowedInMode(role, room.Mode) {
			continue
		}
		return role
	}
	return ""
}

func (room *Room) HandleMessage(ctx context.Context, matchRepository domain.MatchRepository, roomID string, clientID string, message domain.WSMessage) (bool, error) {
	switch message.Move {
	case "rematch":
		room.Lock()
		room.RematchVotes[clientID] = true
		if len(room.RematchVotes) == room.MaxPlayers {
			room.ResetGame()
		}
		room.Unlock()
		return true, nil
	case "resign":
		return room.handleResign(ctx, matchRepository, roomID, clientID)
	case "offer_draw":
		return room.handleDrawOffer(ctx, matchRepository, roomID, clientID)
	default:
		return room.handleMove(ctx, matchRepository, roomID, clientID, message.Move)
	}
}

func (room *Room) Get3v3Roles() (propA string, propB string, decider string) {
	turnIdx := len(room.Moves) / 2
	deciderNum := (turnIdx % 3) + 1

	if room.Game.Position().Turn() == chess.White {
		decider = fmt.Sprintf("w%d", deciderNum)
		if deciderNum == 1 {
			return "w2", "w3", decider
		}
		if deciderNum == 2 {
			return "w1", "w3", decider
		}
		return "w1", "w2", decider
	}

	decider = fmt.Sprintf("b%d", deciderNum)
	if deciderNum == 1 {
		return "b2", "b3", decider
	}
	if deciderNum == 2 {
		return "b1", "b3", decider
	}
	return "b1", "b2", decider
}

func (room *Room) ActiveRole1v1And2v2() string {
	moveCount := len(room.Moves)
	if room.Mode == "2v2" {
		return []string{"w1", "b1", "w2", "b2"}[moveCount%4]
	}
	return []string{"w1", "b1"}[moveCount%2]
}

func (room *Room) ExecuteFinalMove(ctx context.Context, matchRepository domain.MatchRepository, roomID string, uciMove string, role string) error {
	move, err := chess.UCINotation{}.Decode(room.Game.Position(), uciMove)
	if err != nil {
		return fmt.Errorf("falha ao decodificar lance UCI: %w", err)
	}

	if err := room.Game.Move(move); err != nil {
		return fmt.Errorf("falha ao executar lance: %w", err)
	}

	room.Moves = append(room.Moves, uciMove)
	room.ProposedMoves = make(map[string]string)

	teamThatMoved := string(role[0])
	if room.DrawOffer != "" && room.DrawOffer != teamThatMoved {
		room.DrawOffer = ""
	}

	return room.SaveMatch(ctx, matchRepository, roomID)
}

func (room *Room) SaveMatch(ctx context.Context, matchRepository domain.MatchRepository, roomID string) error {
	players := room.Players()
	match := domain.MatchRecord{
		ID:         roomID,
		Mode:       room.Mode,
		CurrentFEN: room.Game.FEN(),
		WhiteName:  players["w1"],
		BlackName:  players["b1"],
		W2Name:     players["w2"],
		B2Name:     players["b2"],
		W3Name:     players["w3"],
		B3Name:     players["b3"],
		Status:     room.Game.Outcome().String(),
		Date:       time.Now().Format("02/01/2006"),
		Moves:      append([]string(nil), room.Moves...),
	}

	if err := matchRepository.Save(ctx, match); err != nil {
		return fmt.Errorf("falha ao persistir partida: %w", err)
	}
	return nil
}

func (room *Room) Players() map[string]string {
	players := make(map[string]string, len(room.Clients))
	for _, info := range room.Clients {
		players[info.Role] = info.Username
	}
	return players
}

func (room *Room) Responses() map[string]domain.WSResponse {
	room.Lock()
	defer room.Unlock()

	validMoves := make([]string, 0, len(room.Game.ValidMoves()))
	for _, move := range room.Game.ValidMoves() {
		validMoves = append(validMoves, move.String())
	}

	players := room.Players()
	activeRoles := room.activeRoles()
	responses := make(map[string]domain.WSResponse, len(room.Clients))
	for clientID, info := range room.Clients {
		responses[clientID] = domain.WSResponse{
			FEN:           room.Game.FEN(),
			Turn:          room.Game.Position().Turn().Name(),
			Status:        room.Game.Outcome().String(),
			PlayerCount:   len(room.Clients),
			MaxPlayers:    room.MaxPlayers,
			ValidMoves:    validMoves,
			Players:       players,
			ActiveRoles:   activeRoles,
			ProposedMoves: room.filteredProposedMoves(info),
			RematchVotes:  len(room.RematchVotes),
			Mode:          room.Mode,
			DrawOffer:     room.DrawOffer,
		}
	}
	return responses
}

func (room *Room) ResetGame() {
	room.Game = chess.NewGame()
	room.Moves = []string{}
	room.ProposedMoves = make(map[string]string)
	room.RematchVotes = make(map[string]bool)
	room.DrawOffer = ""
}

func (room *Room) Resign(role string) {
	if strings.HasPrefix(role, "w") {
		room.Game.Resign(chess.White)
		return
	}
	room.Game.Resign(chess.Black)
}

func (room *Room) handleResign(ctx context.Context, matchRepository domain.MatchRepository, roomID string, clientID string) (bool, error) {
	room.Lock()
	info := room.Clients[clientID]
	if info == nil {
		room.Unlock()
		return false, ErrClientNotFound
	}

	room.Resign(info.Role)
	err := room.SaveMatch(ctx, matchRepository, roomID)
	room.Unlock()
	if err != nil {
		return true, err
	}
	return true, nil
}

func (room *Room) handleDrawOffer(ctx context.Context, matchRepository domain.MatchRepository, roomID string, clientID string) (bool, error) {
	room.Lock()
	info := room.Clients[clientID]
	if info == nil {
		room.Unlock()
		return false, ErrClientNotFound
	}

	role := info.Role
	team := string(role[0])
	var err error
	if room.DrawOffer == "" {
		room.DrawOffer = team
	} else if room.DrawOffer != team {
		room.Game.Draw(chess.DrawOffer)
		room.DrawOffer = ""
		err = room.SaveMatch(ctx, matchRepository, roomID)
	}

	room.Unlock()
	if err != nil {
		return true, err
	}
	return true, nil
}

func (room *Room) handleMove(ctx context.Context, matchRepository domain.MatchRepository, roomID string, clientID string, uciMove string) (bool, error) {
	room.Lock()
	defer room.Unlock()

	info := room.Clients[clientID]
	if info == nil {
		return false, ErrClientNotFound
	}

	if room.Mode == "3v3" {
		return room.handle3v3Move(ctx, matchRepository, roomID, info.Role, uciMove)
	}

	if info.Role != room.ActiveRole1v1And2v2() {
		return false, nil
	}

	if err := room.ExecuteFinalMove(ctx, matchRepository, roomID, uciMove, info.Role); err != nil {
		return true, err
	}
	return true, nil
}

func (room *Room) handle3v3Move(ctx context.Context, matchRepository domain.MatchRepository, roomID string, role string, uciMove string) (bool, error) {
	propA, propB, decider := room.Get3v3Roles()

	if role == propA || role == propB {
		room.ProposedMoves[role] = uciMove
		moveA := room.ProposedMoves[propA]
		moveB := room.ProposedMoves[propB]
		if moveA == "" || moveB == "" {
			return true, nil
		}
		if moveA != moveB {
			return true, nil
		}

		if err := room.ExecuteFinalMove(ctx, matchRepository, roomID, moveA, propA); err != nil {
			return true, err
		}
		return true, nil
	}

	if role != decider {
		return false, nil
	}

	moveA := room.ProposedMoves[propA]
	moveB := room.ProposedMoves[propB]
	if moveA == "" || moveB == "" || moveA == moveB {
		return false, nil
	}
	if uciMove != moveA && uciMove != moveB {
		return false, nil
	}

	if err := room.ExecuteFinalMove(ctx, matchRepository, roomID, uciMove, decider); err != nil {
		return true, err
	}
	return true, nil
}

func (room *Room) activeRoles() []string {
	if room.Mode != "3v3" {
		return []string{room.ActiveRole1v1And2v2()}
	}

	propA, propB, decider := room.Get3v3Roles()
	moveA := room.ProposedMoves[propA]
	moveB := room.ProposedMoves[propB]
	if moveA != "" && moveB != "" && moveA != moveB {
		return []string{decider}
	}
	return []string{propA, propB}
}

func (room *Room) filteredProposedMoves(info *domain.ClientInfo) map[string]string {
	filteredMoves := make(map[string]string)
	if room.Mode != "3v3" {
		return filteredMoves
	}

	propA, propB, decider := room.Get3v3Roles()
	if info.Role == decider {
		filteredMoves[propA] = room.ProposedMoves[propA]
		filteredMoves[propB] = room.ProposedMoves[propB]
		return filteredMoves
	}

	if room.ProposedMoves[propA] != "" {
		filteredMoves[propA] = "voted"
	}
	if room.ProposedMoves[propB] != "" {
		filteredMoves[propB] = "voted"
	}
	filteredMoves[info.Role] = room.ProposedMoves[info.Role]
	return filteredMoves
}

func roleOrder(preferredTeam string) []string {
	switch preferredTeam {
	case "w":
		return []string{"w1", "w2", "w3"}
	case "b":
		return []string{"b1", "b2", "b3"}
	default:
		return []string{"w1", "b1", "w2", "b2", "w3", "b3"}
	}
}

func roleAllowedInMode(role string, mode string) bool {
	switch mode {
	case "1v1":
		return role == "w1" || role == "b1"
	case "2v2":
		return role != "w3" && role != "b3"
	default:
		return true
	}
}
