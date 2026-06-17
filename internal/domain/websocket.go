package domain

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
	DrawOffer     string            `json:"draw_offer"`
}
