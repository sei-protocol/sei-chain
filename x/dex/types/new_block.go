package types

type SudoNewBlockMsg struct {
	NewBlock NewBlockRequest `json:"new_block"`
}

type NewBlockRequest struct {
	Epoch int64 `json:"epoch"`
}
