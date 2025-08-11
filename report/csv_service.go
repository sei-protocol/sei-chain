package report

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

type PostgreSQLConfig struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
}

type CSVService interface {
	Service
	ExportToPostgreSQL(config PostgreSQLConfig) error
}

type csvService struct {
	bk bankkeeper.Keeper
	ak *accountkeeper.AccountKeeper
	ek *evmkeeper.Keeper
	wk *wasmkeeper.Keeper

	ctx       sdk.Context
	outputDir string
	status    string

	// Deduplication maps
	seenTokens   map[string]bool
	seenBalances map[string]bool
	mu           sync.RWMutex
}

func (s *csvService) Name() string {
	return s.outputDir
}

func (s *csvService) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *csvService) setStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// Bucket determination logic from the post-processing script
var gringottsAddresses = map[string]bool{
	"sei18qgau4n88tdaxu9y2t2y2px29yvwp50mk4xctp7grwfj7fkcdn8qvs9ry8": true,
	"sei19se8ass0qvpa2cc60ehnv5dtccznnn5m505cug5tg2gwsjqw5drqm5ptnx": true,
	"sei1letzrrlgdlrpxj6z279fx85hn5u34mm9nrc9hq4e6wxz5c79je2swt6x4a": true,
	"sei1w0fvamykx7v2e6n5x0e2s39m0jz3krejjkpmgc3tmnqdf8p9fy5syg05yv": true,
}

var seiMultisigs = map[string]bool{
	"sei1xt3u4l0nzulhqxtcqhqdmgzt0p76vlwzr84t2g": true,
	"sei1vlrvsppftvaqlf4sy5muaea8jtgs2afn7xfr0w": true,
	"sei1prndl4f7hg6nsdavrlk6a26ea9a4q4780zjfgp": true,
	"sei1xhxnad3c86q3d8ggsyu24j7r0y5k3ef4zcxtc6": true,
	"sei1rufv5d36yrc57gjjs0gfur7ltj3jnhcs2lhz88": true,
	"sei19ey2jrj5qyd68sa4a34w6v6vgf6tar0zpv0cf8": true,
	"sei13u95lctpvwzmqy3thkczrhx3t4eczx7890xzky": true,
	"sei1sdwkgny20e7t5gv0533w4re5mukuusdkhmy433": true,
	"sei1y7xkz75wpgnazl47ttm72kj06wfp9u4du3ejqt": true,
	"sei1z64wl5hfdjwwadwcgf65lkze9mznydkw5j9heh": true,
	"sei15nz8xv0efg4mlaq26cue3u808ghdy29jyd0gaz": true,
	"sei1hrps2v9kl0kmhr0jdge0whfx3ulpfzlu6ptnk4": true,
}

var burnAddress = map[string]bool{
	"sei1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq703fpu": true,
}

var bondingPool = map[string]bool{
	"sei1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3chcelk": true,
}

func (s *csvService) determineBucket(account *Account) string {
	switch {
	case bondingPool[account.Account]:
		return "bonding_pool"
	case burnAddress[account.Account]:
		return "burn_address"
	case seiMultisigs[account.Account]:
		return "sei_multisig"
	case account.IsMultisig:
		return "multisig"
	case gringottsAddresses[account.Account]:
		return "gringotts"
	case account.IsCWContract:
		return "cw_contract"
	case account.IsEVMContract:
		return "evm_contract"
	case account.IsAssociated && account.EVMNonce > 0:
		return "associated_evm"
	case account.IsAssociated && account.EVMNonce == 0:
		return "associated_sei"
	case !account.IsAssociated:
		return "unassociated"
	default:
		return "unknown"
	}
}

type AutoFlushWriter struct {
	*bufio.Writer
	file       *os.File
	writeCount int
	flushEvery int
}

func NewAutoFlushWriter(w io.Writer, flushEvery int) *AutoFlushWriter {
	file, ok := w.(*os.File)
	if !ok {
		file = nil
	}
	
	// Use smaller buffer size to reduce memory usage
	writer := bufio.NewWriterSize(w, 1024) // 1KB instead of default 4KB
	
	return &AutoFlushWriter{
		Writer:     writer,
		file:       file,
		writeCount: 0,
		flushEvery: flushEvery,
	}
}

func (w *AutoFlushWriter) Write(p []byte) (n int, err error) {
	n, err = w.Writer.Write(p)
	if err != nil {
		return n, err
	}

	w.writeCount++
	if w.writeCount%w.flushEvery == 0 {
		// Flush bufio buffer
		if flushErr := w.Writer.Flush(); flushErr != nil {
			return n, flushErr
		}
		
		// Force OS to flush to disk (sync) to prevent kernel buffer buildup
		if w.file != nil {
			if syncErr := w.file.Sync(); syncErr != nil {
				// Log but don't fail on sync errors
				// sync errors are often non-critical
			}
		}
	}

	return n, nil
}

func (s *csvService) openCSVFile(name string) (*os.File, *csv.Writer) {
	file, err := os.Create(fmt.Sprintf("%s/%s", s.outputDir, name))
	if err != nil {
		panic(err)
	}
	
	// Use more aggressive flushing (every 1000 writes instead of 10000)
	// to reduce memory pressure further
	autoFlushWriter := NewAutoFlushWriter(file, 1000)
	writer := csv.NewWriter(autoFlushWriter)
	return file, writer
}

func (s *csvService) Start(ctx sdk.Context) error {
	s.setStatus("processing")
	s.ctx = ctx

	grp, gctx := errgroup.WithContext(ctx.Context())
	ctx = ctx.WithContext(gctx)

	grp.Go(func() error {
		return s.exportAccountsCSV(ctx)
	})

	grp.Go(func() error {
		return s.exportAssetsCSV(ctx)
	})

	grp.Go(func() error {
		return s.exportAccountAssetsCSV(ctx)
	})

	err := grp.Wait()

	if err != nil {
		s.setStatus(err.Error())
	} else {
		s.setStatus("success")
	}

	return err
}

func (s *csvService) ExportToPostgreSQL(config PostgreSQLConfig) error {
	importer, err := NewPostgreSQLImporter(config, s.outputDir, s.ctx)
	if err != nil {
		return fmt.Errorf("failed to create PostgreSQL importer: %w", err)
	}
	defer importer.Close()

	s.ctx.Logger().Info("Starting PostgreSQL import", "outputDir", s.outputDir)

	if err := importer.ImportAll(); err != nil {
		return fmt.Errorf("failed to import data to PostgreSQL: %w", err)
	}

	s.ctx.Logger().Info("Successfully completed PostgreSQL import")
	return nil
}

func (s *csvService) getCoinByDenom(ctx sdk.Context, denom string) *Coin {
	coin := &Coin{
		Denom: denom,
	}

	p, _, exists := s.ek.GetERC20NativePointer(ctx, coin.Denom)
	if exists {
		coin.HasPointer = true
		coin.Pointer = p.Hex()
	}

	return coin
}

func (s *csvService) queryContract(addr sdk.AccAddress, ctx sdk.Context, query []byte, target interface{}) error {
	res, err := s.wk.QuerySmart(ctx, addr, query)
	if err != nil {
		return err
	}
	return json.Unmarshal(res, target)
}

func NewCSVService(
	bk bankkeeper.Keeper,
	ak *accountkeeper.AccountKeeper,
	ek *evmkeeper.Keeper,
	wk *wasmkeeper.Keeper,
	outputDir string) CSVService {

	if outputDir == "" {
		outputDir = fmt.Sprintf("/tmp/csv-report-%d", time.Now().UnixNano())
	}

	if err := os.MkdirAll(outputDir, os.ModePerm); err != nil {
		panic(err)
	}

	return &csvService{
		bk:           bk,
		ak:           ak,
		ek:           ek,
		wk:           wk,
		outputDir:    outputDir,
		seenTokens:   make(map[string]bool),
		seenBalances: make(map[string]bool),
	}
}
