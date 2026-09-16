package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/spf13/cobra"
)

// evmOnlyBaseBalanceShift is the power of two every EVM-only account starts with.
// It mirrors evmOnlyBaseBalance in sei-tendermint/internal/evmonlyapp, which this
// command cannot import across the internal boundary.
const evmOnlyBaseBalanceShift = 200

// evmOnlyInitialBalance returns the implicit starting balance of an EVM-only account.
func evmOnlyInitialBalance() *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), evmOnlyBaseBalanceShift)
}

type verifyConservationOptions struct {
	name          string
	accountsFile  string
	initialWei    string
	maxSampleSize int
}

type conservationAccountsFile struct {
	Addresses        []string          `json:"addresses"`
	ExpectedBalances map[string]string `json:"expected_balances,omitempty"`
	ExpectedSum      string            `json:"expected_sum,omitempty"`
}

// conservationLedger is an accounts file resolved into the sample to query and the
// assertions to make about it: what each address must hold, what they must add up
// to, and the ceiling no account may exceed.
type conservationLedger struct {
	addresses      []common.Address
	expected       map[common.Address]*big.Int
	expectedSum    *big.Int
	initialBalance *big.Int
}

func (a *application) newVerifyConservationCommand() *cobra.Command {
	options := verifyConservationOptions{
		initialWei:    evmOnlyInitialBalance().String(),
		maxSampleSize: 64,
	}
	cmd := &cobra.Command{
		Use:   "verify-conservation",
		Short: "Verify EVM-only balance conservation across cluster RPC endpoints",
		Long: strings.TrimSpace(`
Compare eth_getBalance results across every validator in a cluster.

The accounts file lists addresses to check. Optional expected_balances and
expected_sum entries let a run compare against a precomputed ledger. When
expected values are omitted, the command still requires every endpoint to agree
and every balance to stay at or below the initial balance.`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.verifyConservation(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.name, "name", defaultClusterName, "cluster to verify")
	cmd.Flags().StringVar(&options.accountsFile, "accounts-file", "", "JSON file listing addresses and optional expected balances")
	cmd.Flags().StringVar(&options.initialWei, "initial-balance-wei", options.initialWei, "implicit starting balance for unseen EVM-only accounts")
	cmd.Flags().IntVar(&options.maxSampleSize, "sample-size", options.maxSampleSize, "maximum number of addresses to query; 0 queries every address in the file")
	_ = cmd.MarkFlagRequired("accounts-file")
	return cmd
}

func (a *application) verifyConservation(ctx context.Context, options verifyConservationOptions) error {
	ledger, err := loadConservationLedger(options)
	if err != nil {
		return err
	}
	endpoints, err := a.evmEndpoints(ctx, options.name)
	if err != nil {
		return err
	}

	clients := make([]*ethrpc.Client, len(endpoints))
	defer func() {
		for _, client := range clients {
			if client != nil {
				client.Close()
			}
		}
	}()
	for i, endpoint := range endpoints {
		client, dialErr := ethrpc.DialContext(ctx, endpoint)
		if dialErr != nil {
			return fmt.Errorf("dial %s: %w", endpoint, dialErr)
		}
		clients[i] = client
	}

	sum := new(big.Int)
	for _, address := range ledger.addresses {
		reference, queryErr := queryBalance(ctx, clients[0], address)
		if queryErr != nil {
			return fmt.Errorf("query balance for %s on %s: %w", address, endpoints[0], queryErr)
		}
		for i := 1; i < len(clients); i++ {
			got, compareErr := queryBalance(ctx, clients[i], address)
			if compareErr != nil {
				return fmt.Errorf("query balance for %s on %s: %w", address, endpoints[i], compareErr)
			}
			if got.Cmp(reference) != 0 {
				return fmt.Errorf("balance mismatch for %s: %s has %s, %s has %s",
					address, endpoints[0], reference, endpoints[i], got)
			}
		}
		if err := ledger.checkBalance(address, reference); err != nil {
			return err
		}
		sum.Add(sum, reference)
	}
	if err := ledger.checkSum(sum); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(a.stdout, "verified %d addresses across %d endpoints; sum=%s wei\n",
		len(ledger.addresses), len(endpoints), sum)
	return nil
}

// evmEndpoints returns the EVM JSON-RPC URLs of every running node in a cluster.
func (a *application) evmEndpoints(ctx context.Context, name string) ([]string, error) {
	state, err := a.store().load(name)
	if err != nil {
		return nil, err
	}
	reports, err := a.inspectCluster(ctx, state)
	if err != nil {
		return nil, err
	}
	endpoints := make([]string, 0, len(reports))
	for _, report := range reports {
		if report.Status != "running" || report.EVMTarget == "" {
			continue
		}
		endpoints = append(endpoints, "http://"+report.EVMTarget)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("cluster %q has no running nodes with EVM RPC targets", name)
	}
	return endpoints, nil
}

// loadConservationLedger reads an accounts file and resolves it against the run's
// options. It refuses a file whose expected values cannot be checked as written,
// rather than checking a subset of them and reporting a pass.
func loadConservationLedger(options verifyConservationOptions) (conservationLedger, error) {
	if options.accountsFile == "" {
		return conservationLedger{}, fmt.Errorf("accounts-file is required")
	}
	file, err := loadConservationAccountsFile(options.accountsFile)
	if err != nil {
		return conservationLedger{}, err
	}
	if len(file.Addresses) == 0 {
		return conservationLedger{}, fmt.Errorf("accounts file %q lists no addresses", options.accountsFile)
	}
	initialBalance, ok := new(big.Int).SetString(options.initialWei, 10)
	if !ok {
		return conservationLedger{}, fmt.Errorf("initial-balance-wei %q is not a decimal integer", options.initialWei)
	}

	ledger := conservationLedger{
		addresses:      make([]common.Address, 0, len(file.Addresses)),
		expected:       make(map[common.Address]*big.Int, len(file.ExpectedBalances)),
		initialBalance: initialBalance,
	}
	listed := make(map[common.Address]struct{}, len(file.Addresses))
	for _, addressHex := range file.Addresses {
		if !common.IsHexAddress(addressHex) {
			return conservationLedger{}, fmt.Errorf("address %q is not a hex EVM address", addressHex)
		}
		address := common.HexToAddress(addressHex)
		listed[address] = struct{}{}
		ledger.addresses = append(ledger.addresses, address)
	}
	for addressHex, balance := range file.ExpectedBalances {
		if !common.IsHexAddress(addressHex) {
			return conservationLedger{}, fmt.Errorf("expected_balances key %q is not a hex EVM address", addressHex)
		}
		address := common.HexToAddress(addressHex)
		if _, ok := listed[address]; !ok {
			return conservationLedger{}, fmt.Errorf("expected_balances names %s, which the addresses list does not contain", address)
		}
		want, parseErr := parseWeiDecimal(balance)
		if parseErr != nil {
			return conservationLedger{}, fmt.Errorf("expected balance for %s: %w", address, parseErr)
		}
		ledger.expected[address] = want
	}
	if file.ExpectedSum != "" {
		wantSum, parseErr := parseWeiDecimal(file.ExpectedSum)
		if parseErr != nil {
			return conservationLedger{}, fmt.Errorf("expected_sum: %w", parseErr)
		}
		ledger.expectedSum = wantSum
	}

	// A truncated sample sums to less than the file says it should, so the sum
	// invariant and the sample limit cannot both be honoured.
	if options.maxSampleSize > 0 && len(ledger.addresses) > options.maxSampleSize {
		if ledger.expectedSum != nil {
			return conservationLedger{}, fmt.Errorf(
				"accounts file %q sets expected_sum over %d addresses but sample-size is %d: raise --sample-size or drop expected_sum",
				options.accountsFile, len(ledger.addresses), options.maxSampleSize)
		}
		ledger.addresses = ledger.addresses[:options.maxSampleSize]
	}
	return ledger, nil
}

// checkBalance reports whether a queried balance is consistent with the ledger.
func (l conservationLedger) checkBalance(address common.Address, balance *big.Int) error {
	if balance.Cmp(l.initialBalance) > 0 {
		return fmt.Errorf("balance for %s exceeds initial funding %s: got %s",
			address, l.initialBalance, balance)
	}
	if want, ok := l.expected[address]; ok && balance.Cmp(want) != 0 {
		return fmt.Errorf("balance for %s is %s, expected %s", address, balance, want)
	}
	return nil
}

// checkSum reports whether the queried balances add up to the expected total.
func (l conservationLedger) checkSum(sum *big.Int) error {
	if l.expectedSum == nil || sum.Cmp(l.expectedSum) == 0 {
		return nil
	}
	return fmt.Errorf("sum of queried balances is %s, expected %s", sum, l.expectedSum)
}

func loadConservationAccountsFile(path string) (conservationAccountsFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the operator invoking verify-conservation.
	if err != nil {
		return conservationAccountsFile{}, fmt.Errorf("read accounts file: %w", err)
	}
	var accounts conservationAccountsFile
	if err := json.Unmarshal(data, &accounts); err != nil {
		return conservationAccountsFile{}, fmt.Errorf("decode accounts file: %w", err)
	}
	return accounts, nil
}

func queryBalance(ctx context.Context, client *ethrpc.Client, address common.Address) (*big.Int, error) {
	var got hexutil.Big
	if err := client.CallContext(ctx, &got, "eth_getBalance", address, "latest"); err != nil {
		return nil, err
	}
	return got.ToInt(), nil
}

func parseWeiDecimal(value string) (*big.Int, error) {
	parsed, ok := new(big.Int).SetString(strings.TrimSpace(value), 10)
	if !ok {
		return nil, fmt.Errorf("%q is not a decimal integer", value)
	}
	return parsed, nil
}
