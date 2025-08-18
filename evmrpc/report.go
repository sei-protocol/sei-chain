package evmrpc

import (
	"sync"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	reports     map[string]report.Service // key is outputDir
}

func NewReportAPI(k *keeper.Keeper, wk *wasmkeeper.Keeper, ctxProvider func(int64) sdk.Context) *ReportAPI {
	return &ReportAPI{
		ctxProvider: ctxProvider,
		k:           k,
		wk:          wk,
		s:           make(map[string]reportStatus),
		reports:     make(map[string]report.Service),
	}
}

func (r *ReportAPI) Status(name string) (string, error) {
	r.mx.RLock()
	defer r.mx.RUnlock()
	svc, ok := r.reports[name]
	if !ok {
		return string(statusNotExists), nil
	}
	return svc.Status(), nil
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

func (r *ReportAPI) StartCSVReport(outputDir string) (string, error) {
	r.mx.Lock()
	defer r.mx.Unlock()
	ctx := r.ctxProvider(0)

	bk := r.k.BankKeeper()
	ak := r.k.AccountKeeper()

	s := report.NewService(bk, ak, r.k, r.wk, outputDir)
	r.reports[outputDir] = s // Use outputDir as the key

	ctx = ctx.WithBlockTime(time.Now())
	go func() {
		if err := s.Start(ctx); err != nil {
			ctx.Logger().Error("CSV report failed", "error", err)
		}
	}()

	return outputDir, nil // Return outputDir as the report identifier
}

func (r *ReportAPI) ExportToPostgreSQL(reportName string, host string, port int, database string, username string, password string, sslMode string) (string, error) {
	config := report.PostgreSQLConfig{
		Host:     host,
		Port:     port,
		Database: database,
		User:     username,
		Password: password,
		SSLMode:  sslMode,
	}

	go func() {
		ctx := r.ctxProvider(0)

		// Use reportName directly as the output directory
		importer, err := report.NewPostgreSQLImporter(config, reportName, ctx)
		if err != nil {
			ctx.Logger().Error("Failed to create PostgreSQL importer", "error", err, "report", reportName)
			return
		}
		defer importer.Close()

		// Import all CSV files to PostgreSQL
		if err := importer.ImportAll(); err != nil {
			ctx.Logger().Error("PostgreSQL import failed", "error", err, "report", reportName)
			return
		}

		ctx.Logger().Info("PostgreSQL import completed successfully", "report", reportName)
	}()

	return "PostgreSQL import started", nil
}
