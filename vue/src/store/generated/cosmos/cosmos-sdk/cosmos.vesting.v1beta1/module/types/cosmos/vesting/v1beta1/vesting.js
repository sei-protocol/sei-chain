/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { BaseAccount } from "../../../cosmos/auth/v1beta1/auth";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
export const protobufPackage = "cosmos.vesting.v1beta1";
const baseBaseVestingAccount = { endTime: 0 };
export const BaseVestingAccount = {
    encode(message, writer = Writer.create()) {
        if (message.baseAccount !== undefined) {
            BaseAccount.encode(message.baseAccount, writer.uint32(10).fork()).ldelim();
        }
        for (const v of message.originalVesting) {
            Coin.encode(v, writer.uint32(18).fork()).ldelim();
        }
        for (const v of message.delegatedFree) {
            Coin.encode(v, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.delegatedVesting) {
            Coin.encode(v, writer.uint32(34).fork()).ldelim();
        }
        if (message.endTime !== 0) {
            writer.uint32(40).int64(message.endTime);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseBaseVestingAccount };
        message.originalVesting = [];
        message.delegatedFree = [];
        message.delegatedVesting = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.baseAccount = BaseAccount.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.originalVesting.push(Coin.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.delegatedFree.push(Coin.decode(reader, reader.uint32()));
                    break;
                case 4:
                    message.delegatedVesting.push(Coin.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.endTime = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseBaseVestingAccount };
        message.originalVesting = [];
        message.delegatedFree = [];
        message.delegatedVesting = [];
        if (object.baseAccount !== undefined && object.baseAccount !== null) {
            message.baseAccount = BaseAccount.fromJSON(object.baseAccount);
        }
        else {
            message.baseAccount = undefined;
        }
        if (object.originalVesting !== undefined &&
            object.originalVesting !== null) {
            for (const e of object.originalVesting) {
                message.originalVesting.push(Coin.fromJSON(e));
            }
        }
        if (object.delegatedFree !== undefined && object.delegatedFree !== null) {
            for (const e of object.delegatedFree) {
                message.delegatedFree.push(Coin.fromJSON(e));
            }
        }
        if (object.delegatedVesting !== undefined &&
            object.delegatedVesting !== null) {
            for (const e of object.delegatedVesting) {
                message.delegatedVesting.push(Coin.fromJSON(e));
            }
        }
        if (object.endTime !== undefined && object.endTime !== null) {
            message.endTime = Number(object.endTime);
        }
        else {
            message.endTime = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.baseAccount !== undefined &&
            (obj.baseAccount = message.baseAccount
                ? BaseAccount.toJSON(message.baseAccount)
                : undefined);
        if (message.originalVesting) {
            obj.originalVesting = message.originalVesting.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.originalVesting = [];
        }
        if (message.delegatedFree) {
            obj.delegatedFree = message.delegatedFree.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.delegatedFree = [];
        }
        if (message.delegatedVesting) {
            obj.delegatedVesting = message.delegatedVesting.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.delegatedVesting = [];
        }
        message.endTime !== undefined && (obj.endTime = message.endTime);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseBaseVestingAccount };
        message.originalVesting = [];
        message.delegatedFree = [];
        message.delegatedVesting = [];
        if (object.baseAccount !== undefined && object.baseAccount !== null) {
            message.baseAccount = BaseAccount.fromPartial(object.baseAccount);
        }
        else {
            message.baseAccount = undefined;
        }
        if (object.originalVesting !== undefined &&
            object.originalVesting !== null) {
            for (const e of object.originalVesting) {
                message.originalVesting.push(Coin.fromPartial(e));
            }
        }
        if (object.delegatedFree !== undefined && object.delegatedFree !== null) {
            for (const e of object.delegatedFree) {
                message.delegatedFree.push(Coin.fromPartial(e));
            }
        }
        if (object.delegatedVesting !== undefined &&
            object.delegatedVesting !== null) {
            for (const e of object.delegatedVesting) {
                message.delegatedVesting.push(Coin.fromPartial(e));
            }
        }
        if (object.endTime !== undefined && object.endTime !== null) {
            message.endTime = object.endTime;
        }
        else {
            message.endTime = 0;
        }
        return message;
    },
};
const baseContinuousVestingAccount = { startTime: 0 };
export const ContinuousVestingAccount = {
    encode(message, writer = Writer.create()) {
        if (message.baseVestingAccount !== undefined) {
            BaseVestingAccount.encode(message.baseVestingAccount, writer.uint32(10).fork()).ldelim();
        }
        if (message.startTime !== 0) {
            writer.uint32(16).int64(message.startTime);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseContinuousVestingAccount,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.baseVestingAccount = BaseVestingAccount.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.startTime = longToNumber(reader.int64());
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
            ...baseContinuousVestingAccount,
        };
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromJSON(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        if (object.startTime !== undefined && object.startTime !== null) {
            message.startTime = Number(object.startTime);
        }
        else {
            message.startTime = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.baseVestingAccount !== undefined &&
            (obj.baseVestingAccount = message.baseVestingAccount
                ? BaseVestingAccount.toJSON(message.baseVestingAccount)
                : undefined);
        message.startTime !== undefined && (obj.startTime = message.startTime);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseContinuousVestingAccount,
        };
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromPartial(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        if (object.startTime !== undefined && object.startTime !== null) {
            message.startTime = object.startTime;
        }
        else {
            message.startTime = 0;
        }
        return message;
    },
};
const baseDelayedVestingAccount = {};
export const DelayedVestingAccount = {
    encode(message, writer = Writer.create()) {
        if (message.baseVestingAccount !== undefined) {
            BaseVestingAccount.encode(message.baseVestingAccount, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseDelayedVestingAccount };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.baseVestingAccount = BaseVestingAccount.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseDelayedVestingAccount };
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromJSON(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.baseVestingAccount !== undefined &&
            (obj.baseVestingAccount = message.baseVestingAccount
                ? BaseVestingAccount.toJSON(message.baseVestingAccount)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseDelayedVestingAccount };
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromPartial(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        return message;
    },
};
const basePeriod = { length: 0 };
export const Period = {
    encode(message, writer = Writer.create()) {
        if (message.length !== 0) {
            writer.uint32(8).int64(message.length);
        }
        for (const v of message.amount) {
            Coin.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePeriod };
        message.amount = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.length = longToNumber(reader.int64());
                    break;
                case 2:
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
        const message = { ...basePeriod };
        message.amount = [];
        if (object.length !== undefined && object.length !== null) {
            message.length = Number(object.length);
        }
        else {
            message.length = 0;
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
        message.length !== undefined && (obj.length = message.length);
        if (message.amount) {
            obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
        }
        else {
            obj.amount = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePeriod };
        message.amount = [];
        if (object.length !== undefined && object.length !== null) {
            message.length = object.length;
        }
        else {
            message.length = 0;
        }
        if (object.amount !== undefined && object.amount !== null) {
            for (const e of object.amount) {
                message.amount.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
const basePeriodicVestingAccount = { startTime: 0 };
export const PeriodicVestingAccount = {
    encode(message, writer = Writer.create()) {
        if (message.baseVestingAccount !== undefined) {
            BaseVestingAccount.encode(message.baseVestingAccount, writer.uint32(10).fork()).ldelim();
        }
        if (message.startTime !== 0) {
            writer.uint32(16).int64(message.startTime);
        }
        for (const v of message.vestingPeriods) {
            Period.encode(v, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePeriodicVestingAccount };
        message.vestingPeriods = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.baseVestingAccount = BaseVestingAccount.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.startTime = longToNumber(reader.int64());
                    break;
                case 3:
                    message.vestingPeriods.push(Period.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...basePeriodicVestingAccount };
        message.vestingPeriods = [];
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromJSON(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        if (object.startTime !== undefined && object.startTime !== null) {
            message.startTime = Number(object.startTime);
        }
        else {
            message.startTime = 0;
        }
        if (object.vestingPeriods !== undefined && object.vestingPeriods !== null) {
            for (const e of object.vestingPeriods) {
                message.vestingPeriods.push(Period.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.baseVestingAccount !== undefined &&
            (obj.baseVestingAccount = message.baseVestingAccount
                ? BaseVestingAccount.toJSON(message.baseVestingAccount)
                : undefined);
        message.startTime !== undefined && (obj.startTime = message.startTime);
        if (message.vestingPeriods) {
            obj.vestingPeriods = message.vestingPeriods.map((e) => e ? Period.toJSON(e) : undefined);
        }
        else {
            obj.vestingPeriods = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePeriodicVestingAccount };
        message.vestingPeriods = [];
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromPartial(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        if (object.startTime !== undefined && object.startTime !== null) {
            message.startTime = object.startTime;
        }
        else {
            message.startTime = 0;
        }
        if (object.vestingPeriods !== undefined && object.vestingPeriods !== null) {
            for (const e of object.vestingPeriods) {
                message.vestingPeriods.push(Period.fromPartial(e));
            }
        }
        return message;
    },
};
const basePermanentLockedAccount = {};
export const PermanentLockedAccount = {
    encode(message, writer = Writer.create()) {
        if (message.baseVestingAccount !== undefined) {
            BaseVestingAccount.encode(message.baseVestingAccount, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePermanentLockedAccount };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.baseVestingAccount = BaseVestingAccount.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...basePermanentLockedAccount };
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromJSON(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.baseVestingAccount !== undefined &&
            (obj.baseVestingAccount = message.baseVestingAccount
                ? BaseVestingAccount.toJSON(message.baseVestingAccount)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePermanentLockedAccount };
        if (object.baseVestingAccount !== undefined &&
            object.baseVestingAccount !== null) {
            message.baseVestingAccount = BaseVestingAccount.fromPartial(object.baseVestingAccount);
        }
        else {
            message.baseVestingAccount = undefined;
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
