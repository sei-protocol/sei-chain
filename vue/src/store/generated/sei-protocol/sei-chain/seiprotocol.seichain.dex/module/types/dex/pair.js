/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const basePair = { priceDenom: "", assetDenom: "", ticksize: "" };
export const Pair = {
    encode(message, writer = Writer.create()) {
        if (message.priceDenom !== "") {
            writer.uint32(10).string(message.priceDenom);
        }
        if (message.assetDenom !== "") {
            writer.uint32(18).string(message.assetDenom);
        }
        if (message.ticksize !== "") {
            writer.uint32(26).string(message.ticksize);
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
                    message.priceDenom = reader.string();
                    break;
                case 2:
                    message.assetDenom = reader.string();
                    break;
                case 3:
                    message.ticksize = reader.string();
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
            message.priceDenom = String(object.priceDenom);
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = String(object.assetDenom);
        }
        else {
            message.assetDenom = "";
        }
        if (object.ticksize !== undefined && object.ticksize !== null) {
            message.ticksize = String(object.ticksize);
        }
        else {
            message.ticksize = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.priceDenom !== undefined && (obj.priceDenom = message.priceDenom);
        message.assetDenom !== undefined && (obj.assetDenom = message.assetDenom);
        message.ticksize !== undefined && (obj.ticksize = message.ticksize);
        return obj;
    },
    fromPartial(object) {
        const message = { ...basePair };
        if (object.priceDenom !== undefined && object.priceDenom !== null) {
            message.priceDenom = object.priceDenom;
        }
        else {
            message.priceDenom = "";
        }
        if (object.assetDenom !== undefined && object.assetDenom !== null) {
            message.assetDenom = object.assetDenom;
        }
        else {
            message.assetDenom = "";
        }
        if (object.ticksize !== undefined && object.ticksize !== null) {
            message.ticksize = object.ticksize;
        }
        else {
            message.ticksize = "";
        }
        return message;
    },
};
const baseBatchContractPair = { contractAddr: "" };
export const BatchContractPair = {
    encode(message, writer = Writer.create()) {
        if (message.contractAddr !== "") {
            writer.uint32(10).string(message.contractAddr);
        }
        for (const v of message.pairs) {
            Pair.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseBatchContractPair };
        message.pairs = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.contractAddr = reader.string();
                    break;
                case 2:
                    message.pairs.push(Pair.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseBatchContractPair };
        message.pairs = [];
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.pairs !== undefined && object.pairs !== null) {
            for (const e of object.pairs) {
                message.pairs.push(Pair.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        if (message.pairs) {
            obj.pairs = message.pairs.map((e) => (e ? Pair.toJSON(e) : undefined));
        }
        else {
            obj.pairs = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseBatchContractPair };
        message.pairs = [];
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.pairs !== undefined && object.pairs !== null) {
            for (const e of object.pairs) {
                message.pairs.push(Pair.fromPartial(e));
            }
        }
        return message;
    },
};
