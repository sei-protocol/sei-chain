/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import { Any } from "../../../google/protobuf/any";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.authz.v1beta1";
const baseGenericAuthorization = { msg: "" };
export const GenericAuthorization = {
    encode(message, writer = Writer.create()) {
        if (message.msg !== "") {
            writer.uint32(10).string(message.msg);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenericAuthorization };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.msg = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGenericAuthorization };
        if (object.msg !== undefined && object.msg !== null) {
            message.msg = String(object.msg);
        }
        else {
            message.msg = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.msg !== undefined && (obj.msg = message.msg);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenericAuthorization };
        if (object.msg !== undefined && object.msg !== null) {
            message.msg = object.msg;
        }
        else {
            message.msg = "";
        }
        return message;
    },
};
const baseGrant = {};
export const Grant = {
    encode(message, writer = Writer.create()) {
        if (message.authorization !== undefined) {
            Any.encode(message.authorization, writer.uint32(10).fork()).ldelim();
        }
        if (message.expiration !== undefined) {
            Timestamp.encode(toTimestamp(message.expiration), writer.uint32(18).fork()).ldelim();
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
                    message.authorization = Any.decode(reader, reader.uint32());
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
        const message = { ...baseGrant };
        if (object.authorization !== undefined && object.authorization !== null) {
            message.authorization = Any.fromJSON(object.authorization);
        }
        else {
            message.authorization = undefined;
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
        message.authorization !== undefined &&
            (obj.authorization = message.authorization
                ? Any.toJSON(message.authorization)
                : undefined);
        message.expiration !== undefined &&
            (obj.expiration =
                message.expiration !== undefined
                    ? message.expiration.toISOString()
                    : null);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGrant };
        if (object.authorization !== undefined && object.authorization !== null) {
            message.authorization = Any.fromPartial(object.authorization);
        }
        else {
            message.authorization = undefined;
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
const baseGrantAuthorization = { granter: "", grantee: "" };
export const GrantAuthorization = {
    encode(message, writer = Writer.create()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.authorization !== undefined) {
            Any.encode(message.authorization, writer.uint32(26).fork()).ldelim();
        }
        if (message.expiration !== undefined) {
            Timestamp.encode(toTimestamp(message.expiration), writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGrantAuthorization };
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
                    message.authorization = Any.decode(reader, reader.uint32());
                    break;
                case 4:
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
        const message = { ...baseGrantAuthorization };
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
        if (object.authorization !== undefined && object.authorization !== null) {
            message.authorization = Any.fromJSON(object.authorization);
        }
        else {
            message.authorization = undefined;
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
        message.granter !== undefined && (obj.granter = message.granter);
        message.grantee !== undefined && (obj.grantee = message.grantee);
        message.authorization !== undefined &&
            (obj.authorization = message.authorization
                ? Any.toJSON(message.authorization)
                : undefined);
        message.expiration !== undefined &&
            (obj.expiration =
                message.expiration !== undefined
                    ? message.expiration.toISOString()
                    : null);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGrantAuthorization };
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
        if (object.authorization !== undefined && object.authorization !== null) {
            message.authorization = Any.fromPartial(object.authorization);
        }
        else {
            message.authorization = undefined;
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
