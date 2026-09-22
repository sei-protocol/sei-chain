package cosmosmetrics

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	distrtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	slashingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/slashing/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

// StakingKeeper is the staking state the collector reads.
type StakingKeeper interface {
	GetParams(ctx sdk.Context) stakingtypes.Params
	BondDenom(ctx sdk.Context) string
	GetAllValidators(ctx sdk.Context) []stakingtypes.Validator
	GetValidator(ctx sdk.Context, addr sdk.ValAddress) (stakingtypes.Validator, bool)
	GetBondedPool(ctx sdk.Context) authtypes.ModuleAccountI
	GetNotBondedPool(ctx sdk.Context) authtypes.ModuleAccountI
	GetAllDelegatorDelegations(ctx sdk.Context, delegator sdk.AccAddress) []stakingtypes.Delegation
	GetUnbondingDelegations(ctx sdk.Context, delegator sdk.AccAddress, maxRetrieve uint16) []stakingtypes.UnbondingDelegation
	GetRedelegations(ctx sdk.Context, delegator sdk.AccAddress, maxRetrieve uint16) []stakingtypes.Redelegation
}

// SlashingKeeper is the slashing state the collector reads.
type SlashingKeeper interface {
	GetParams(ctx sdk.Context) slashingtypes.Params
	GetValidatorSigningInfo(ctx sdk.Context, address sdk.ConsAddress) (slashingtypes.ValidatorSigningInfo, bool)
}

// DistributionKeeper is the distribution state the collector reads.
type DistributionKeeper interface {
	GetParams(ctx sdk.Context) distrtypes.Params
	GetFeePoolCommunityCoins(ctx sdk.Context) sdk.DecCoins
	DelegationTotalRewards(ctx context.Context, req *distrtypes.QueryDelegationTotalRewardsRequest) (*distrtypes.QueryDelegationTotalRewardsResponse, error)
}

// BankKeeper is the bank state the collector reads.
type BankKeeper interface {
	GetBalance(ctx sdk.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetSupply(ctx sdk.Context, denom string) sdk.Coin
}

// OracleKeeper is the oracle state the collector reads.
type OracleKeeper interface {
	GetVotePenaltyCounter(ctx sdk.Context, operator sdk.ValAddress) oracletypes.VotePenaltyCounter
}

// Keepers groups the module state the collector reads.
type Keepers struct {
	Staking      StakingKeeper
	Slashing     SlashingKeeper
	Distribution DistributionKeeper
	Bank         BankKeeper
	Oracle       OracleKeeper
}

// QueryContextFunc returns a read-only context over the latest committed state.
type QueryContextFunc func() (sdk.Context, error)

// maxWalletEntries bounds the unbonding and redelegation entries read per wallet.
const maxWalletEntries = 100

// Collector is a prometheus.Collector for the cosmos_* gauges, read from the node's own keepers.
type Collector struct {
	cfg      Config
	keepers  Keepers
	queryCtx QueryContextFunc
	logger   *slog.Logger
	wallets  []sdk.AccAddress
	scale    float64

	mu          sync.Mutex
	lastRefresh time.Time
	cached      []prometheus.Metric

	transfers *transferGauge
	now       func() time.Time
}

// NewCollector returns a Collector for cfg. cfg must have passed ReadConfig.
func NewCollector(cfg Config, keepers Keepers, queryCtx QueryContextFunc, logger *slog.Logger) (*Collector, error) {
	wallets := make([]sdk.AccAddress, 0, len(cfg.WalletAddresses))
	for _, addr := range cfg.WalletAddresses {
		acc, err := sdk.AccAddressFromBech32(addr)
		if err != nil {
			return nil, fmt.Errorf("wallet address %q: %w", addr, err)
		}
		wallets = append(wallets, acc)
	}
	return &Collector{
		cfg:       cfg,
		keepers:   keepers,
		queryCtx:  queryCtx,
		logger:    logger,
		wallets:   wallets,
		scale:     math.Pow10(int(cfg.DenomExponent)),
		transfers: newTransferGauge(cfg.BankTransferThreshold, transferTTL),
		now:       time.Now,
	}, nil
}

// Register adds the collector and the bank transfer gauge to reg.
func (c *Collector) Register(reg prometheus.Registerer) error {
	if err := reg.Register(c); err != nil {
		return err
	}
	if err := reg.Register(c.transfers.gauge); err != nil {
		reg.Unregister(c)
		return err
	}
	return nil
}

// Unregister removes what Register added.
func (c *Collector) Unregister(reg prometheus.Registerer) {
	reg.Unregister(c)
	reg.Unregister(c.transfers.gauge)
}

// Describe implements prometheus.Collector as an unchecked collector, since the set of label values
// is only known once state has been read.
func (c *Collector) Describe(chan<- *prometheus.Desc) {}

// Collect implements prometheus.Collector. State is re-read at most once per RefreshInterval.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.cached == nil || now.Sub(c.lastRefresh) >= c.cfg.RefreshInterval {
		if metrics, ok := c.read(); ok {
			c.cached = metrics
			c.lastRefresh = now
		}
	}
	for _, m := range c.cached {
		ch <- m
	}
}

// read collects every gauge from the latest committed state. A panic in any keeper read is
// recovered and reported as a failed read so a scrape never takes the node down.
func (c *Collector) read() (metrics []prometheus.Metric, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("cosmos metrics read panicked", "panic", r, "stack", string(debug.Stack()))
			metrics, ok = nil, false
		}
	}()
	ctx, err := c.queryCtx()
	if err != nil {
		c.logger.Error("cosmos metrics: no query context", "err", err)
		return nil, false
	}
	b := &builder{}
	bondDenom := c.keepers.Staking.BondDenom(ctx)
	c.readParams(ctx, b)
	c.readGeneral(ctx, b, bondDenom)
	c.readValidators(ctx, b, bondDenom)
	c.readWallets(ctx, b, bondDenom)
	for _, err := range b.errs {
		c.logger.Error("cosmos metrics: metric skipped", "err", err)
	}
	return b.metrics, true
}

func (c *Collector) readParams(ctx sdk.Context, b *builder) {
	staking := c.keepers.Staking.GetParams(ctx)
	b.gauge(descParamsMaxValidators, float64(staking.MaxValidators))
	b.gauge(descParamsUnbondingTime, staking.UnbondingTime.Seconds())

	slashing := c.keepers.Slashing.GetParams(ctx)
	b.gauge(descParamsDowntimeJailDuration, slashing.DowntimeJailDuration.Seconds())
	b.gauge(descParamsSignedBlocksWindow, float64(slashing.SignedBlocksWindow))
	b.dec(descParamsMinSignedPerWindow, slashing.MinSignedPerWindow)
	b.dec(descParamsSlashFractionDoubleSign, slashing.SlashFractionDoubleSign)
	b.dec(descParamsSlashFractionDowntime, slashing.SlashFractionDowntime)

	distr := c.keepers.Distribution.GetParams(ctx)
	b.dec(descParamsBaseProposerReward, distr.BaseProposerReward)
	b.dec(descParamsBonusProposerReward, distr.BonusProposerReward)
	b.dec(descParamsCommunityTax, distr.CommunityTax)
}

func (c *Collector) readGeneral(ctx sdk.Context, b *builder, bondDenom string) {
	bonded := c.keepers.Bank.GetBalance(ctx, c.keepers.Staking.GetBondedPool(ctx).GetAddress(), bondDenom)
	notBonded := c.keepers.Bank.GetBalance(ctx, c.keepers.Staking.GetNotBondedPool(ctx).GetAddress(), bondDenom)
	b.int(descGeneralBondedTokens, bonded.Amount, 1)
	b.int(descGeneralNotBondedTokens, notBonded.Amount, 1)
	for _, coin := range c.keepers.Distribution.GetFeePoolCommunityCoins(ctx) {
		b.decScaled(descGeneralCommunityPool, coin.Amount, c.scaleFor(coin.Denom, bondDenom), coin.Denom)
	}
	supply := c.keepers.Bank.GetSupply(ctx, bondDenom)
	b.int(descGeneralSupplyTotal, supply.Amount, c.scale, bondDenom)
}

func (c *Collector) readValidators(ctx sdk.Context, b *builder, bondDenom string) {
	validators := c.keepers.Staking.GetAllValidators(ctx)
	sort.SliceStable(validators, func(i, j int) bool {
		return validators[i].Tokens.GT(validators[j].Tokens)
	})
	for rank, v := range validators {
		addr, moniker := v.OperatorAddress, v.Description.Moniker
		b.dec(descValidatorsCommission, v.Commission.Rate, addr, moniker)
		b.gauge(descValidatorsStatus, float64(v.Status), addr, moniker)
		b.gauge(descValidatorsJailed, boolToFloat(v.Jailed), addr, moniker)
		b.int(descValidatorsTokens, v.Tokens, c.scale, addr, moniker, bondDenom)
		b.decScaled(descValidatorsDelegatorShares, v.DelegatorShares, c.scale, addr, moniker, bondDenom)
		b.int(descValidatorsMinSelfDelegation, v.MinSelfDelegation, c.scale, addr, moniker, bondDenom)
		b.gauge(descValidatorsRank, float64(rank+1), addr, moniker)

		consAddr, err := v.GetConsAddr()
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("validator %s: consensus address: %w", addr, err))
			continue
		}
		b.gauge(descValidatorsActive, boolToFloat(v.IsBonded()), addr, strings.ToUpper(hex.EncodeToString(consAddr)), moniker)
		if !v.IsBonded() {
			continue
		}
		if info, found := c.keepers.Slashing.GetValidatorSigningInfo(ctx, consAddr); found {
			b.gauge(descValidatorsMissedBlocks, float64(info.MissedBlocksCounter), addr, moniker)
		}
		penalty := c.keepers.Oracle.GetVotePenaltyCounter(ctx, v.GetOperator())
		b.gauge(descOracleVotePenaltyCount, float64(penalty.MissCount), addr, moniker, "miss")
		b.gauge(descOracleVotePenaltyCount, float64(penalty.AbstainCount), addr, moniker, "abstain")
		b.gauge(descOracleVotePenaltyCount, float64(penalty.SuccessCount), addr, moniker, "success")
	}
}

func (c *Collector) readWallets(ctx sdk.Context, b *builder, bondDenom string) {
	for _, acc := range c.wallets {
		addr := acc.String()
		balance := c.keepers.Bank.GetBalance(ctx, acc, bondDenom)
		b.int(descWalletBalance, balance.Amount, c.scale, addr, bondDenom)

		for _, d := range c.keepers.Staking.GetAllDelegatorDelegations(ctx, acc) {
			validator, found := c.keepers.Staking.GetValidator(ctx, d.GetValidatorAddr())
			if !found {
				continue
			}
			b.decScaled(descWalletDelegations, validator.TokensFromShares(d.Shares), c.scale, addr, bondDenom, d.ValidatorAddress)
		}
		for _, u := range c.keepers.Staking.GetUnbondingDelegations(ctx, acc, maxWalletEntries) {
			sum := sdk.ZeroInt()
			for _, e := range u.Entries {
				sum = sum.Add(e.Balance)
			}
			b.int(descWalletUnbondings, sum, c.scale, addr, bondDenom, u.ValidatorAddress)
		}
		for _, r := range c.keepers.Staking.GetRedelegations(ctx, acc, maxWalletEntries) {
			sum := sdk.ZeroInt()
			for _, e := range r.Entries {
				sum = sum.Add(e.InitialBalance)
			}
			b.int(descWalletRedelegations, sum, c.scale, addr, bondDenom, r.ValidatorSrcAddress, r.ValidatorDstAddress)
		}

		rewards, err := c.keepers.Distribution.DelegationTotalRewards(sdk.WrapSDKContext(ctx), &distrtypes.QueryDelegationTotalRewardsRequest{DelegatorAddress: addr})
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("wallet %s: rewards: %w", addr, err))
			continue
		}
		for _, r := range rewards.Rewards {
			for _, coin := range r.Reward {
				b.decScaled(descWalletRewards, coin.Amount, c.scaleFor(coin.Denom, bondDenom), addr, coin.Denom, r.ValidatorAddress)
			}
		}
	}
}

// scaleFor is the divisor for amounts of denom: the configured exponent for the bond denom, and
// unity for anything else.
func (c *Collector) scaleFor(denom, bondDenom string) float64 {
	if denom == bondDenom {
		return c.scale
	}
	return 1
}

// builder accumulates constant gauges and the errors of the ones it could not build.
type builder struct {
	metrics []prometheus.Metric
	errs    []error
}

func (b *builder) gauge(desc *prometheus.Desc, value float64, labels ...string) {
	m, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	if err != nil {
		b.errs = append(b.errs, err)
		return
	}
	b.metrics = append(b.metrics, m)
}

func (b *builder) int(desc *prometheus.Desc, value sdk.Int, scale float64, labels ...string) {
	f, _ := new(big.Float).SetInt(value.BigInt()).Float64()
	b.gauge(desc, f/scale, labels...)
}

func (b *builder) dec(desc *prometheus.Desc, value sdk.Dec, labels ...string) {
	b.decScaled(desc, value, 1, labels...)
}

func (b *builder) decScaled(desc *prometheus.Desc, value sdk.Dec, scale float64, labels ...string) {
	f, err := value.Float64()
	if err != nil {
		b.errs = append(b.errs, fmt.Errorf("%s: %w", desc, err))
		return
	}
	b.gauge(desc, f/scale, labels...)
}

func boolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
