package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"code/internal/domain"
)

type fakeMatchRepository struct {
	saved []domain.MatchRecord
}

func (repository *fakeMatchRepository) Save(ctx context.Context, match domain.MatchRecord) error {
	repository.saved = append(repository.saved, match)
	return nil
}

func (repository *fakeMatchRepository) FindByPlayer(ctx context.Context, username string) ([]domain.MatchRecord, error) {
	return nil, nil
}

func TestAssignRoleRespectsModeAndPreferredTeam(t *testing.T) {
	room := NewRoom("2v2")
	room.Clients[connKey(0)] = &domain.ClientInfo{Username: "ana", Role: "w1"}

	role := room.AssignRole("w")
	if role != "w2" {
		t.Fatalf("expected w2, got %q", role)
	}

	room.Clients[connKey(1)] = &domain.ClientInfo{Username: "bia", Role: "w2"}
	role = room.AssignRole("w")
	if role != "" {
		t.Fatalf("expected no white role available, got %q", role)
	}
}

func TestRoomManagerJoinRoomCapacityAndAvailableRooms(t *testing.T) {
	manager := NewRoomManager()

	firstRoom, err := manager.JoinRoom("sala-1", "ana", "1v1", "", connKey(1))
	if err != nil {
		t.Fatalf("join first player failed: %v", err)
	}

	secondRoom, err := manager.JoinRoom("sala-1", "bia", "1v1", "", connKey(2))
	if err != nil {
		t.Fatalf("join second player failed: %v", err)
	}
	if firstRoom != secondRoom {
		t.Fatal("expected both players to join the same room instance")
	}

	_, err = manager.JoinRoom("sala-1", "caio", "1v1", "", connKey(3))
	if !errors.Is(err, ErrRoomFull) {
		t.Fatalf("expected ErrRoomFull, got %v", err)
	}

	if rooms := manager.AvailableRooms(); len(rooms) != 0 {
		t.Fatalf("expected full room to be omitted, got %#v", rooms)
	}
}

func TestGet3v3RolesCyclesDeciderByFullTurn(t *testing.T) {
	room := NewRoom("3v3")

	propA, propB, decider := room.Get3v3Roles()
	if propA != "w2" || propB != "w3" || decider != "w1" {
		t.Fatalf("unexpected first white turn roles: %s %s %s", propA, propB, decider)
	}

	room.Moves = []string{"e2e4", "e7e5"}
	propA, propB, decider = room.Get3v3Roles()
	if propA != "w1" || propB != "w3" || decider != "w2" {
		t.Fatalf("unexpected second white turn roles: %s %s %s", propA, propB, decider)
	}
}

func TestExecuteFinalMovePersistsMatchAndClearsOpponentDrawOffer(t *testing.T) {
	room := NewRoom("1v1")
	room.Clients[connKey(0)] = &domain.ClientInfo{Username: "ana", Role: "w1"}
	room.Clients[connKey(1)] = &domain.ClientInfo{Username: "bia", Role: "b1"}
	room.DrawOffer = "b"
	repository := &fakeMatchRepository{}

	err := room.ExecuteFinalMove(context.Background(), repository, "sala-1", "e2e4", "w1")
	if err != nil {
		t.Fatalf("execute final move failed: %v", err)
	}

	if len(room.Moves) != 1 || room.Moves[0] != "e2e4" {
		t.Fatalf("expected move to be appended, got %#v", room.Moves)
	}
	if room.DrawOffer != "" {
		t.Fatalf("expected opponent draw offer to be cleared, got %q", room.DrawOffer)
	}
	if len(repository.saved) != 1 {
		t.Fatalf("expected one saved match, got %d", len(repository.saved))
	}
	if repository.saved[0].WhiteName != "ana" || repository.saved[0].BlackName != "bia" {
		t.Fatalf("unexpected saved players: %#v", repository.saved[0])
	}
}

func TestHandleMessage1v1EnforcesActiveRole(t *testing.T) {
	room := NewRoom("1v1")
	whiteConn := connKey(1)
	blackConn := connKey(2)
	room.Clients[whiteConn] = &domain.ClientInfo{Username: "ana", Role: "w1"}
	room.Clients[blackConn] = &domain.ClientInfo{Username: "bia", Role: "b1"}
	repository := &fakeMatchRepository{}

	shouldBroadcast, err := room.HandleMessage(context.Background(), repository, "sala-1", blackConn, domain.WSMessage{Move: "e7e5"})
	if err != nil {
		t.Fatalf("unexpected error for inactive role: %v", err)
	}
	if shouldBroadcast {
		t.Fatal("expected inactive player move not to broadcast")
	}
	if len(room.Moves) != 0 || len(repository.saved) != 0 {
		t.Fatalf("inactive player changed state: moves=%#v saved=%#v", room.Moves, repository.saved)
	}

	shouldBroadcast, err = room.HandleMessage(context.Background(), repository, "sala-1", whiteConn, domain.WSMessage{Move: "e2e4"})
	if err != nil {
		t.Fatalf("expected active move to succeed: %v", err)
	}
	if !shouldBroadcast {
		t.Fatal("expected valid move to broadcast")
	}
	if len(room.Moves) != 1 || room.Moves[0] != "e2e4" {
		t.Fatalf("expected move e2e4 to be persisted in room, got %#v", room.Moves)
	}
	if len(repository.saved) != 1 {
		t.Fatalf("expected one saved match, got %d", len(repository.saved))
	}
}

func TestHandleMessage3v3ProposalPrivacyAndDecider(t *testing.T) {
	room := NewRoom("3v3")
	w1 := connKey(1)
	w2 := connKey(2)
	w3 := connKey(3)
	room.Clients[w1] = &domain.ClientInfo{Username: "lider", Role: "w1"}
	room.Clients[w2] = &domain.ClientInfo{Username: "p2", Role: "w2"}
	room.Clients[w3] = &domain.ClientInfo{Username: "p3", Role: "w3"}
	repository := &fakeMatchRepository{}

	shouldBroadcast, err := room.HandleMessage(context.Background(), repository, "sala-3", w2, domain.WSMessage{Move: "e2e4"})
	if err != nil {
		t.Fatalf("first proposal failed: %v", err)
	}
	if !shouldBroadcast {
		t.Fatal("expected first proposal to broadcast voting state")
	}

	shouldBroadcast, err = room.HandleMessage(context.Background(), repository, "sala-3", w3, domain.WSMessage{Move: "d2d4"})
	if err != nil {
		t.Fatalf("second proposal failed: %v", err)
	}
	if !shouldBroadcast {
		t.Fatal("expected second proposal to broadcast decider state")
	}

	responses := room.Responses()
	leaderResponse := responses[w1]
	if leaderResponse.ProposedMoves["w2"] != "e2e4" || leaderResponse.ProposedMoves["w3"] != "d2d4" {
		t.Fatalf("leader should see both proposals, got %#v", leaderResponse.ProposedMoves)
	}
	proposerResponse := responses[w2]
	if proposerResponse.ProposedMoves["w2"] != "e2e4" || proposerResponse.ProposedMoves["w3"] != "voted" {
		t.Fatalf("proposer should see own move and hidden peer vote, got %#v", proposerResponse.ProposedMoves)
	}
	if len(leaderResponse.ActiveRoles) != 1 || leaderResponse.ActiveRoles[0] != "w1" {
		t.Fatalf("expected decider to be active, got %#v", leaderResponse.ActiveRoles)
	}

	shouldBroadcast, err = room.HandleMessage(context.Background(), repository, "sala-3", w1, domain.WSMessage{Move: "d2d4"})
	if err != nil {
		t.Fatalf("decider move failed: %v", err)
	}
	if !shouldBroadcast {
		t.Fatal("expected decider move to broadcast")
	}
	if len(room.Moves) != 1 || room.Moves[0] != "d2d4" {
		t.Fatalf("expected decided move d2d4, got %#v", room.Moves)
	}
	if len(room.ProposedMoves) != 0 {
		t.Fatalf("expected proposals to be cleared after final move, got %#v", room.ProposedMoves)
	}
	if len(repository.saved) != 1 {
		t.Fatalf("expected one saved match after decider move, got %d", len(repository.saved))
	}
}

func TestRemoveClientResetsRoomOrDeletesWhenEmpty(t *testing.T) {
	manager := NewRoomManager()
	first := connKey(1)
	second := connKey(2)
	room, err := manager.JoinRoom("sala-1", "ana", "1v1", "", first)
	if err != nil {
		t.Fatalf("join first player failed: %v", err)
	}
	if _, err := manager.JoinRoom("sala-1", "bia", "1v1", "", second); err != nil {
		t.Fatalf("join second player failed: %v", err)
	}

	room.Moves = []string{"e2e4"}
	room.DrawOffer = "w"
	shouldBroadcast := manager.RemoveClient("sala-1", room, first)
	if !shouldBroadcast {
		t.Fatal("expected remaining clients to receive reset state")
	}
	if len(room.Moves) != 0 || room.DrawOffer != "" {
		t.Fatalf("expected room to reset after one client leaves, moves=%#v draw=%q", room.Moves, room.DrawOffer)
	}

	shouldBroadcast = manager.RemoveClient("sala-1", room, second)
	if shouldBroadcast {
		t.Fatal("expected no broadcast after last client leaves")
	}
	if rooms := manager.AvailableRooms(); len(rooms) != 0 {
		t.Fatalf("expected deleted room not to be listed, got %#v", rooms)
	}
}

func connKey(n int) string {
	return fmt.Sprintf("client-%d", n)
}
