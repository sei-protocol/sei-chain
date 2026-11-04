// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

import "giga-storage/StakingStorage.sol";

/// @notice Writes known values into StakingStorage with solc's own codegen, so
/// the Go reader can be checked against the compiler's slot derivation.
contract StakingStorageHarness is StakingStorage {
    function seed(address op, address other, address delegator, bytes32 pubkey) external {
        lastValidators.push(ValidatorPower({operator: op, power: 30}));
        lastValidators.push(ValidatorPower({operator: other, power: 20}));
        lastTotalPower = 50;

        Validator storage v = validators[op];
        v.consensusPubkey = pubkey;
        v.jailed = true;
        v.status = 2;
        v.tokens = 31e18;
        v.delegatorShares = 32e36;
        v.description.moniker = "a moniker long enough to spill past one storage word";
        v.description.details = "d";
        v.unbondingHeight = 33;
        v.unbondingTime = 34;
        v.commission.commissionRates.rate = 35;
        v.commission.updateTime = 36;
        v.minSelfDelegation = 37;
        validators[other].status = 3;
        add(validatorSet, op);
        add(validatorSet, other);
        validatorsByConsensusPubkey[pubkey] = op;
        validatorsByPower.push(other);
        validatorsByPower.push(op);

        delegations[delegator][op].shares = 41;
        add(delegationsByDelegator[delegator], op);

        unbondingDelegations[delegator][op].push(
            UnbondingDelegationEntry({creationHeight: 51, completionTime: 52, initialBalance: 53, balance: 54})
        );
        unbondingDelegations[delegator][op].push(
            UnbondingDelegationEntry({creationHeight: 55, completionTime: 56, initialBalance: 57, balance: 58})
        );

        redelegationEntryCounts[delegator][op][other] = 2;
        redelegationsByValDst[other][delegator] = 3;

        unbondingQueueBounds = QueueBounds({head: 1, tail: 2});
        unbondingQueue[1] = UnbondingQueueEntry({completionTime: 61, delegator: delegator, validator: op});
        redelegationQueueBounds = QueueBounds({head: 0, tail: 1});
        redelegationQueue[0] = RedelegationQueueEntry({
            completionTime: 71,
            delegator: delegator,
            creationHeight: 72,
            srcValidator: op,
            dstValidator: other,
            initialBalance: 73,
            sharesDst: 74
        });
        validatorQueueBounds = QueueBounds({head: 4, tail: 5});
        validatorQueue[4] = ValidatorQueueEntry({completionTime: 81, unbondingHeight: 82, operator: other});
    }

    function add(AddressSet storage set, address a) internal {
        set.values.push(a);
        set.positions[a] = set.values.length;
    }
}
