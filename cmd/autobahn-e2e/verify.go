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

func (a *application) newVerifyConservationCommand() *cobra.Command {
	options := verifyConservationOptions{
		initialWei:    evmonlyInitialBalanceWei,
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
and every balance to stay within [0, initial-balance].`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.verifyConservation(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.name, "name", defaultClusterName, "cluster to verify")
	cmd.Flags().StringVar(&options.accountsFile, "accounts-file", "", "JSON file listing addresses and optional expected balances")
	cmd.Flags().StringVar(&options.initialWei, "initial-balance-wei", options.initialWei, "implicit starting balance for unseen EVM-only accounts")
	cmd.Flags().IntVar(&options.maxSampleSize, "sample-size", options.maxSampleSize, "maximum number of addresses to query")
	_ = cmd.MarkFlagRequired("accounts-file")
	return cmd
}

const evmonlyInitialBalanceWei = "1606931358149371912483291383930117527567243605732557312714412456632766846976"

func (a *application) verifyConservation(ctx context.Context, options verifyConservationOptions) error {
	if options.accountsFile == "" {
		return fmt.Errorf("accounts-file is required")
	}
	accounts, err := loadConservationAccountsFile(options.accountsFile)
	if err != nil {
		return err
	}
	if len(accounts.Addresses) == 0 {
		return fmt.Errorf("accounts file %q lists no addresses", options.accountsFile)
	}
	if options.maxSampleSize > 0 && len(accounts.Addresses) > options.maxSampleSize {
		accounts.Addresses = accounts.Addresses[:options.maxSampleSize]
	}
	initialBalance, ok := new(big.Int).SetString(options.initialWei, 10)
	if !ok {
		return fmt.Errorf("initial-balance-wei %q is not a decimal integer", options.initialWei)
	}

	state, err := a.store().load(options.name)
	if err != nil {
		return err
	}
	reports, err := a.inspectCluster(ctx, state)
	if err != nil {
		return err
	}
	endpoints := make([]string, 0, len(reports))
	for _, report := range reports {
		if report.Status != "running" || report.EVMTarget == "" {
			continue
		}
		endpoints = append(endpoints, "http://"+report.EVMTarget)
	}
	if len(endpoints) == 0 {
		return fmt.Errorf("cluster %q has no running nodes with EVM RPC targets", options.name)
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
	for _, addressHex := range accounts.Addresses {
		if !common.IsHexAddress(addressHex) {
			return fmt.Errorf("address %q is not a hex EVM address", addressHex)
		}
		address := common.HexToAddress(addressHex)
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
		if reference.Sign() < 0 {
			return fmt.Errorf("balance for %s is negative: %s", address, reference)
		}
		if reference.Cmp(initialBalance) > 0 {
			return fmt.Errorf("balance for %s exceeds initial funding %s: got %s",
				address, initialBalance, reference)
		}
		if expected, ok := accounts.ExpectedBalances[strings.ToLower(addressHex)]; ok {
			want, parseErr := parseWeiDecimal(expected)
			if parseErr != nil {
				return fmt.Errorf("expected balance for %s: %w", address, parseErr)
			}
			if reference.Cmp(want) != 0 {
				return fmt.Errorf("balance for %s is %s, expected %s", address, reference, want)
			}
		} else if expected, ok := accounts.ExpectedBalances[addressHex]; ok {
			want, parseErr := parseWeiDecimal(expected)
			if parseErr != nil {
				return fmt.Errorf("expected balance for %s: %w", address, parseErr)
			}
			if reference.Cmp(want) != 0 {
				return fmt.Errorf("balance for %s is %s, expected %s", address, reference, want)
			}
		}
		sum.Add(sum, reference)
	}

	if accounts.ExpectedSum != "" {
		wantSum, parseErr := parseWeiDecimal(accounts.ExpectedSum)
		if parseErr != nil {
			return fmt.Errorf("expected_sum: %w", parseErr)
		}
		if sum.Cmp(wantSum) != 0 {
			return fmt.Errorf("sum of queried balances is %s, expected %s", sum, wantSum)
		}
	}

	_, _ = fmt.Fprintf(a.stdout, "verified %d addresses across %d endpoints; sum=%s wei\n",
		len(accounts.Addresses), len(endpoints), sum)
	return nil
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
