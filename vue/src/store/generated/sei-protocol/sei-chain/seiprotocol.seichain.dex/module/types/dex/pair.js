/* eslint-disable */
import { denomFromJSON, denomToJSON } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const basePair = { priceDenom: 0, assetDenom: 0 };
export const Pair = {
    encode(message, writer = Writer.create()) {
        if (message.priceDenom !== 0) {
            writer.uint32(8).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(16).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...basePair };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.priceDenom = reader.int32();
                    break;
                case 2:
                    message.assetDenom = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...basePair };
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = denomFromJSON(object.priceDenom);
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = denomFromJSON(object.assetDenom);
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePair };
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = 0;
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = 0;
        }
        return message;
    },
};
