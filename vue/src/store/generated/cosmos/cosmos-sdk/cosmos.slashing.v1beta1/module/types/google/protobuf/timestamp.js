/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "google.protobuf";
const baseTimestamp = { seconds: 0, nanos: 0 };
export const Timestamp = {
    encode(message, writer = Writer.create()) {
        if (message.seconds !== 0) {
            writer.uint32(8).int64(message.seconds);
        }
        if (message.nanos !== 0) {
            writer.uint32(16).int32(message.nanos);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseTimestamp };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.seconds = longToNumber(reader.int64());
                    break;
                case 2:
                    message.nanos = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseTimestamp };
        if (object.seconds !== undefined && object.seconds !== null) {
            message.seconds = Number(object.seconds);
        }
        else {
            message.seconds = 0;
        }
        if (object.nanos !== undefined && object.nanos !== null) {
            message.nanos = Number(object.nanos);
        }
        else {
            message.nanos = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.seconds !== undefined && (obj.seconds = message.seconds);
        message.nanos !== undefined && (obj.nanos = message.nanos);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseTimestamp };
        if (object.seconds !== undefined && object.seconds !== null) {
            message.seconds = object.seconds;
        }
        else {
            message.seconds = 0;
        }
        if (object.nanos !== undefined && object.nanos !== null) {
            message.nanos = object.nanos;
        }
        else {
            message.nanos = 0;
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
