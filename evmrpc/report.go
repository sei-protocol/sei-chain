package evmrpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/report"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type reportStatus string

var statusNotExists reportStatus = "not_exists"
var statusProcessing reportStatus = "processing"
var statusDone reportStatus = "done"

type ReportAPI struct {
	ctxProvider func(int64) sdk.Context
	k           *keeper.Keeper
	wk          *wasmkeeper.Keeper
	mx          sync.RWMutex
	s           map[string]reportStatus
	hasPointer  map[string]bool
	pointers    map[string]common.Address

	reports map[string]report.Service
}

func NewReportAPI(k *keeper.Keeper, wk *wasmkeeper.Keeper, ctxProvider func(int64) sdk.Context) *ReportAPI {
	return &ReportAPI{
		ctxProvider: ctxProvider,
		k:           k,
		wk:          wk,
		s:           make(map[string]reportStatus),
		hasPointer:  make(map[string]bool),
		pointers:    make(map[string]common.Address),
		reports:     make(map[string]report.Service),
	}
}

func (r *ReportAPI) Status(name string) (string, error) {
	r.mx.RLock()
	defer r.mx.RUnlock()
	status, ok := r.s[name]
	if !ok {
		return string(statusNotExists), nil
	}
	return string(status), nil
}

func (r *ReportAPI) ReportStatus(name string) (string, error) {
	r.mx.RLock()
	defer r.mx.RUnlock()
	svc, ok := r.reports[name]
	if !ok {
		return string(statusNotExists), nil
	}
	return svc.Status(), nil
}

type Report struct {
	Account       string        `json:"account"`
	Associated    bool          `json:"associated"`
	EVMAddress    string        `json:"evmAddress"`
	EVMNonce      uint64        `json:"evmNonce"`
	IsEVMContract bool          `json:"isEvmContract"`
	Coins         []*ReportCoin `json:"coins"`
	Error         error         `json:"error,omitempty"`
}

type ReportCoin struct {
	Denom      string  `json:"denom"`
	Amount     sdk.Int `json:"amount"`
	HasPointer bool    `json:"hasPointer"`
	Pointer    string  `json:"pointer,omitempty"`
}

func (r *ReportAPI) StartReport() (string, error) {
	r.mx.Lock()
	defer r.mx.Unlock()
	ctx := r.ctxProvider(0)

	bk := r.k.BankKeeper()
	ak := r.k.AccountKeeper()

	s := report.NewService(bk, ak, r.k, r.wk)
	r.reports[s.Name()] = s

	ctx = ctx.WithBlockTime(time.Now())
	go func() {
		err := s.Start(ctx)
		if err != nil {
			log.Printf("failed to start report: %v", err)
		}
	}()

	return s.Name(), nil
}

func (r *ReportAPI) CheckAssociations(name string) (string, error) {
	filename := "/tmp/associated.csv"
	outputfile, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}

	inputfile, err := os.Open(fmt.Sprintf("/tmp/%s", name))
	if err != nil {
		return "", err
	}
	ctx := r.ctxProvider(0)

	go func() {
		defer inputfile.Close()
		defer outputfile.Close()

		scanner := bufio.NewScanner(inputfile)
		// optionally, resize scanner's capacity for lines over 64K, see next example
		for scanner.Scan() {
			address := scanner.Text()
			if accAddr, err := sdk.AccAddressFromBech32(address); err == nil {
				evmAddr, associated := r.k.GetEVMAddress(ctx, accAddr)
				report := &Report{
					Account: address,
				}
				report.Associated = associated
				if !associated {
					defaultAddr := r.k.GetEVMAddressOrDefault(ctx, accAddr)
					report.EVMAddress = defaultAddr.Hex()
				} else {
					report.EVMAddress = evmAddr.Hex()
					report.EVMNonce = r.k.GetNonce(ctx, evmAddr)
					c := r.k.GetCode(ctx, evmAddr)
					report.IsEVMContract = len(c) > 0
				}

				reportJSON, err := json.Marshal(report)
				if err != nil {
					log.Printf("failed to marshal report: %v", err)
					continue
				}

				// Append the JSON line to the file
				if _, err := outputfile.WriteString(string(reportJSON) + "\n"); err != nil {
					log.Printf("failed to write report: %v", err)
					continue
				}
			}
		}

		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}()

	return filename, nil
}

func (r *ReportAPI) FindMultisigs(name string) (string, error) {
	s, err := r.Status(name)
	if s == string(statusNotExists) {
		r.mx.Lock()
		r.s[name] = statusProcessing
		r.mx.Unlock()
	} else if s == string(statusProcessing) {
		return "", fmt.Errorf("report is processing")
	} else if s == string(statusDone) {
		return "", fmt.Errorf("report is done")
	} else if err != nil {
		return "", err
	}

	ctx := r.ctxProvider(0)
	ak := r.k.AccountKeeper()

	// Open file for appending, create if not exists
	filename := fmt.Sprintf("/tmp/%s", name)
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}

	go func() {
		defer func() {
			file.Close()
			r.mx.Lock()
			r.s[name] = statusDone
			r.mx.Unlock()
		}()

		ak.IterateAccounts(ctx, func(account authtypes.AccountI) (stop bool) {
			if baseAcct, ok := account.(*authtypes.BaseAccount); ok {
				if _, multiOk := baseAcct.GetPubKey().(multisig.PubKey); multiOk {
					if _, err := file.WriteString(fmt.Sprintf("%s\n", baseAcct.GetAddress().String())); err != nil {
						log.Printf("failed to write report: %v", err)
					}
				}
			}
			return false
		})
	}()

	return filename, nil
}

func (r *ReportAPI) ExportBalances(name string) (string, error) {
	s, err := r.Status(name)
	if s == string(statusNotExists) {
		r.mx.Lock()
		r.s[name] = statusProcessing
		r.mx.Unlock()
	} else if s == string(statusProcessing) {
		return "", fmt.Errorf("report is processing")
	} else if s == string(statusDone) {
		return "", fmt.Errorf("report is done")
	} else if err != nil {
		return "", err
	}

	ctx := r.ctxProvider(0)
	bank := r.k.BankKeeper()

	// Open file for appending, create if not exists
	filename := fmt.Sprintf("/tmp/%s", name)
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}

	go func() {
		defer func() {
			file.Close()
			r.mx.Lock()
			r.s[name] = statusDone
			r.mx.Unlock()
		}()

		balances := bank.GetAccountsBalances(ctx)

		for _, b := range balances {
			report := &Report{
				Account: b.Address,
			}

			for _, c := range b.Coins {
				coin := &ReportCoin{
					Denom:  c.Denom,
					Amount: c.Amount,
				}

				r.decorateCoinWithDetails(c, ctx, coin)

				report.Coins = append(report.Coins, coin)
			}

			if accAddr, err := sdk.AccAddressFromBech32(b.Address); err == nil {
				evmAddr, associated := r.k.GetEVMAddress(ctx, accAddr)
				report.Associated = associated
				if !associated {
					defaultAddr := r.k.GetEVMAddressOrDefault(ctx, accAddr)
					report.EVMAddress = defaultAddr.Hex()
				} else {
					report.EVMAddress = evmAddr.Hex()
					report.EVMNonce = r.k.GetNonce(ctx, evmAddr)
					c := r.k.GetCode(ctx, evmAddr)
					report.IsEVMContract = len(c) > 0
				}
			} else {
				report.Error = err
			}

			// Marshal the report to JSON
			reportJSON, err := json.Marshal(report)
			if err != nil {
				log.Printf("failed to marshal report: %v", err)
				continue
			}

			// Append the JSON line to the file
			if _, err := file.WriteString(string(reportJSON) + "\n"); err != nil {
				log.Printf("failed to write report: %v", err)
				continue
			}
		}
	}()

	return filename, nil
}

func (r *ReportAPI) decorateCoinWithDetails(c sdk.Coin, ctx sdk.Context, coin *ReportCoin) {
	r.mx.Lock()
	defer r.mx.Unlock()
	if hasPointer, ok := r.hasPointer[c.Denom]; !ok {
		p, _, exists := r.k.GetERC20NativePointer(ctx, c.Denom)
		if exists {
			r.hasPointer[c.Denom] = true
			r.pointers[c.Denom] = p
			coin.HasPointer = true
			coin.Pointer = r.pointers[c.Denom].Hex()
		} else {
			r.hasPointer[c.Denom] = false
		}
	} else if hasPointer {
		coin.HasPointer = true
		coin.Pointer = r.pointers[c.Denom].Hex()
	}
}
