/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmwasm.wasm.v1";
const baseMsgIBCSend = {
    channel: "",
    timeoutHeight: 0,
    timeoutTimestamp: 0,
};
export const MsgIBCSend = {
    encode(message, writer = Writer.create()) {
        if (message.channel !== "") {
            writer.uint32(18).string(message.channel);
        }
        if (message.timeoutHeight !== 0) {
            writer.uint32(32).uint64(message.timeoutHeight);
        }
        if (message.timeoutTimestamp !== 0) {
            writer.uint32(40).uint64(message.timeoutTimestamp);
        }
        if (message.data.length !== 0) {
            writer.uint32(50).bytes(message.data);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgIBCSend };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    message.channel = reader.string();
                    break;
                case 4:
                    message.timeoutHeight = longToNumber(reader.uint64());
                    break;
                case 5:
                    message.timeoutTimestamp = longToNumber(reader.uint64());
                    break;
                case 6:
                    message.data = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgIBCSend };
        if (object.channel !== undefined && object.channel !== null) {
            message.channel = String(object.channel);
        }
        else {
            message.channel = "";
        }
        if (object.timeoutHeight !== undefined && object.timeoutHeight !== null) {
            message.timeoutHeight = Number(object.timeoutHeight);
        }
        else {
            message.timeoutHeight = 0;
        }
        if (object.timeoutTimestamp !== undefined &&
            object.timeoutTimestamp !== null) {
            message.timeoutTimestamp = Number(object.timeoutTimestamp);
        }
        else {
            message.timeoutTimestamp = 0;
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = bytesFromBase64(object.data);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.channel !== undefined && (obj.channel = message.channel);
        message.timeoutHeight !== undefined &&
            (obj.timeoutHeight = message.timeoutHeight);
        message.timeoutTimestamp !== undefined &&
            (obj.timeoutTimestamp = message.timeoutTimestamp);
        message.data !== undefined &&
            (obj.data = base64FromBytes(message.data !== undefined ? message.data : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgIBCSend };
        if (object.channel !== undefined && object.channel !== null) {
            message.channel = object.channel;
        }
        else {
            message.channel = "";
        }
        if (object.timeoutHeight !== undefined && object.timeoutHeight !== null) {
            message.timeoutHeight = object.timeoutHeight;
        }
        else {
            message.timeoutHeight = 0;
        }
        if (object.timeoutTimestamp !== undefined &&
            object.timeoutTimestamp !== null) {
            message.timeoutTimestamp = object.timeoutTimestamp;
        }
        else {
            message.timeoutTimestamp = 0;
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = new Uint8Array();
        }
        return message;
    },
};
const baseMsgIBCCloseChannel = { channel: "" };
export const MsgIBCCloseChannel = {
    encode(message, writer = Writer.create()) {
        if (message.channel !== "") {
            writer.uint32(18).string(message.channel);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgIBCCloseChannel };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    message.channel = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgIBCCloseChannel };
        if (object.channel !== undefined && object.channel !== null) {
            message.channel = String(object.channel);
        }
        else {
            message.channel = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.channel !== undefined && (obj.channel = message.channel);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgIBCCloseChannel };
        if (object.channel !== undefined && object.channel !== null) {
            message.channel = object.channel;
        }
        else {
            message.channel = "";
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
