// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "./Sets.sol";

/// @notice Storage schema of the evmonly staking precompile (0x1005), held as that
/// address's contract storage. Each state variable is an x/staking store prefix,
/// named in its comment.
/// @dev Params are `ParamsStorage.staking`. Bonded tokens sit in the balance of
/// `BondedEscrow` and unbonded and unbonding tokens in `NotBondedEscrow`, derived
/// addresses in `staking/layout.go`. Tokens and balances are wei; shares and
/// rates are 1e18-scaled; times are Unix nanoseconds. Accounts and operators are EVM
/// addresses; a consensus address is sha256(consensusPubkey)[:20].
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
        uint256[8] __gap;
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

    struct RedelegationEntry {
        uint64 creationHeight;
        uint64 completionTime;
        uint256 initialBalance;
        uint256 sharesDst;
    }

    /// @dev A queue entry's completionTime is never earlier than the one before it.
    struct UnbondingQueueEntry {
        uint64 completionTime;
        address delegator;
        address validator;
    }

    struct RedelegationQueueEntry {
        uint64 completionTime;
        address delegator;
        address srcValidator;
        address dstValidator;
    }

    struct ValidatorQueueEntry {
        uint64 completionTime;
        uint64 completionHeight;
        address operator;
    }

    /// @dev A first-in first-out queue: entries are at indexes [head, tail).
    struct QueueBounds {
        uint64 head;
        uint64 tail;
    }

    mapping(address => uint64) internal lastValidatorPower; // 0x11: operator => power
    AddressSet internal lastValidatorPowerSet; // operators with a lastValidatorPower
    uint256 internal lastTotalPower; // 0x12

    mapping(address => Validator) internal validators; // 0x21: operator =>
    AddressSet internal validatorSet; // every operator in validators
    mapping(address => address) internal validatorsByConsAddr; // 0x22: consensus address => operator
    address[] internal validatorsByPower; // 0x23: unjailed operators, descending by power, ties to the lower address

    mapping(address => mapping(address => Delegation)) internal delegations; // 0x31: delegator => validator =>
    mapping(address => AddressSet) internal delegationsByValidator; // 0x37: validator => delegators
    mapping(address => AddressSet) internal delegationsByDelegator; // delegator => validators

    mapping(address => mapping(address => UnbondingDelegationEntry[])) internal unbondingDelegations; // 0x32: delegator => validator => entries, at most maxEntries
    mapping(address => AddressSet) internal unbondingDelegationsByValidator; // 0x33: validator => delegators
    mapping(address => AddressSet) internal unbondingDelegationsByDelegator; // delegator => validators

    mapping(address => mapping(address => mapping(address => RedelegationEntry[]))) internal redelegations; // 0x34: delegator => src => dst => entries, at most maxEntries
    mapping(address => mapping(address => AddressSet)) internal redelegationsByValSrc; // 0x35: src => delegator => dsts
    mapping(address => AddressSet) internal redelegationDelegatorsByValSrc; // src => delegators
    mapping(address => mapping(address => uint256)) internal redelegationsByValDst; // 0x36: dst => delegator => open entries into dst
    mapping(address => mapping(address => AddressSet)) internal redelegationsByDelegator; // delegator => src => dsts
    mapping(address => AddressSet) internal redelegationSrcsByDelegator; // delegator => srcs

    QueueBounds internal unbondingQueueBounds;
    mapping(uint64 => UnbondingQueueEntry) internal unbondingQueue; // 0x41: index =>
    QueueBounds internal redelegationQueueBounds;
    mapping(uint64 => RedelegationQueueEntry) internal redelegationQueue; // 0x42: index =>
    QueueBounds internal validatorQueueBounds;
    mapping(uint64 => ValidatorQueueEntry) internal validatorQueue; // 0x43: index =>

    /// @dev V2 to giga migration. The phase runs from the cutover height until
    /// migrationCursor passes the last V2 key.
    bool internal migrating;
    bytes internal migrationCursor; // last V2 store key copied
    bytes[] internal migrationFailures; // V2 store keys that could not be copied
}
