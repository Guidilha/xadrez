package domain

import "github.com/corentings/chess"

type ClientInfo struct {
	Username string
	Role     string
}

type Room struct {
	Mode          string
	MaxPlayers    int
	Game          *chess.Game
	Moves         []string
	ProposedMoves map[string]string
	DrawOffer     string
}

type RoomInfo struct {
	ID        string `json:"id"`
	Nome      string `json:"nome"`
	Jogadores int    `json:"jogadores"`
	Max       int    `json:"max"`
	Mode      string `json:"mode"`
}
