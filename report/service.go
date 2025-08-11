package report

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"golang.org/x/sync/errgroup"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type Service interface {
	Start(ctx sdk.Context) error
	Name() string
	Status() string
}

type service struct {
	bk bankkeeper.Keeper
	ak *accountkeeper.AccountKeeper
	ek *evmkeeper.Keeper
	wk *wasmkeeper.Keeper

	ctx       sdk.Context
	outputDir string
	status    string
}

func (s *service) Name() string {
	return s.outputDir
}

func jsonRow(i interface{}) string {
	b, _ := json.Marshal(i)
	return fmt.Sprintf("%s\n", string(b))
}

func (s *service) openFile(name string) *os.File {
	file, err := os.Create(fmt.Sprintf("%s/%s", s.outputDir, name))
	if err != nil {
		panic(err)
	}
	return file
}

func (s *service) Status() string {
	return s.status
}

func (s *service) Start(ctx sdk.Context) error {
	s.status = "processing"
	grp, gctx := errgroup.WithContext(ctx.Context())
	ctx = ctx.WithContext(gctx)

	grp.Go(func() error {
		return s.exportAccounts(ctx)
	})

	grp.Go(func() error {
		return s.exportCoins(ctx)
	})

	grp.Go(func() error {
		return s.exportTokens(ctx)
	})

	err := grp.Wait()

	if err != nil {
		s.status = err.Error()
	} else {
		s.status = "success"
	}

	return err
}

func NewService(
	bk bankkeeper.Keeper,
	ak *accountkeeper.AccountKeeper,
	ek *evmkeeper.Keeper,
	wk *wasmkeeper.Keeper) Service {
	outputDir := fmt.Sprintf("/tmp/report-%d", time.Now().UnixNano())

	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		panic(err)
	}

	return &service{
		bk:        bk,
		ak:        ak,
		ek:        ek,
		wk:        wk,
		outputDir: outputDir,
	}
}
