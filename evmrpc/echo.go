package evmrpc

type EchoAPI struct{}

func NewEchoAPI() *EchoAPI {
	return &EchoAPI{}
}

func (a *EchoAPI) Echo(data string) string {
	return data
}
