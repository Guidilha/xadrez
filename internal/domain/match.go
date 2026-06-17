package domain

type MatchRecord struct {
	ID         string   `json:"id"`
	Mode       string   `json:"mode"`
	CurrentFEN string   `json:"current_fen"`
	WhiteName  string   `json:"white_name"`
	BlackName  string   `json:"black_name"`
	W2Name     string   `json:"w2_name"`
	B2Name     string   `json:"b2_name"`
	W3Name     string   `json:"w3_name"`
	B3Name     string   `json:"b3_name"`
	Status     string   `json:"status"`
	Date       string   `json:"date"`
	Moves      []string `json:"moves"`
}
