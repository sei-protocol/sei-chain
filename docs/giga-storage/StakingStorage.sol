// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "./Sets.sol";

/// @notice Storage schema of the evmonly staking precompile (0x1005), held as that
/// address's contract storage. Each state variable is an x/staking store prefix,
/// named in its comment.
/// @dev Staking applies `params`, its copy of `ParamsStorage.staking`. Bonded
/// tokens sit in the balance of `BondedEscrow`, unbonded and unbonding tokens in
/// `NotBondedEscrow`, and staged delegations' tokens in `StagedEscrow`, derived
/// addresses in `staking/layout.go`. Tokens and balances are wei; shares and
/// rates are 1e18-scaled; times are Unix nanoseconds. Accounts are EVM addresses,
/// and a validator is keyed by its operator's. An enumeration is kept only where an
/// on-chain path walks it.
contract StakingStorage {
    struct Description {
        string moniker;
        string identity;
        string website;
        string securityContact;
        string details;
    }

    struct CommissionRates {
        uint256 rate;
        uint256 maxRate;
        uint256 maxChangeRate;
    }

    struct Commission {
        CommissionRates commissionRates;
        uint64 updateTime;
    }

    struct Validator {
        bytes32 consensusPubkey; // ed25519
        bool jailed;
        uint8 status; // 1=Unbonded, 2=Unbonding, 3=Bonded
        uint256 tokens;
        uint256 delegatorShares;
        Description description;
        uint64 unbondingHeight;
        uint64 unbondingTime;
        Commission commission;
        uint256 minSelfDelegation;
    }

    /// @dev A bonded validator and the consensus power last sent for it.
    struct ValidatorPower {
        address operator;
        uint64 power;
    }

    /// @dev A member of an epoch snapshot.
    struct EpochValidator {
        address operator;
        uint64 power;
        bytes32 consensusPubkey; // ed25519
    }

    struct Delegation {
        uint256 shares;
    }

    struct UnbondingDelegationEntry {
        uint64 creationHeight;
        uint64 completionTime;
        uint256 initialBalance;
        uint256 balance;
    }

    /// @dev A queue entry's completionTime is never earlier than the one before it.
    struct UnbondingQueueEntry {
        uint64 completionTime;
        address delegator;
        address validator;
    }

    /// @dev A redelegation entry's only record: V2's queue triplet and its
    /// RedelegationEntry together.
    struct RedelegationQueueEntry {
        uint64 completionTime;
        address delegator;
        uint64 creationHeight;
        address srcValidator;
        address dstValidator;
        uint256 initialBalance;
        uint256 sharesDst;
    }

    /// @dev completionTime and unbondingHeight are the operator's validator's
    /// unbondingTime and unbondingHeight when it began unbonding.
    struct ValidatorQueueEntry {
        uint64 completionTime;
        uint64 unbondingHeight;
        address operator;
    }

    /// @dev A first-in first-out queue: entries are at indexes [head, tail).
    struct QueueBounds {
        uint64 head;
        uint64 tail;
    }

    /// @dev A delegate, undelegate or redelegate staged in one epoch, which takes
    /// effect at the end of the next. An entry's epoch is never earlier than the
    /// one before it.
    struct StagedOperation {
        uint64 epoch; // the epoch it takes effect at the end of
        uint8 kind; // 1=Delegate, 2=Undelegate, 3=Redelegate
        address delegator;
        address validator; // delegated to, or undelegated or redelegated from
        address dstValidator; // redelegated to
        uint256 amount; // tokens delegated, or shares undelegated or redelegated
    }

    /// @dev What a delegation's staged operations will take or add once they take
    /// effect.
    struct StagedDelegation {
        uint256 sharesOut; // undelegated or redelegated away
        uint64 undelegations;
        uint64 redelegationsOut;
        uint64 redelegationsIn; // redelegated to this delegation's validator
    }

    /// @dev ParamsStorage.Staking's members.
    struct Params {
        uint64 unbondingTime;
        uint32 maxValidators;
        uint32 maxEntries;
        uint256 maxVotingPowerRatio;
        uint256 maxVotingPowerEnforcementThreshold;
    }

    ValidatorPower[] internal lastValidators; // 0x11: the bonded set, as last sent to consensus
    uint256 internal lastTotalPower; // 0x12
    /// @dev The bonded set at the end of each epoch's last block, with each
    /// member's consensus key. Consensus derives an epoch's committee from the
    /// set two epochs before it, so the latest three are kept.
    mapping(uint64 => EpochValidator[]) internal epochValidators; // epoch =>
    uint64 internal nextSnapshotEpoch; // the epoch after the latest in epochValidators, 0 before any

    mapping(address => Validator) internal validators; // 0x21: operator =>
    AddressSet internal validatorSet; // every operator in validators
    mapping(bytes32 => address) internal validatorsByConsensusPubkey; // 0x22, keyed by the key itself: consensusPubkey => operator
    address[] internal validatorsByPower; // 0x23: unjailed operators, descending by power, ties to the lower address

    mapping(address => mapping(address => Delegation)) internal delegations; // 0x31: delegator => validator =>
    mapping(address => AddressSet) internal delegationsByDelegator; // delegator => validators
    mapping(address => AddressSet) internal delegationsByValidator; // 0x37: validator => delegators

    mapping(address => mapping(address => UnbondingDelegationEntry[])) internal unbondingDelegations; // 0x32: delegator => validator => entries, at most maxEntries

    mapping(address => mapping(address => mapping(address => uint256))) internal redelegationEntryCounts; // 0x34: delegator => src => dst => open entries, at most maxEntries
    mapping(address => mapping(address => uint256)) internal redelegationsByValDst; // 0x36: dst => delegator => open entries into dst

    QueueBounds internal unbondingQueueBounds;
    mapping(uint64 => UnbondingQueueEntry) internal unbondingQueue; // 0x41: index =>
    QueueBounds internal redelegationQueueBounds;
    mapping(uint64 => RedelegationQueueEntry) internal redelegationQueue; // 0x42: index =>
    QueueBounds internal validatorQueueBounds;
    mapping(uint64 => ValidatorQueueEntry) internal validatorQueue; // 0x43: index =>

    /// @dev Delegates, undelegates and redelegates wait here from the epoch they
    /// are called in until the end of the next.
    uint64 internal currentEpoch; // one after the last epoch a block closed, 0 before any
    QueueBounds internal stagedQueueBounds;
    mapping(uint64 => StagedOperation) internal stagedQueue; // index =>
    mapping(address => mapping(address => StagedDelegation)) internal stagedDelegations; // delegator => validator =>
    mapping(address => uint64) internal stagedInbound; // validator => staged delegates and redelegates to it
    uint64 internal stagedWork; // work staged during the current epoch: one unit per delegation an operation changes

    /// @dev ParamsStorage.staking as of the latest epoch's end, or as genesis
    /// wrote it before any.
    Params internal params;

    /// @dev V2 to giga migration. The phase runs from the cutover height until
    /// migrationCursor passes the last V2 key.
    bool internal migrating;
    bytes internal migrationCursor; // last V2 store key copied
    bytes[] internal migrationFailures; // V2 store keys that could not be copied
}
