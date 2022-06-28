/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Duration } from "../../../google/protobuf/duration";
import { Any } from "../../../google/protobuf/any";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.feegrant.v1beta1";
const baseBasicAllowance = {};
export const BasicAllowance = {
    encode(message, writer = Writer.create()) {
        for (const v of message.spendLimit) {
            Coin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.expiration !== undefined) {
            Timestamp.encode(toTimestamp(message.expiration), writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseBasicAllowance };
        message.spendLimit = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.spendLimit.push(Coin.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.expiration = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseBasicAllowance };
        message.spendLimit = [];
        if (object.spendLimit !== undefined && object.spendLimit !== null) {
            for (const e of object.spendLimit) {
                message.spendLimit.push(Coin.fromJSON(e));
            }
        }
        if (object.expiration !== undefined && object.expiration !== null) {
            message.expiration = fromJsonTimestamp(object.expiration);
        }
        else {
            message.expiration = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.spendLimit) {
            obj.spendLimit = message.spendLimit.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.spendLimit = [];
        }
        message.expiration !== undefined &&
            (obj.expiration =
                message.expiration !== undefined
                    ? message.expiration.toISOString()
                    : null);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseBasicAllowance };
        message.spendLimit = [];
        if (object.spendLimit !== undefined && object.spendLimit !== null) {
            for (const e of object.spendLimit) {
                message.spendLimit.push(Coin.fromPartial(e));
            }
        }
        if (object.expiration !== undefined && object.expiration !== null) {
            message.expiration = object.expiration;
        }
        else {
            message.expiration = undefined;
        }
        return message;
    },
};
const basePeriodicAllowance = {};
export const PeriodicAllowance = {
    encode(message, writer = Writer.create()) {
        if (message.basic !== undefined) {
            BasicAllowance.encode(message.basic, writer.uint32(10).fork()).ldelim();
        }
        if (message.period !== undefined) {
            Duration.encode(message.period, writer.uint32(18).fork()).ldelim();
        }
        for (const v of message.periodSpendLimit) {
            Coin.encode(v, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.periodCanSpend) {
            Coin.encode(v, writer.uint32(34).fork()).ldelim();
        }
        if (message.periodReset !== undefined) {
            Timestamp.encode(toTimestamp(message.periodReset), writer.uint32(42).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePeriodicAllowance };
        message.periodSpendLimit = [];
        message.periodCanSpend = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.basic = BasicAllowance.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.period = Duration.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.periodSpendLimit.push(Coin.decode(reader, reader.uint32()));
                    break;
                case 4:
                    message.periodCanSpend.push(Coin.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.periodReset = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...basePeriodicAllowance };
        message.periodSpendLimit = [];
        message.periodCanSpend = [];
        if (object.basic !== undefined && object.basic !== null) {
            message.basic = BasicAllowance.fromJSON(object.basic);
        }
        else {
            message.basic = undefined;
        }
        if (object.period !== undefined && object.period !== null) {
            message.period = Duration.fromJSON(object.period);
        }
        else {
            message.period = undefined;
        }
        if (object.periodSpendLimit !== undefined &&
            object.periodSpendLimit !== null) {
            for (const e of object.periodSpendLimit) {
                message.periodSpendLimit.push(Coin.fromJSON(e));
            }
        }
        if (object.periodCanSpend !== undefined && object.periodCanSpend !== null) {
            for (const e of object.periodCanSpend) {
                message.periodCanSpend.push(Coin.fromJSON(e));
            }
        }
        if (object.periodReset !== undefined && object.periodReset !== null) {
            message.periodReset = fromJsonTimestamp(object.periodReset);
        }
        else {
            message.periodReset = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.basic !== undefined &&
            (obj.basic = message.basic
                ? BasicAllowance.toJSON(message.basic)
                : undefined);
        message.period !== undefined &&
            (obj.period = message.period
                ? Duration.toJSON(message.period)
                : undefined);
        if (message.periodSpendLimit) {
            obj.periodSpendLimit = message.periodSpendLimit.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.periodSpendLimit = [];
        }
        if (message.periodCanSpend) {
            obj.periodCanSpend = message.periodCanSpend.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.periodCanSpend = [];
        }
        message.periodReset !== undefined &&
            (obj.periodReset =
                message.periodReset !== undefined
                    ? message.periodReset.toISOString()
                    : null);
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePeriodicAllowance };
        message.periodSpendLimit = [];
        message.periodCanSpend = [];
        if (object.basic !== undefined && object.basic !== null) {
            message.basic = BasicAllowance.fromPartial(object.basic);
        }
        else {
            message.basic = undefined;
        }
        if (object.period !== undefined && object.period !== null) {
            message.period = Duration.fromPartial(object.period);
        }
        else {
            message.period = undefined;
        }
        if (object.periodSpendLimit !== undefined &&
            object.periodSpendLimit !== null) {
            for (const e of object.periodSpendLimit) {
                message.periodSpendLimit.push(Coin.fromPartial(e));
            }
        }
        if (object.periodCanSpend !== undefined && object.periodCanSpend !== null) {
            for (const e of object.periodCanSpend) {
                message.periodCanSpend.push(Coin.fromPartial(e));
            }
        }
        if (object.periodReset !== undefined && object.periodReset !== null) {
            message.periodReset = object.periodReset;
        }
        else {
            message.periodReset = undefined;
        }
        return message;
    },
};
const baseAllowedMsgAllowance = { allowedMessages: "" };
export const AllowedMsgAllowance = {
    encode(message, writer = Writer.create()) {
        if (message.allowance !== undefined) {
            Any.encode(message.allowance, writer.uint32(10).fork()).ldelim();
        }
        for (const v of message.allowedMessages) {
            writer.uint32(18).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseAllowedMsgAllowance };
        message.allowedMessages = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.allowance = Any.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.allowedMessages.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseAllowedMsgAllowance };
        message.allowedMessages = [];
        if (object.allowance !== undefined && object.allowance !== null) {
            message.allowance = Any.fromJSON(object.allowance);
        }
        else {
            message.allowance = undefined;
        }
        if (object.allowedMessages !== undefined &&
            object.allowedMessages !== null) {
            for (const e of object.allowedMessages) {
                message.allowedMessages.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.allowance !== undefined &&
            (obj.allowance = message.allowance
                ? Any.toJSON(message.allowance)
                : undefined);
        if (message.allowedMessages) {
            obj.allowedMessages = message.allowedMessages.map((e) => e);
        }
        else {
            obj.allowedMessages = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseAllowedMsgAllowance };
        message.allowedMessages = [];
        if (object.allowance !== undefined && object.allowance !== null) {
            message.allowance = Any.fromPartial(object.allowance);
        }
        else {
            message.allowance = undefined;
        }
        if (object.allowedMessages !== undefined &&
            object.allowedMessages !== null) {
            for (const e of object.allowedMessages) {
                message.allowedMessages.push(e);
            }
        }
        return message;
    },
};
const baseGrant = { granter: "", grantee: "" };
export const Grant = {
    encode(message, writer = Writer.create()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.allowance !== undefined) {
            Any.encode(message.allowance, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGrant };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.granter = reader.string();
                    break;
                case 2:
                    message.grantee = reader.string();
                    break;
                case 3:
                    message.allowance = Any.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGrant };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = String(object.granter);
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = String(object.grantee);
        }
        else {
            message.grantee = "";
        }
        if (object.allowance !== undefined && object.allowance !== null) {
            message.allowance = Any.fromJSON(object.allowance);
        }
        else {
            message.allowance = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.granter !== undefined && (obj.granter = message.granter);
        message.grantee !== undefined && (obj.grantee = message.grantee);
        message.allowance !== undefined &&
            (obj.allowance = message.allowance
                ? Any.toJSON(message.allowance)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGrant };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = object.granter;
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = object.grantee;
        }
        else {
            message.grantee = "";
        }
        if (object.allowance !== undefined && object.allowance !== null) {
            message.allowance = Any.fromPartial(object.allowance);
        }
        else {
            message.allowance = undefined;
        }
        return message;
    },
};
function toTimestamp(date) {
    const seconds = date.getTime() / 1000;
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = t.seconds * 1000;
    millis += t.nanos / 1000000;
    return new Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new Date(o);
    }
    else {
        return fromTimestamp(Timestamp.fromJSON(o));
    }
}
