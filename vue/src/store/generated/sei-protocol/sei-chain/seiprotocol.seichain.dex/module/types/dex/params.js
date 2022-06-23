/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const baseParams = { priceSnapshotRetention: 0 };
export const Params = {
    encode(message, writer = Writer.create()) {
        if (message.priceSnapshotRetention !== 0) {
            writer.uint32(8).uint64(message.priceSnapshotRetention);
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
                    message.priceSnapshotRetention = longToNumber(reader.uint64());
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
        if (object.priceSnapshotRetention !== undefined &&
            object.priceSnapshotRetention !== null) {
            message.priceSnapshotRetention = Number(object.priceSnapshotRetention);
        }
        else {
            message.priceSnapshotRetention = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.priceSnapshotRetention !== undefined &&
            (obj.priceSnapshotRetention = message.priceSnapshotRetention);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseParams };
        if (object.priceSnapshotRetention !== undefined &&
            object.priceSnapshotRetention !== null) {
            message.priceSnapshotRetention = object.priceSnapshotRetention;
        }
        else {
            message.priceSnapshotRetention = 0;
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
