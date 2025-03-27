package evmrpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sei-protocol/sei-chain/mev"
)

type MevAPI struct {
	handler mev.MEVHandler
}

func NewMevAPI(handler mev.MEVHandler) *MevAPI {
	return &MevAPI{handler: handler}
}

func (i *MevAPI) Submission(ctx context.Context, req json.RawMessage) (res json.RawMessage, err error) {
	if i.handler == nil {
		return nil, errors.New("this node does not support MEV submission")
	}
	return i.handler.RPCSubmission(ctx, req)
}
