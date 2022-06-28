/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { DecCoin, Coin } from "../../../cosmos/base/v1beta1/coin";
export const protobufPackage = "cosmos.distribution.v1beta1";
const baseParams = {
    communityTax: "",
    baseProposerReward: "",
    bonusProposerReward: "",
    withdrawAddrEnabled: false,
};
export const Params = {
    encode(message, writer = Writer.create()) {
        if (message.communityTax !== "") {
            writer.uint32(10).string(message.communityTax);
        }
        if (message.baseProposerReward !== "") {
            writer.uint32(18).string(message.baseProposerReward);
        }
        if (message.bonusProposerReward !== "") {
            writer.uint32(26).string(message.bonusProposerReward);
        }
        if (message.withdrawAddrEnabled === true) {
            writer.uint32(32).bool(message.withdrawAddrEnabled);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.communityTax = reader.string();
                    break;
                case 2:
                    message.baseProposerReward = reader.string();
                    break;
                case 3:
                    message.bonusProposerReward = reader.string();
                    break;
                case 4:
                    message.withdrawAddrEnabled = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseParams };
        if (object.communityTax !== undefined && object.communityTax !== null) {
            message.communityTax = String(object.communityTax);
        }
        else {
            message.communityTax = "";
        }
        if (object.baseProposerReward !== undefined &&
            object.baseProposerReward !== null) {
            message.baseProposerReward = String(object.baseProposerReward);
        }
        else {
            message.baseProposerReward = "";
        }
        if (object.bonusProposerReward !== undefined &&
            object.bonusProposerReward !== null) {
            message.bonusProposerReward = String(object.bonusProposerReward);
        }
        else {
            message.bonusProposerReward = "";
        }
        if (object.withdrawAddrEnabled !== undefined &&
            object.withdrawAddrEnabled !== null) {
            message.withdrawAddrEnabled = Boolean(object.withdrawAddrEnabled);
        }
        else {
            message.withdrawAddrEnabled = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.communityTax !== undefined &&
            (obj.communityTax = message.communityTax);
        message.baseProposerReward !== undefined &&
            (obj.baseProposerReward = message.baseProposerReward);
        message.bonusProposerReward !== undefined &&
            (obj.bonusProposerReward = message.bonusProposerReward);
        message.withdrawAddrEnabled !== undefined &&
            (obj.withdrawAddrEnabled = message.withdrawAddrEnabled);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseParams };
        if (object.communityTax !== undefined && object.communityTax !== null) {
            message.communityTax = object.communityTax;
        }
        else {
            message.communityTax = "";
        }
        if (object.baseProposerReward !== undefined &&
            object.baseProposerReward !== null) {
            message.baseProposerReward = object.baseProposerReward;
        }
        else {
            message.baseProposerReward = "";
        }
        if (object.bonusProposerReward !== undefined &&
            object.bonusProposerReward !== null) {
            message.bonusProposerReward = object.bonusProposerReward;
        }
        else {
            message.bonusProposerReward = "";
        }
        if (object.withdrawAddrEnabled !== undefined &&
            object.withdrawAddrEnabled !== null) {
            message.withdrawAddrEnabled = object.withdrawAddrEnabled;
        }
        else {
            message.withdrawAddrEnabled = false;
        }
        return message;
    },
};
const baseValidatorHistoricalRewards = { referenceCount: 0 };
export const ValidatorHistoricalRewards = {
    encode(message, writer = Writer.create()) {
        for (const v of message.cumulativeRewardRatio) {
            DecCoin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.referenceCount !== 0) {
            writer.uint32(16).uint32(message.referenceCount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseValidatorHistoricalRewards,
        };
        message.cumulativeRewardRatio = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.cumulativeRewardRatio.push(DecCoin.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.referenceCount = reader.uint32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseValidatorHistoricalRewards,
        };
        message.cumulativeRewardRatio = [];
        if (object.cumulativeRewardRatio !== undefined &&
            object.cumulativeRewardRatio !== null) {
            for (const e of object.cumulativeRewardRatio) {
                message.cumulativeRewardRatio.push(DecCoin.fromJSON(e));
            }
        }
        if (object.referenceCount !== undefined && object.referenceCount !== null) {
            message.referenceCount = Number(object.referenceCount);
        }
        else {
            message.referenceCount = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.cumulativeRewardRatio) {
            obj.cumulativeRewardRatio = message.cumulativeRewardRatio.map((e) => e ? DecCoin.toJSON(e) : undefined);
        }
        else {
            obj.cumulativeRewardRatio = [];
        }
        message.referenceCount !== undefined &&
            (obj.referenceCount = message.referenceCount);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseValidatorHistoricalRewards,
        };
        message.cumulativeRewardRatio = [];
        if (object.cumulativeRewardRatio !== undefined &&
            object.cumulativeRewardRatio !== null) {
            for (const e of object.cumulativeRewardRatio) {
                message.cumulativeRewardRatio.push(DecCoin.fromPartial(e));
            }
        }
        if (object.referenceCount !== undefined && object.referenceCount !== null) {
            message.referenceCount = object.referenceCount;
        }
        else {
            message.referenceCount = 0;
        }
        return message;
    },
};
const baseValidatorCurrentRewards = { period: 0 };
export const ValidatorCurrentRewards = {
    encode(message, writer = Writer.create()) {
        for (const v of message.rewards) {
            DecCoin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.period !== 0) {
            writer.uint32(16).uint64(message.period);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseValidatorCurrentRewards,
        };
        message.rewards = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.rewards.push(DecCoin.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.period = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseValidatorCurrentRewards,
        };
        message.rewards = [];
        if (object.rewards !== undefined && object.rewards !== null) {
            for (const e of object.rewards) {
                message.rewards.push(DecCoin.fromJSON(e));
            }
        }
        if (object.period !== undefined && object.period !== null) {
            message.period = Number(object.period);
        }
        else {
            message.period = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards) {
            obj.rewards = message.rewards.map((e) => e ? DecCoin.toJSON(e) : undefined);
        }
        else {
            obj.rewards = [];
        }
        message.period !== undefined && (obj.period = message.period);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseValidatorCurrentRewards,
        };
        message.rewards = [];
        if (object.rewards !== undefined && object.rewards !== null) {
            for (const e of object.rewards) {
                message.rewards.push(DecCoin.fromPartial(e));
            }
        }
        if (object.period !== undefined && object.period !== null) {
            message.period = object.period;
        }
        else {
            message.period = 0;
        }
        return message;
    },
};
const baseValidatorAccumulatedCommission = {};
export const ValidatorAccumulatedCommission = {
    encode(message, writer = Writer.create()) {
        for (const v of message.commission) {
            DecCoin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseValidatorAccumulatedCommission,
        };
        message.commission = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.commission.push(DecCoin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseValidatorAccumulatedCommission,
        };
        message.commission = [];
        if (object.commission !== undefined && object.commission !== null) {
            for (const e of object.commission) {
                message.commission.push(DecCoin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.commission) {
            obj.commission = message.commission.map((e) => e ? DecCoin.toJSON(e) : undefined);
        }
        else {
            obj.commission = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseValidatorAccumulatedCommission,
        };
        message.commission = [];
        if (object.commission !== undefined && object.commission !== null) {
            for (const e of object.commission) {
                message.commission.push(DecCoin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseValidatorOutstandingRewards = {};
export const ValidatorOutstandingRewards = {
    encode(message, writer = Writer.create()) {
        for (const v of message.rewards) {
            DecCoin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseValidatorOutstandingRewards,
        };
        message.rewards = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.rewards.push(DecCoin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseValidatorOutstandingRewards,
        };
        message.rewards = [];
        if (object.rewards !== undefined && object.rewards !== null) {
            for (const e of object.rewards) {
                message.rewards.push(DecCoin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards) {
            obj.rewards = message.rewards.map((e) => e ? DecCoin.toJSON(e) : undefined);
        }
        else {
            obj.rewards = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseValidatorOutstandingRewards,
        };
        message.rewards = [];
        if (object.rewards !== undefined && object.rewards !== null) {
            for (const e of object.rewards) {
                message.rewards.push(DecCoin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseValidatorSlashEvent = { validatorPeriod: 0, fraction: "" };
export const ValidatorSlashEvent = {
    encode(message, writer = Writer.create()) {
        if (message.validatorPeriod !== 0) {
            writer.uint32(8).uint64(message.validatorPeriod);
        }
        if (message.fraction !== "") {
            writer.uint32(18).string(message.fraction);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseValidatorSlashEvent };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorPeriod = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.fraction = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseValidatorSlashEvent };
        if (object.validatorPeriod !== undefined &&
            object.validatorPeriod !== null) {
            message.validatorPeriod = Number(object.validatorPeriod);
        }
        else {
            message.validatorPeriod = 0;
        }
        if (object.fraction !== undefined && object.fraction !== null) {
            message.fraction = String(object.fraction);
        }
        else {
            message.fraction = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorPeriod !== undefined &&
            (obj.validatorPeriod = message.validatorPeriod);
        message.fraction !== undefined && (obj.fraction = message.fraction);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseValidatorSlashEvent };
        if (object.validatorPeriod !== undefined &&
            object.validatorPeriod !== null) {
            message.validatorPeriod = object.validatorPeriod;
        }
        else {
            message.validatorPeriod = 0;
        }
        if (object.fraction !== undefined && object.fraction !== null) {
            message.fraction = object.fraction;
        }
        else {
            message.fraction = "";
        }
        return message;
    },
};
const baseValidatorSlashEvents = {};
export const ValidatorSlashEvents = {
    encode(message, writer = Writer.create()) {
        for (const v of message.validatorSlashEvents) {
            ValidatorSlashEvent.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseValidatorSlashEvents };
        message.validatorSlashEvents = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorSlashEvents.push(ValidatorSlashEvent.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseValidatorSlashEvents };
        message.validatorSlashEvents = [];
        if (object.validatorSlashEvents !== undefined &&
            object.validatorSlashEvents !== null) {
            for (const e of object.validatorSlashEvents) {
                message.validatorSlashEvents.push(ValidatorSlashEvent.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.validatorSlashEvents) {
            obj.validatorSlashEvents = message.validatorSlashEvents.map((e) => e ? ValidatorSlashEvent.toJSON(e) : undefined);
        }
        else {
            obj.validatorSlashEvents = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseValidatorSlashEvents };
        message.validatorSlashEvents = [];
        if (object.validatorSlashEvents !== undefined &&
            object.validatorSlashEvents !== null) {
            for (const e of object.validatorSlashEvents) {
                message.validatorSlashEvents.push(ValidatorSlashEvent.fromPartial(e));
            }
        }
        return message;
    },
};
const baseFeePool = {};
export const FeePool = {
    encode(message, writer = Writer.create()) {
        for (const v of message.communityPool) {
            DecCoin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFeePool };
        message.communityPool = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.communityPool.push(DecCoin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseFeePool };
        message.communityPool = [];
        if (object.communityPool !== undefined && object.communityPool !== null) {
            for (const e of object.communityPool) {
                message.communityPool.push(DecCoin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.communityPool) {
            obj.communityPool = message.communityPool.map((e) => e ? DecCoin.toJSON(e) : undefined);
        }
        else {
            obj.communityPool = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFeePool };
        message.communityPool = [];
        if (object.communityPool !== undefined && object.communityPool !== null) {
            for (const e of object.communityPool) {
                message.communityPool.push(DecCoin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseCommunityPoolSpendProposal = {
    title: "",
    description: "",
    recipient: "",
};
export const CommunityPoolSpendProposal = {
    encode(message, writer = Writer.create()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.recipient !== "") {
            writer.uint32(26).string(message.recipient);
        }
        for (const v of message.amount) {
            Coin.encode(v, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseCommunityPoolSpendProposal,
        };
        message.amount = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.title = reader.string();
                    break;
                case 2:
                    message.description = reader.string();
                    break;
                case 3:
                    message.recipient = reader.string();
                    break;
                case 4:
                    message.amount.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseCommunityPoolSpendProposal,
        };
        message.amount = [];
        if (object.title !== undefined && object.title !== null) {
            message.title = String(object.title);
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = String(object.description);
        }
        else {
            message.description = "";
        }
        if (object.recipient !== undefined && object.recipient !== null) {
            message.recipient = String(object.recipient);
        }
        else {
            message.recipient = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            for (const e of object.amount) {
                message.amount.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.title !== undefined && (obj.title = message.title);
        message.description !== undefined &&
            (obj.description = message.description);
        message.recipient !== undefined && (obj.recipient = message.recipient);
        if (message.amount) {
            obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
        }
        else {
            obj.amount = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseCommunityPoolSpendProposal,
        };
        message.amount = [];
        if (object.title !== undefined && object.title !== null) {
            message.title = object.title;
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = object.description;
        }
        else {
            message.description = "";
        }
        if (object.recipient !== undefined && object.recipient !== null) {
            message.recipient = object.recipient;
        }
        else {
            message.recipient = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            for (const e of object.amount) {
                message.amount.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseDelegatorStartingInfo = {
    previousPeriod: 0,
    stake: "",
    height: 0,
};
export const DelegatorStartingInfo = {
    encode(message, writer = Writer.create()) {
        if (message.previousPeriod !== 0) {
            writer.uint32(8).uint64(message.previousPeriod);
        }
        if (message.stake !== "") {
            writer.uint32(18).string(message.stake);
        }
        if (message.height !== 0) {
            writer.uint32(24).uint64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseDelegatorStartingInfo };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.previousPeriod = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.stake = reader.string();
                    break;
                case 3:
                    message.height = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseDelegatorStartingInfo };
        if (object.previousPeriod !== undefined && object.previousPeriod !== null) {
            message.previousPeriod = Number(object.previousPeriod);
        }
        else {
            message.previousPeriod = 0;
        }
        if (object.stake !== undefined && object.stake !== null) {
            message.stake = String(object.stake);
        }
        else {
            message.stake = "";
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = Number(object.height);
        }
        else {
            message.height = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.previousPeriod !== undefined &&
            (obj.previousPeriod = message.previousPeriod);
        message.stake !== undefined && (obj.stake = message.stake);
        message.height !== undefined && (obj.height = message.height);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseDelegatorStartingInfo };
        if (object.previousPeriod !== undefined && object.previousPeriod !== null) {
            message.previousPeriod = object.previousPeriod;
        }
        else {
            message.previousPeriod = 0;
        }
        if (object.stake !== undefined && object.stake !== null) {
            message.stake = object.stake;
        }
        else {
            message.stake = "";
        }
        if (object.height !== undefined && object.height !== null) {
            message.height = object.height;
        }
        else {
            message.height = 0;
        }
        return message;
    },
};
const baseDelegationDelegatorReward = { validatorAddress: "" };
export const DelegationDelegatorReward = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddress !== "") {
            writer.uint32(10).string(message.validatorAddress);
        }
        for (const v of message.reward) {
            DecCoin.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseDelegationDelegatorReward,
        };
        message.reward = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorAddress = reader.string();
                    break;
                case 2:
                    message.reward.push(DecCoin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseDelegationDelegatorReward,
        };
        message.reward = [];
        if (object.validatorAddress !== undefined &&
            object.validatorAddress !== null) {
            message.validatorAddress = String(object.validatorAddress);
        }
        else {
            message.validatorAddress = "";
        }
        if (object.reward !== undefined && object.reward !== null) {
            for (const e of object.reward) {
                message.reward.push(DecCoin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddress !== undefined &&
            (obj.validatorAddress = message.validatorAddress);
        if (message.reward) {
            obj.reward = message.reward.map((e) => e ? DecCoin.toJSON(e) : undefined);
        }
        else {
            obj.reward = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseDelegationDelegatorReward,
        };
        message.reward = [];
        if (object.validatorAddress !== undefined &&
            object.validatorAddress !== null) {
            message.validatorAddress = object.validatorAddress;
        }
        else {
            message.validatorAddress = "";
        }
        if (object.reward !== undefined && object.reward !== null) {
            for (const e of object.reward) {
                message.reward.push(DecCoin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseCommunityPoolSpendProposalWithDeposit = {
    title: "",
    description: "",
    recipient: "",
    amount: "",
    deposit: "",
};
export const CommunityPoolSpendProposalWithDeposit = {
    encode(message, writer = Writer.create()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.recipient !== "") {
            writer.uint32(26).string(message.recipient);
        }
        if (message.amount !== "") {
            writer.uint32(34).string(message.amount);
        }
        if (message.deposit !== "") {
            writer.uint32(42).string(message.deposit);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseCommunityPoolSpendProposalWithDeposit,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.title = reader.string();
                    break;
                case 2:
                    message.description = reader.string();
                    break;
                case 3:
                    message.recipient = reader.string();
                    break;
                case 4:
                    message.amount = reader.string();
                    break;
                case 5:
                    message.deposit = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseCommunityPoolSpendProposalWithDeposit,
        };
        if (object.title !== undefined && object.title !== null) {
            message.title = String(object.title);
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = String(object.description);
        }
        else {
            message.description = "";
        }
        if (object.recipient !== undefined && object.recipient !== null) {
            message.recipient = String(object.recipient);
        }
        else {
            message.recipient = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            message.amount = String(object.amount);
        }
        else {
            message.amount = "";
        }
        if (object.deposit !== undefined && object.deposit !== null) {
            message.deposit = String(object.deposit);
        }
        else {
            message.deposit = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.title !== undefined && (obj.title = message.title);
        message.description !== undefined &&
            (obj.description = message.description);
        message.recipient !== undefined && (obj.recipient = message.recipient);
        message.amount !== undefined && (obj.amount = message.amount);
        message.deposit !== undefined && (obj.deposit = message.deposit);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseCommunityPoolSpendProposalWithDeposit,
        };
        if (object.title !== undefined && object.title !== null) {
            message.title = object.title;
        }
        else {
            message.title = "";
        }
        if (object.description !== undefined && object.description !== null) {
            message.description = object.description;
        }
        else {
            message.description = "";
        }
        if (object.recipient !== undefined && object.recipient !== null) {
            message.recipient = object.recipient;
        }
        else {
            message.recipient = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            message.amount = object.amount;
        }
        else {
            message.amount = "";
        }
        if (object.deposit !== undefined && object.deposit !== null) {
            message.deposit = object.deposit;
        }
        else {
            message.deposit = "";
        }
        return message;
    },
};
var globalThis = (() => {
    if (typeof globalThis !== "undefined")
        return globalThis;
    if (typeof self !== "undefined")
        return self;
    if (typeof window !== "undefined")
        return window;
    if (typeof global !== "undefined")
        return global;
    throw "Unable to locate global object";
})();
function longToNumber(long) {
    if (long.gt(Number.MAX_SAFE_INTEGER)) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    return long.toNumber();
}
if (util.Long !== Long) {
    util.Long = Long;
    configure();
}
