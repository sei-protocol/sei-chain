package staking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/seilog"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	"github.com/sei-protocol/sei-chain/sei-cosmos/std"
	sdkstore "github.com/sei-protocol/sei-chain/sei-cosmos/store"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	authkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/keeper"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	bankkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/keeper"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	stakingkeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/keeper"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

const (
	bondDenom    = "usei"
	minterModule = "minter"

	// seiInUsei is one SEI in usei.
	seiInUsei = 1_000_000
)

// toWei returns usei in wei.
func toWei(usei sdk.Int) *big.Int {
	return new(big.Int).Mul(usei.BigInt(), weiPerUsei)
}

// sharesToWei returns x/staking shares, counted in usei, as the precompile
// counts them, in wei.
func sharesToWei(shares sdk.Dec) *big.Int {
	return new(big.Int).Mul(shares.BigInt(), weiPerUsei)
}

// v2Params returns x/staking's params for the precompile's p.
func v2Params(p chainparams.Staking) stakingtypes.Params {
	return stakingtypes.Params{
		UnbondingTime:                      time.Duration(p.UnbondingTime), //nolint:gosec // params bound it below MaxPeriod
		MaxValidators:                      p.MaxValidators,
		MaxEntries:                         p.MaxEntries,
		BondDenom:                          bondDenom,
		MinCommissionRate:                  sdk.ZeroDec(),
		MaxVotingPowerRatio:                sdk.NewDecFromBigIntWithPrec(p.MaxVotingPowerRatio, sdk.Precision),
		MaxVotingPowerEnforcementThreshold: sdk.NewIntFromBigInt(p.MaxVotingPowerEnforcementThreshold),
	}
}

// v2Chain is x/staking's keeper, with the auth and bank keepers it moves
// tokens through, over in-memory stores.
type v2Chain struct {
	t        *testing.T
	ctx      sdk.Context
	keeper   stakingkeeper.Keeper
	msgs     stakingtypes.MsgServer
	bank     bankkeeper.BaseKeeper
	accounts authkeeper.AccountKeeper
}

func newV2Chain(t *testing.T, params stakingtypes.Params, hooks stakingtypes.StakingHooks) *v2Chain {
	// x/staking logs every delegation the voting power ratio refuses as an error.
	seilog.SetLevel("cosmos/x/staking/keeper", slog.LevelError+1)
	keys := sdk.NewKVStoreKeys(authtypes.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey, paramstypes.StoreKey)
	tkey := sdk.NewTransientStoreKey(paramstypes.TStoreKey)
	db := dbm.NewMemDB()
	ms := sdkstore.NewCommitMultiStore(db)
	for _, key := range keys {
		ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	}
	ms.MountStoreWithDB(tkey, sdk.StoreTypeTransient, db)
	require.NoError(t, ms.LoadLatestVersion())
	ctx := sdk.NewContext(ms, tmproto.Header{}, false)

	registry := codectypes.NewInterfaceRegistry()
	std.RegisterInterfaces(registry)
	authtypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	paramsKeeper := paramskeeper.NewKeeper(cdc, codec.NewLegacyAmino(), keys[paramstypes.StoreKey], tkey)
	accounts := authkeeper.NewAccountKeeper(cdc, keys[authtypes.StoreKey], paramsKeeper.Subspace(authtypes.ModuleName),
		authtypes.ProtoBaseAccount, map[string][]string{
			minterModule:                   {authtypes.Minter},
			stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
			stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
		})
	bank := bankkeeper.NewBaseKeeper(cdc, keys[banktypes.StoreKey], accounts, paramsKeeper.Subspace(banktypes.ModuleName), map[string]bool{})
	bank.SetParams(ctx, banktypes.DefaultParams())
	keeper := stakingkeeper.NewKeeper(cdc, keys[stakingtypes.StoreKey], accounts, bank, paramsKeeper.Subspace(stakingtypes.ModuleName))
	keeper.SetParams(ctx, params)
	keeper.SetHooks(hooks)
	return &v2Chain{t: t, ctx: ctx, keeper: keeper, msgs: stakingkeeper.NewMsgServerImpl(keeper), bank: bank, accounts: accounts}
}

// setBlock makes the current block number, at unixNano.
func (c *v2Chain) setBlock(number, unixNano uint64) {
	c.ctx = c.ctx.WithBlockHeight(int64(number)).WithBlockTime(time.Unix(0, int64(unixNano))) //nolint:gosec // test heights and times fit
}

// fund mints amount usei to addr.
func (c *v2Chain) fund(addr common.Address, amount sdk.Int) {
	coins := sdk.NewCoins(sdk.NewCoin(bondDenom, amount))
	require.NoError(c.t, c.bank.MintCoins(c.ctx, minterModule, coins))
	require.NoError(c.t, c.bank.SendCoinsFromModuleToAccount(c.ctx, minterModule, sdk.AccAddress(addr[:]), coins))
}

// deliver runs msg as a transaction does: it fails msg's stateless checks
// before running it, and keeps no write of a run that fails.
func (c *v2Chain) deliver(msg interface{ ValidateBasic() error }, run func(context.Context) error) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	ctx, write := c.ctx.CacheContext()
	if err := run(sdk.WrapSDKContext(ctx)); err != nil {
		return err
	}
	write()
	return nil
}

func (c *v2Chain) createValidator(op common.Address, pubkey common.Hash, self, minSelf sdk.Int) error {
	zero := sdk.ZeroDec()
	msg, err := stakingtypes.NewMsgCreateValidator(sdk.ValAddress(op[:]), &ed25519.PubKey{Key: pubkey[:]}, sdk.NewCoin(bondDenom, self),
		stakingtypes.NewDescription(op.Hex(), "", "", "", ""), stakingtypes.NewCommissionRates(zero, zero, zero), minSelf)
	require.NoError(c.t, err)
	return c.deliver(msg, func(ctx context.Context) error {
		_, err := c.msgs.CreateValidator(ctx, msg)
		return err
	})
}

func (c *v2Chain) delegate(delegator, op common.Address, amount sdk.Int) error {
	msg := stakingtypes.NewMsgDelegate(sdk.AccAddress(delegator[:]), sdk.ValAddress(op[:]), sdk.NewCoin(bondDenom, amount))
	return c.deliver(msg, func(ctx context.Context) error {
		_, err := c.msgs.Delegate(ctx, msg)
		return err
	})
}

func (c *v2Chain) undelegate(delegator, op common.Address, amount sdk.Int) error {
	msg := stakingtypes.NewMsgUndelegate(sdk.AccAddress(delegator[:]), sdk.ValAddress(op[:]), sdk.NewCoin(bondDenom, amount))
	if err := c.deliver(msg, func(ctx context.Context) error {
		_, err := c.msgs.Undelegate(ctx, msg)
		return err
	}); err != nil {
		return err
	}
	c.unjail(op)
	return nil
}

func (c *v2Chain) redelegate(delegator, src, dst common.Address, amount sdk.Int) error {
	msg := stakingtypes.NewMsgBeginRedelegate(sdk.AccAddress(delegator[:]), sdk.ValAddress(src[:]), sdk.ValAddress(dst[:]), sdk.NewCoin(bondDenom, amount))
	if err := c.deliver(msg, func(ctx context.Context) error {
		_, err := c.msgs.BeginRedelegate(ctx, msg)
		return err
	}); err != nil {
		return err
	}
	c.unjail(src)
	return nil
}

// unjail undoes x/staking's jailing of op's validator for a self-delegation
// below its minimum, which the precompile never does. x/staking's jail and
// unjail only take the validator out of its power index and put it back.
func (c *v2Chain) unjail(op common.Address) {
	v, ok := c.validator(op)
	if !ok || !v.Jailed {
		return
	}
	consAddr, err := v.GetConsAddr()
	require.NoError(c.t, err)
	c.keeper.Unjail(c.ctx, consAddr)
}

// delegatedTokens returns the usei delegator's delegation to op is worth, or
// zero when it has none.
func (c *v2Chain) delegatedTokens(delegator, op common.Address) sdk.Int {
	d, found := c.keeper.GetDelegation(c.ctx, sdk.AccAddress(delegator[:]), sdk.ValAddress(op[:]))
	v, ok := c.keeper.GetValidator(c.ctx, sdk.ValAddress(op[:]))
	if !found || !ok {
		return sdk.ZeroInt()
	}
	return v.TokensFromShares(d.Shares).TruncateInt()
}

func (c *v2Chain) validator(op common.Address) (stakingtypes.Validator, bool) {
	return c.keeper.GetValidator(c.ctx, sdk.ValAddress(op[:]))
}

// stakingState is what staking holds that x/staking's rules decide, in the
// precompile's units. Unbonding and redelegation entries are keyed by the
// accounts and validators they name.
type stakingState struct {
	validators         map[common.Address]validatorState
	delegations        map[delegationKey]string
	unbonding          map[delegationKey][]unbondingEntryState
	redelegations      map[redelegationKey][]redelegationEntryState
	redelegationCounts map[redelegationKey]uint64
	// receiving counts the redelegation entries into each validator, keyed by
	// that validator.
	receiving      map[delegationKey]uint64
	lastValidators map[common.Address]uint64
	lastTotalPower string
	powerIndex     []common.Address
	balances       map[common.Address]string
}

type delegationKey struct{ delegator, validator common.Address }

type redelegationKey struct{ delegator, src, dst common.Address }

type validatorState struct {
	pubkey            common.Hash
	jailed            bool
	status            uint8
	tokens            string
	shares            string
	minSelfDelegation string
	unbondingHeight   uint64
	unbondingTime     uint64
}

type unbondingEntryState struct {
	creationHeight, completionTime uint64
	initialBalance, balance        string
}

type redelegationEntryState struct {
	creationHeight, completionTime uint64
	initialBalance, sharesDst      string
}

func newStakingState() stakingState {
	return stakingState{
		validators:         map[common.Address]validatorState{},
		delegations:        map[delegationKey]string{},
		unbonding:          map[delegationKey][]unbondingEntryState{},
		redelegations:      map[redelegationKey][]redelegationEntryState{},
		redelegationCounts: map[redelegationKey]uint64{},
		receiving:          map[delegationKey]uint64{},
		lastValidators:     map[common.Address]uint64{},
		powerIndex:         []common.Address{},
		balances:           map[common.Address]string{},
	}
}

// state returns x/staking's state for accounts, in the precompile's units.
func (c *v2Chain) state(accounts []common.Address) stakingState {
	st := newStakingState()
	for _, v := range c.keeper.GetAllValidators(c.ctx) {
		pk, err := v.ConsPubKey()
		require.NoError(c.t, err)
		st.validators[common.BytesToAddress(v.GetOperator())] = validatorState{
			pubkey:            common.BytesToHash(pk.Bytes()),
			jailed:            v.Jailed,
			status:            uint8(v.Status), //nolint:gosec // a BondStatus
			tokens:            toWei(v.Tokens).String(),
			shares:            sharesToWei(v.DelegatorShares).String(),
			minSelfDelegation: toWei(v.MinSelfDelegation).String(),
			unbondingHeight:   uint64(v.UnbondingHeight),          //nolint:gosec // a block height
			unbondingTime:     uint64(v.UnbondingTime.UnixNano()), //nolint:gosec // a block time
		}
	}
	for _, d := range c.keeper.GetAllDelegations(c.ctx) {
		key := delegationKey{common.BytesToAddress(d.GetDelegatorAddr()), common.BytesToAddress(d.GetValidatorAddr())}
		st.delegations[key] = sharesToWei(d.Shares).String()
	}
	for _, a := range accounts {
		for _, ubd := range c.keeper.GetAllUnbondingDelegations(c.ctx, sdk.AccAddress(a[:])) {
			op, err := sdk.ValAddressFromBech32(ubd.ValidatorAddress)
			require.NoError(c.t, err)
			key := delegationKey{a, common.BytesToAddress(op)}
			for _, e := range ubd.Entries {
				st.unbonding[key] = append(st.unbonding[key], unbondingEntryState{
					creationHeight: uint64(e.CreationHeight),            //nolint:gosec // a block height
					completionTime: uint64(e.CompletionTime.UnixNano()), //nolint:gosec // a block time
					initialBalance: toWei(e.InitialBalance).String(),
					balance:        toWei(e.Balance).String(),
				})
			}
		}
		for _, red := range c.keeper.GetAllRedelegations(c.ctx, sdk.AccAddress(a[:]), nil, nil) {
			src, err := sdk.ValAddressFromBech32(red.ValidatorSrcAddress)
			require.NoError(c.t, err)
			dst, err := sdk.ValAddressFromBech32(red.ValidatorDstAddress)
			require.NoError(c.t, err)
			key := redelegationKey{a, common.BytesToAddress(src), common.BytesToAddress(dst)}
			for _, e := range red.Entries {
				st.redelegations[key] = append(st.redelegations[key], redelegationEntryState{
					creationHeight: uint64(e.CreationHeight),            //nolint:gosec // a block height
					completionTime: uint64(e.CompletionTime.UnixNano()), //nolint:gosec // a block time
					initialBalance: toWei(e.InitialBalance).String(),
					sharesDst:      sharesToWei(e.SharesDst).String(),
				})
			}
			st.redelegationCounts[key] = uint64(len(red.Entries))
			st.receiving[delegationKey{a, key.dst}] += uint64(len(red.Entries))
		}
		st.balances[a] = toWei(c.bank.GetBalance(c.ctx, sdk.AccAddress(a[:]), bondDenom).Amount).String()
	}
	for _, v := range c.keeper.GetLastValidators(c.ctx) {
		st.lastValidators[common.BytesToAddress(v.GetOperator())] = uint64(c.keeper.GetLastValidatorPower(c.ctx, v.GetOperator())) //nolint:gosec // a consensus power
	}
	st.lastTotalPower = c.keeper.GetLastTotalPower(c.ctx).String()
	it := c.keeper.ValidatorsPowerStoreIterator(c.ctx)
	for ; it.Valid(); it.Next() {
		st.powerIndex = append(st.powerIndex, common.BytesToAddress(it.Value()))
	}
	require.NoError(c.t, it.Close())
	for escrow, pool := range map[common.Address]string{BondedEscrow: stakingtypes.BondedPoolName, NotBondedEscrow: stakingtypes.NotBondedPoolName} {
		st.balances[escrow] = toWei(c.bank.GetBalance(c.ctx, c.accounts.GetModuleAddress(pool), bondDenom).Amount).String()
	}
	return st
}

// parityState returns the precompile's state for accounts and validators, in
// the form v2Chain.state returns x/staking's.
func (h *harness) parityState(validators, accounts []common.Address) stakingState {
	s, st := h.store(), newStakingState()
	set := s.validatorSet()
	for i := uint64(0); i < set.Len(); i++ {
		op := solstorage.MemberAddress(set.At(i))
		v := h.validator(op)
		st.validators[op] = validatorState{
			pubkey:            v.consensusPubkey,
			jailed:            v.jailed,
			status:            v.status,
			tokens:            v.tokens.String(),
			shares:            v.delegatorShares.String(),
			minSelfDelegation: v.minSelfDelegation.String(),
			unbondingHeight:   v.unbondingHeight,
			unbondingTime:     v.unbondingTime,
		}
	}
	for _, a := range accounts {
		delegated := s.delegatedValidators(a)
		for i := uint64(0); i < delegated.Len(); i++ {
			op := solstorage.MemberAddress(delegated.At(i))
			shares, _ := s.delegation(a, op)
			st.delegations[delegationKey{a, op}] = shares.String()
		}
		for _, op := range validators {
			for _, e := range must(s.unbondingEntries(a, op)) {
				key := delegationKey{a, op}
				st.unbonding[key] = append(st.unbonding[key], unbondingEntryState{
					creationHeight: e.creationHeight,
					completionTime: e.completionTime,
					initialBalance: e.initialBalance.String(),
					balance:        e.balance.String(),
				})
			}
			if n := s.receivingRedelegations(op, a); n.Sign() != 0 {
				st.receiving[delegationKey{a, op}] = n.Uint64()
			}
			for _, dst := range validators {
				if n := s.redelegationEntryCount(a, op, dst); n.Sign() != 0 {
					st.redelegationCounts[redelegationKey{a, op, dst}] = n.Uint64()
				}
			}
		}
		st.balances[a] = h.state.GetBalance(a).String()
	}
	q := s.redelegationQueue()
	for i := q.head(); i < q.tail(); i++ {
		e := q.entry(i)
		key := redelegationKey{
			s.AddressAt(at(e, redelegationQueueDelegator), redelegationQueueDelegator.offset),
			s.AddressAt(at(e, redelegationQueueSrcValidator), redelegationQueueSrcValidator.offset),
			s.AddressAt(at(e, redelegationQueueDstValidator), redelegationQueueDstValidator.offset),
		}
		st.redelegations[key] = append(st.redelegations[key], redelegationEntryState{
			creationHeight: s.Uint64(at(e, redelegationQueueCreationHeight), redelegationQueueCreationHeight.offset),
			completionTime: s.Uint64(at(e, queueCompletionTime), queueCompletionTime.offset),
			initialBalance: s.Uint256(at(e, redelegationQueueInitialBalance)).String(),
			sharesDst:      s.Uint256(at(e, redelegationQueueSharesDst)).String(),
		})
	}
	for _, v := range h.lastValidators() {
		st.lastValidators[v.operator] = v.power
	}
	st.lastTotalPower = s.lastTotalPower().String()
	st.powerIndex = h.powerIndex()
	st.balances[BondedEscrow] = h.state.GetBalance(BondedEscrow).String()
	st.balances[NotBondedEscrow] = h.state.GetBalance(NotBondedEscrow).String()
	return st
}

// hookLog records each hook call as its name and the addresses it names.
type hookLog []string

func (l *hookLog) add(name string, addrs ...common.Address) {
	call := name
	for _, a := range addrs {
		call += " " + a.Hex()
	}
	*l = append(*l, call)
}

// loggingHooks are Hooks that record each call.
type loggingHooks struct{ log *hookLog }

var _ Hooks = loggingHooks{}

func (h loggingHooks) AfterValidatorCreated(_ precompiles.BlockContext, _ precompiles.State, v common.Address) error {
	h.log.add("AfterValidatorCreated", v)
	return nil
}

func (h loggingHooks) BeforeDelegationCreated(_ precompiles.BlockContext, _ precompiles.State, d, v common.Address) error {
	h.log.add("BeforeDelegationCreated", d, v)
	return nil
}

func (h loggingHooks) BeforeDelegationSharesModified(_ precompiles.BlockContext, _ precompiles.State, d, v common.Address) error {
	h.log.add("BeforeDelegationSharesModified", d, v)
	return nil
}

func (h loggingHooks) BeforeDelegationRemoved(_ precompiles.BlockContext, _ precompiles.State, d, v common.Address) error {
	h.log.add("BeforeDelegationRemoved", d, v)
	return nil
}

func (h loggingHooks) AfterDelegationModified(_ precompiles.BlockContext, _ precompiles.State, d, v common.Address) error {
	h.log.add("AfterDelegationModified", d, v)
	return nil
}

func (h loggingHooks) AfterValidatorBonded(_ precompiles.BlockContext, _ precompiles.State, v common.Address) error {
	h.log.add("AfterValidatorBonded", v)
	return nil
}

func (h loggingHooks) AfterValidatorBeginUnbonding(_ precompiles.BlockContext, _ precompiles.State, v common.Address) error {
	h.log.add("AfterValidatorBeginUnbonding", v)
	return nil
}

func (h loggingHooks) AfterValidatorRemoved(_ precompiles.BlockContext, _ precompiles.State, v common.Address) error {
	h.log.add("AfterValidatorRemoved", v)
	return nil
}

// v2LoggingHooks are x/staking's StakingHooks that record each call.
type v2LoggingHooks struct{ log *hookLog }

var _ stakingtypes.StakingHooks = v2LoggingHooks{}

func (h v2LoggingHooks) AfterValidatorCreated(_ sdk.Context, v sdk.ValAddress) {
	h.log.add("AfterValidatorCreated", common.BytesToAddress(v))
}

func (h v2LoggingHooks) BeforeValidatorModified(_ sdk.Context, v sdk.ValAddress) {
	h.log.add("BeforeValidatorModified", common.BytesToAddress(v))
}

func (h v2LoggingHooks) AfterValidatorRemoved(_ sdk.Context, _ sdk.ConsAddress, v sdk.ValAddress) {
	h.log.add("AfterValidatorRemoved", common.BytesToAddress(v))
}

func (h v2LoggingHooks) AfterValidatorBonded(_ sdk.Context, _ sdk.ConsAddress, v sdk.ValAddress) {
	h.log.add("AfterValidatorBonded", common.BytesToAddress(v))
}

func (h v2LoggingHooks) AfterValidatorBeginUnbonding(_ sdk.Context, _ sdk.ConsAddress, v sdk.ValAddress) {
	h.log.add("AfterValidatorBeginUnbonding", common.BytesToAddress(v))
}

func (h v2LoggingHooks) BeforeDelegationCreated(_ sdk.Context, d sdk.AccAddress, v sdk.ValAddress) {
	h.log.add("BeforeDelegationCreated", common.BytesToAddress(d), common.BytesToAddress(v))
}

func (h v2LoggingHooks) BeforeDelegationSharesModified(_ sdk.Context, d sdk.AccAddress, v sdk.ValAddress) {
	h.log.add("BeforeDelegationSharesModified", common.BytesToAddress(d), common.BytesToAddress(v))
}

func (h v2LoggingHooks) BeforeDelegationRemoved(_ sdk.Context, d sdk.AccAddress, v sdk.ValAddress) {
	h.log.add("BeforeDelegationRemoved", common.BytesToAddress(d), common.BytesToAddress(v))
}

func (h v2LoggingHooks) AfterDelegationModified(_ sdk.Context, d sdk.AccAddress, v sdk.ValAddress) {
	h.log.add("AfterDelegationModified", common.BytesToAddress(d), common.BytesToAddress(v))
}

func (h v2LoggingHooks) BeforeValidatorSlashed(_ sdk.Context, v sdk.ValAddress, _ sdk.Dec) {
	h.log.add("BeforeValidatorSlashed", common.BytesToAddress(v))
}

// v2Errors pairs each x/staking error a delegation can fail with to the
// precompile's error for the same rule. msg narrows a V2 error that several
// rules share to the one whose message contains it.
var v2Errors = []struct {
	v2         error
	msg        string
	precompile error
}{
	{v2: stakingtypes.ErrNoValidatorFound, precompile: errNoValidatorFound},
	{v2: stakingtypes.ErrNoDelegation, precompile: errNoDelegation},
	{v2: stakingtypes.ErrNotEnoughDelegationShares, precompile: errNotEnoughShares},
	{v2: stakingtypes.ErrDelegatorShareExRateInvalid, precompile: errExRateInvalid},
	{v2: stakingtypes.ErrExceedMaxVotingPowerRatio, precompile: errExceedMaxVotingPower},
	{v2: stakingtypes.ErrMaxUnbondingDelegationEntries, precompile: errMaxUnbondingEntries},
	{v2: stakingtypes.ErrMaxRedelegationEntries, precompile: errMaxRedelegationEntries},
	{v2: stakingtypes.ErrSelfRedelegation, precompile: errSelfRedelegation},
	{v2: stakingtypes.ErrBadRedelegationDst, precompile: errBadRedelegationValidator},
	{v2: stakingtypes.ErrTransitiveRedelegation, precompile: errTransitiveRedelegation},
	{v2: stakingtypes.ErrTinyRedelegationAmount, precompile: errTinyRedelegationAmount},
	// ValidateUnbondAmount's, for more than the delegation holds. Every
	// ValidateBasic refusal is an ErrInvalidRequest too.
	{v2: sdkerrors.ErrInvalidRequest, msg: "invalid shares amount", precompile: errInvalidShares},
}

// pair runs every operation on the precompile and on x/staking, in wei on one
// and usei on the other, and checks that both end it the same way, call the
// same hooks, and leave the same state.
type pair struct {
	t               *testing.T
	p               *harness
	v2              *v2Chain
	pHooks, v2Hooks hookLog
	validators      []common.Address // every validator created, removed ones included
	accounts        []common.Address // every account that may delegate, operators included
	outcomes        map[string]int   // how often each operation kind ended each way, and each hook was called
}

// parityValidator is a validator a pair starts with, its amounts in usei.
type parityValidator struct {
	op            common.Address
	self, minSelf int64
}

// newPair starts both sides with params, validators and the accounts that
// will delegate, then runs the first EndBlock.
func newPair(t *testing.T, params chainparams.Staking, validators []parityValidator, accounts ...common.Address) *pair {
	pr := &pair{t: t, outcomes: map[string]int{}}
	pr.p = &harness{t: t, state: newMemState(), time: startTime, staking: Precompile{Hooks: loggingHooks{&pr.pHooks}}}
	require.NoError(t, SetParams(pr.p.state, params))
	pr.v2 = newV2Chain(t, v2Params(params), v2LoggingHooks{&pr.v2Hooks})
	pr.v2.setBlock(pr.p.number, pr.p.time)
	pr.accounts = append(pr.accounts, accounts...)
	for _, v := range validators {
		pr.createValidator(v)
	}
	pr.endBlock()
	return pr
}

// apply runs an operation of kind on both sides, checks it, and returns V2's
// error.
func (pr *pair) apply(kind, detail string, onPrecompile, onV2 func() error) error {
	t := pr.t
	t.Helper()
	step := fmt.Sprintf("%s at block %d", strings.TrimSpace(kind+" "+detail), pr.p.number)
	pHooks, v2Hooks := len(pr.pHooks), len(pr.v2Hooks)
	v2Err := onV2()
	pErr := onPrecompile()
	outcome := pr.requireSameOutcome(step, v2Err, pErr)
	pr.outcomes[kind+": "+outcome]++
	if v2Err != nil {
		pr.pHooks, pr.v2Hooks = pr.pHooks[:pHooks], pr.v2Hooks[:v2Hooks]
	}
	require.Equal(t, []string(pr.v2Hooks[v2Hooks:]), []string(pr.pHooks[pHooks:]), "%s calls the same hooks", step)
	for _, call := range pr.pHooks[pHooks:] {
		name, _, _ := strings.Cut(call, " ")
		pr.outcomes["hook: "+name]++
	}
	require.Equal(t, pr.v2.state(pr.accounts), pr.p.parityState(pr.validators, pr.accounts), "%s leaves the same state", step)
	require.Zero(t, pr.p.state.GetBalance(Address).Sign(), "the precompile keeps nothing after %s", step)
	return v2Err
}

// requireSameOutcome checks that the precompile fails exactly when V2 does,
// by the same rule, and returns the outcome: "ok" or V2's error.
func (pr *pair) requireSameOutcome(step string, v2Err, pErr error) string {
	t := pr.t
	t.Helper()
	if v2Err == nil {
		require.NoError(t, pErr, "%s succeeds on V2", step)
		return "ok"
	}
	require.Error(t, pErr, "%s fails on V2 with %v", step, v2Err)
	for _, e := range v2Errors {
		if errors.Is(v2Err, e.v2) && strings.Contains(v2Err.Error(), e.msg) {
			require.ErrorIs(t, pErr, e.precompile, "%s fails on V2 with %v", step, v2Err)
			return e.v2.Error()
		}
	}
	require.Failf(t, "unmapped V2 error", "%s fails on V2 with %v", step, v2Err)
	return ""
}

func (pr *pair) createValidator(v parityValidator) {
	pubkey := common.BytesToHash(crypto.Keccak256(v.op[:]))
	self, minSelf := sdk.NewInt(v.self), sdk.NewInt(v.minSelf)
	pr.v2.fund(v.op, self)
	pr.p.fund(v.op, toWei(self))
	pr.validators = append(pr.validators, v.op)
	pr.accounts = append(pr.accounts, v.op)
	require.NoError(pr.t, pr.apply("create validator", v.op.Hex(),
		func() error {
			return pr.p.staking.AddGenesisValidator(pr.p.block(), pr.p.state, GenesisValidator{
				Operator:          v.op,
				ConsensusPubkey:   pubkey,
				SelfDelegation:    toWei(self),
				MinSelfDelegation: toWei(minSelf),
			})
		},
		func() error { return pr.v2.createValidator(v.op, pubkey, self, minSelf) }))
}

// delegate funds delegator with usei, then delegates it to op.
func (pr *pair) delegate(delegator, op common.Address, usei int64) error {
	amount := sdk.NewInt(usei)
	pr.v2.fund(delegator, amount)
	pr.p.fund(delegator, toWei(amount))
	return pr.apply("delegate", fmt.Sprintf("%d usei from %s to %s", usei, delegator, op),
		func() error { return pr.p.call(delegator, callOpts{value: toWei(amount)}, DelegateMethod, op) },
		func() error { return pr.v2.delegate(delegator, op, amount) })
}

func (pr *pair) undelegate(delegator, op common.Address, usei int64) error {
	amount := sdk.NewInt(usei)
	return pr.apply("undelegate", fmt.Sprintf("%d usei of %s from %s", usei, delegator, op),
		func() error { return pr.p.undelegate(delegator, op, toWei(amount)) },
		func() error { return pr.v2.undelegate(delegator, op, amount) })
}

func (pr *pair) redelegate(delegator, src, dst common.Address, usei int64) error {
	amount := sdk.NewInt(usei)
	return pr.apply("redelegate", fmt.Sprintf("%d usei of %s from %s to %s", usei, delegator, src, dst),
		func() error { return pr.p.redelegate(delegator, src, dst, toWei(amount)) },
		func() error { return pr.v2.redelegate(delegator, src, dst, amount) })
}

// endBlock runs EndBlock on both sides, then starts the next block one second
// later.
func (pr *pair) endBlock() {
	_ = pr.apply("EndBlock", "", func() error {
		pr.p.endBlock()
		return errors.Join(pr.p.failures...)
	}, func() error {
		pr.v2.keeper.BlockValidatorUpdates(pr.v2.ctx)
		return nil
	})
	pr.v2.setBlock(pr.p.number, pr.p.time)
}

// wait moves the current block d later.
func (pr *pair) wait(d uint64) {
	pr.p.time += d
	pr.v2.setBlock(pr.p.number, pr.p.time)
}

// clampsRedelegation reports whether a redelegation out of src would queue
// behind a later entry and complete after V2's does, the precompile's
// first-in first-out queues' deviation. Only a redelegation out of an
// unbonding validator can, while the unbonding time stays the same.
func (pr *pair) clampsRedelegation(src common.Address) bool {
	v, ok := pr.v2.validator(src)
	if !ok || !v.IsUnbonding() {
		return false
	}
	t := uint64(v.UnbondingTime.UnixNano()) //nolint:gosec // a block time
	return pr.p.store().redelegationQueue().clamp(t) != t
}
