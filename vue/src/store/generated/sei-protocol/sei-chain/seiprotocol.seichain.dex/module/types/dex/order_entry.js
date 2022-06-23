/* eslint-disable */
import { denomFromJSON, denomToJSON } from "../dex/enums";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const baseOrderEntry = {
    price: "",
    quantity: "",
    allocationCreator: "",
    allocation: "",
    priceDenom: 0,
    assetDenom: 0,
};
export const OrderEntry = {
    encode(message, writer = Writer.create()) {
        if (message.price !== "") {
            writer.uint32(10).string(message.price);
        }
        if (message.quantity !== "") {
            writer.uint32(18).string(message.quantity);
        }
        for (const v of message.allocationCreator) {
            writer.uint32(26).string(v);
        }
        for (const v of message.allocation) {
            writer.uint32(34).string(v);
        }
        if (message.priceDenom !== 0) {
            writer.uint32(40).int32(message.priceDenom);
        }
        if (message.assetDenom !== 0) {
            writer.uint32(48).int32(message.assetDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseOrderEntry };
        message.allocationCreator = [];
        message.allocation = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.price = reader.string();
                    break;
                case 2:
                    message.quantity = reader.string();
                    break;
                case 3:
                    message.allocationCreator.push(reader.string());
                    break;
                case 4:
                    message.allocation.push(reader.string());
                    break;
                case 5:
                    message.priceDenom = reader.int32();
                    break;
                case 6:
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
        const message = { ...baseOrderEntry };
        message.allocationCreator = [];
        message.allocation = [];
        if (object.price !== undefined && object.price !== null) {
            message.price = String(object.price);
        }
        else {
            message.price = "";
        }
        if (object.quantity !== undefined && object.quantity !== null) {
            message.quantity = String(object.quantity);
        }
        else {
            message.quantity = "";
        }
        if (object.allocationCreator !== undefined &&
            object.allocationCreator !== null) {
            for (const e of object.allocationCreator) {
                message.allocationCreator.push(String(e));
            }
        }
        if (object.allocation !== undefined && object.allocation !== null) {
            for (const e of object.allocation) {
                message.allocation.push(String(e));
            }
        }
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
        message.price !== undefined && (obj.price = message.price);
        message.quantity !== undefined && (obj.quantity = message.quantity);
        if (message.allocationCreator) {
            obj.allocationCreator = message.allocationCreator.map((e) => e);
        }
        else {
            obj.allocationCreator = [];
        }
        if (message.allocation) {
            obj.allocation = message.allocation.map((e) => e);
        }
        else {
            obj.allocation = [];
        }
        message.priceDenom !== undefined &&
            (obj.priceDenom = denomToJSON(message.priceDenom));
        message.assetDenom !== undefined &&
            (obj.assetDenom = denomToJSON(message.assetDenom));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseOrderEntry };
        message.allocationCreator = [];
        message.allocation = [];
        if (object.price !== undefined && object.price !== null) {
            message.price = object.price;
        }
        else {
            message.price = "";
        }
        if (object.quantity !== undefined && object.quantity !== null) {
            message.quantity = object.quantity;
        }
        else {
            message.quantity = "";
        }
        if (object.allocationCreator !== undefined &&
            object.allocationCreator !== null) {
            for (const e of object.allocationCreator) {
                message.allocationCreator.push(e);
            }
        }
        if (object.allocation !== undefined && object.allocation !== null) {
            for (const e of object.allocation) {
                message.allocation.push(e);
            }
        }
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
