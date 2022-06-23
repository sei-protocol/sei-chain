/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Pair } from "../dex/pair";
export const protobufPackage = "seiprotocol.seichain.dex";
const basePrice = { snapshotTimestampInSeconds: 0, price: "" };
export const Price = {
    encode(message, writer = Writer.create()) {
        if (message.snapshotTimestampInSeconds !== 0) {
            writer.uint32(8).uint64(message.snapshotTimestampInSeconds);
        }
        if (message.price !== "") {
            writer.uint32(18).string(message.price);
        }
        if (message.pair !== undefined) {
            Pair.encode(message.pair, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePrice };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.snapshotTimestampInSeconds = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.price = reader.string();
                    break;
                case 3:
                    message.pair = Pair.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...basePrice };
        if (object.snapshotTimestampInSeconds !== undefined &&
            object.snapshotTimestampInSeconds !== null) {
            message.snapshotTimestampInSeconds = Number(object.snapshotTimestampInSeconds);
        }
        else {
            message.snapshotTimestampInSeconds = 0;
        }
        if (object.price !== undefined && object.price !== null) {
            message.price = String(object.price);
        }
        else {
            message.price = "";
        }
        if (object.pair !== undefined && object.pair !== null) {
            message.pair = Pair.fromJSON(object.pair);
        }
        else {
            message.pair = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.snapshotTimestampInSeconds !== undefined &&
            (obj.snapshotTimestampInSeconds = message.snapshotTimestampInSeconds);
        message.price !== undefined && (obj.price = message.price);
        message.pair !== undefined &&
            (obj.pair = message.pair ? Pair.toJSON(message.pair) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePrice };
        if (object.snapshotTimestampInSeconds !== undefined &&
            object.snapshotTimestampInSeconds !== null) {
            message.snapshotTimestampInSeconds = object.snapshotTimestampInSeconds;
        }
        else {
            message.snapshotTimestampInSeconds = 0;
        }
        if (object.price !== undefined && object.price !== null) {
            message.price = object.price;
        }
        else {
            message.price = "";
        }
        if (object.pair !== undefined && object.pair !== null) {
            message.pair = Pair.fromPartial(object.pair);
        }
        else {
            message.pair = undefined;
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
