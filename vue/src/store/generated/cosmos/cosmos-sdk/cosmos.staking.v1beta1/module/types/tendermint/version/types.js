/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "tendermint.version";
const baseApp = { protocol: 0, software: "" };
export const App = {
    encode(message, writer = Writer.create()) {
        if (message.protocol !== 0) {
            writer.uint32(8).uint64(message.protocol);
        }
        if (message.software !== "") {
            writer.uint32(18).string(message.software);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseApp };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.protocol = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.software = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseApp };
        if (object.protocol !== undefined && object.protocol !== null) {
            message.protocol = Number(object.protocol);
        }
        else {
            message.protocol = 0;
        }
        if (object.software !== undefined && object.software !== null) {
            message.software = String(object.software);
        }
        else {
            message.software = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.protocol !== undefined && (obj.protocol = message.protocol);
        message.software !== undefined && (obj.software = message.software);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseApp };
        if (object.protocol !== undefined && object.protocol !== null) {
            message.protocol = object.protocol;
        }
        else {
            message.protocol = 0;
        }
        if (object.software !== undefined && object.software !== null) {
            message.software = object.software;
        }
        else {
            message.software = "";
        }
        return message;
    },
};
const baseConsensus = { block: 0, app: 0 };
export const Consensus = {
    encode(message, writer = Writer.create()) {
        if (message.block !== 0) {
            writer.uint32(8).uint64(message.block);
        }
        if (message.app !== 0) {
            writer.uint32(16).uint64(message.app);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseConsensus };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.block = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.app = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseConsensus };
        if (object.block !== undefined && object.block !== null) {
            message.block = Number(object.block);
        }
        else {
            message.block = 0;
        }
        if (object.app !== undefined && object.app !== null) {
            message.app = Number(object.app);
        }
        else {
            message.app = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.block !== undefined && (obj.block = message.block);
        message.app !== undefined && (obj.app = message.app);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseConsensus };
        if (object.block !== undefined && object.block !== null) {
            message.block = object.block;
        }
        else {
            message.block = 0;
        }
        if (object.app !== undefined && object.app !== null) {
            message.app = object.app;
        }
        else {
            message.app = 0;
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
