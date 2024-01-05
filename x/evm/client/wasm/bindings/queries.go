package bindings

type SeiEVMQuery struct {
	StaticCall *StaticCallRequest `json:"static_call,omitempty"`
}

type StaticCallRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	Data []byte `json:"data"`
}
