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
        epochValidators[7].push(EpochValidator({operator: other, power: 21, consensusPubkey: pubkey}));
        nextSnapshotEpoch = 8;

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
        add(delegationsByValidator[op], delegator);

        unbondingDelegations[delegator][op].push(
            UnbondingDelegationEntry({creationHeight: 51, completionTime: 52, initialBalance: 53, balance: 54})
        );
        unbondingDelegations[delegator][op].push(
            UnbondingDelegationEntry({creationHeight: 55, completionTime: 56, initialBalance: 57, balance: 58})
        );

        unbondingQueueBounds = QueueBounds({head: 1, tail: 2});
        unbondingQueue[1] = UnbondingQueueEntry({completionTime: 61, delegator: delegator, validator: op});
        validatorQueueBounds = QueueBounds({head: 4, tail: 5});
        validatorQueue[4] = ValidatorQueueEntry({completionTime: 81, unbondingHeight: 82, operator: other});

        currentEpoch = 9;
        stagedQueueBounds = QueueBounds({head: 2, tail: 3});
        stagedQueue[2] = StagedOperation({epoch: 10, kind: 2, delegator: delegator, validator: op, amount: 91});
        stagedDelegations[delegator][op] = StagedDelegation({sharesOut: 92, undelegations: 93});
        stagedInbound[other] = 96;
    }

    function add(AddressSet storage set, address a) internal {
        set.values.push(a);
        set.positions[a] = set.values.length;
    }
}
