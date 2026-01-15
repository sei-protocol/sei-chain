package p2p

import (
	"context"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

type GigaRouter struct {

}

func NewGigaRouter() *GigaRouter {
	return &GigaRouter{}
}

func (r *GigaRouter) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		return nil
	})
}
