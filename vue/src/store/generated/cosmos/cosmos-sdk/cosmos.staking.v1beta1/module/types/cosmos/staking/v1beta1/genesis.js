/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Params, Validator, Delegation, UnbondingDelegation, Redelegation, } from "../../../cosmos/staking/v1beta1/staking";
export const protobufPackage = "cosmos.staking.v1beta1";
const baseGenesisState = { exported: false };
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).ldelim();
        }
        if (message.lastTotalPower.length !== 0) {
            writer.uint32(18).bytes(message.lastTotalPower);
        }
        for (const v of message.lastValidatorPowers) {
            LastValidatorPower.encode(v, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.validators) {
            Validator.encode(v, writer.uint32(34).fork()).ldelim();
        }
        for (const v of message.delegations) {
            Delegation.encode(v, writer.uint32(42).fork()).ldelim();
        }
        for (const v of message.unbondingDelegations) {
            UnbondingDelegation.encode(v, writer.uint32(50).fork()).ldelim();
        }
        for (const v of message.redelegations) {
            Redelegation.encode(v, writer.uint32(58).fork()).ldelim();
        }
        if (message.exported === true) {
            writer.uint32(64).bool(message.exported);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.lastValidatorPowers = [];
        message.validators = [];
        message.delegations = [];
        message.unbondingDelegations = [];
        message.redelegations = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.params = Params.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.lastTotalPower = reader.bytes();
                    break;
                case 3:
                    message.lastValidatorPowers.push(LastValidatorPower.decode(reader, reader.uint32()));
                    break;
                case 4:
                    message.validators.push(Validator.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.delegations.push(Delegation.decode(reader, reader.uint32()));
                    break;
                case 6:
                    message.unbondingDelegations.push(UnbondingDelegation.decode(reader, reader.uint32()));
                    break;
                case 7:
                    message.redelegations.push(Redelegation.decode(reader, reader.uint32()));
                    break;
                case 8:
                    message.exported = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGenesisState };
        message.lastValidatorPowers = [];
        message.validators = [];
        message.delegations = [];
        message.unbondingDelegations = [];
        message.redelegations = [];
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromJSON(object.params);
        }
        else {
            message.params = undefined;
        }
        if (object.lastTotalPower !== undefined && object.lastTotalPower !== null) {
            message.lastTotalPower = bytesFromBase64(object.lastTotalPower);
        }
        if (object.lastValidatorPowers !== undefined &&
            object.lastValidatorPowers !== null) {
            for (const e of object.lastValidatorPowers) {
                message.lastValidatorPowers.push(LastValidatorPower.fromJSON(e));
            }
        }
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(Validator.fromJSON(e));
            }
        }
        if (object.delegations !== undefined && object.delegations !== null) {
            for (const e of object.delegations) {
                message.delegations.push(Delegation.fromJSON(e));
            }
        }
        if (object.unbondingDelegations !== undefined &&
            object.unbondingDelegations !== null) {
            for (const e of object.unbondingDelegations) {
                message.unbondingDelegations.push(UnbondingDelegation.fromJSON(e));
            }
        }
        if (object.redelegations !== undefined && object.redelegations !== null) {
            for (const e of object.redelegations) {
                message.redelegations.push(Redelegation.fromJSON(e));
            }
        }
        if (object.exported !== undefined && object.exported !== null) {
            message.exported = Boolean(object.exported);
        }
        else {
            message.exported = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.params !== undefined &&
            (obj.params = message.params ? Params.toJSON(message.params) : undefined);
        message.lastTotalPower !== undefined &&
            (obj.lastTotalPower = base64FromBytes(message.lastTotalPower !== undefined
                ? message.lastTotalPower
                : new Uint8Array()));
        if (message.lastValidatorPowers) {
            obj.lastValidatorPowers = message.lastValidatorPowers.map((e) => e ? LastValidatorPower.toJSON(e) : undefined);
        }
        else {
            obj.lastValidatorPowers = [];
        }
        if (message.validators) {
            obj.validators = message.validators.map((e) => e ? Validator.toJSON(e) : undefined);
        }
        else {
            obj.validators = [];
        }
        if (message.delegations) {
            obj.delegations = message.delegations.map((e) => e ? Delegation.toJSON(e) : undefined);
        }
        else {
            obj.delegations = [];
        }
        if (message.unbondingDelegations) {
            obj.unbondingDelegations = message.unbondingDelegations.map((e) => e ? UnbondingDelegation.toJSON(e) : undefined);
        }
        else {
            obj.unbondingDelegations = [];
        }
        if (message.redelegations) {
            obj.redelegations = message.redelegations.map((e) => e ? Redelegation.toJSON(e) : undefined);
        }
        else {
            obj.redelegations = [];
        }
        message.exported !== undefined && (obj.exported = message.exported);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.lastValidatorPowers = [];
        message.validators = [];
        message.delegations = [];
        message.unbondingDelegations = [];
        message.redelegations = [];
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromPartial(object.params);
        }
        else {
            message.params = undefined;
        }
        if (object.lastTotalPower !== undefined && object.lastTotalPower !== null) {
            message.lastTotalPower = object.lastTotalPower;
        }
        else {
            message.lastTotalPower = new Uint8Array();
        }
        if (object.lastValidatorPowers !== undefined &&
            object.lastValidatorPowers !== null) {
            for (const e of object.lastValidatorPowers) {
                message.lastValidatorPowers.push(LastValidatorPower.fromPartial(e));
            }
        }
        if (object.validators !== undefined && object.validators !== null) {
            for (const e of object.validators) {
                message.validators.push(Validator.fromPartial(e));
            }
        }
        if (object.delegations !== undefined && object.delegations !== null) {
            for (const e of object.delegations) {
                message.delegations.push(Delegation.fromPartial(e));
            }
        }
        if (object.unbondingDelegations !== undefined &&
            object.unbondingDelegations !== null) {
            for (const e of object.unbondingDelegations) {
                message.unbondingDelegations.push(UnbondingDelegation.fromPartial(e));
            }
        }
        if (object.redelegations !== undefined && object.redelegations !== null) {
            for (const e of object.redelegations) {
                message.redelegations.push(Redelegation.fromPartial(e));
            }
        }
        if (object.exported !== undefined && object.exported !== null) {
            message.exported = object.exported;
        }
        else {
            message.exported = false;
        }
        return message;
    },
};
const baseLastValidatorPower = { address: "", power: 0 };
export const LastValidatorPower = {
    encode(message, writer = Writer.create()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.power !== 0) {
            writer.uint32(16).int64(message.power);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseLastValidatorPower };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.address = reader.string();
                    break;
                case 2:
                    message.power = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseLastValidatorPower };
        if (object.address !== undefined && object.address !== null) {
            message.address = String(object.address);
        }
        else {
            message.address = "";
        }
        if (object.power !== undefined && object.power !== null) {
            message.power = Number(object.power);
        }
        else {
            message.power = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.address !== undefined && (obj.address = message.address);
        message.power !== undefined && (obj.power = message.power);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseLastValidatorPower };
        if (object.address !== undefined && object.address !== null) {
            message.address = object.address;
        }
        else {
            message.address = "";
        }
        if (object.power !== undefined && object.power !== null) {
            message.power = object.power;
        }
        else {
            message.power = 0;
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
const atob = globalThis.atob ||
    ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64) {
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; ++i) {
        arr[i] = bin.charCodeAt(i);
    }
    return arr;
}
const btoa = globalThis.btoa ||
    ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr) {
    const bin = [];
    for (let i = 0; i < arr.byteLength; ++i) {
        bin.push(String.fromCharCode(arr[i]));
    }
    return btoa(bin.join(""));
}
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
