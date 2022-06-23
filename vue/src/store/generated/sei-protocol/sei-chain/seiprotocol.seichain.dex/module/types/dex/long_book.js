/* eslint-disable */
import { OrderEntry } from "../dex/order_entry";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const baseLongBook = { price: "" };
export const LongBook = {
    encode(message, writer = Writer.create()) {
        if (message.price !== "") {
            writer.uint32(10).string(message.price);
        }
        if (message.entry !== undefined) {
            OrderEntry.encode(message.entry, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseLongBook };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.price = reader.string();
                    break;
                case 2:
                    message.entry = OrderEntry.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseLongBook };
        if (object.price !== undefined && object.price !== null) {
            message.price = String(object.price);
        }
        else {
            message.price = "";
        }
        if (object.entry !== undefined && object.entry !== null) {
            message.entry = OrderEntry.fromJSON(object.entry);
        }
        else {
            message.entry = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.price !== undefined && (obj.price = message.price);
        message.entry !== undefined &&
            (obj.entry = message.entry
                ? OrderEntry.toJSON(message.entry)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseLongBook };
        if (object.price !== undefined && object.price !== null) {
            message.price = object.price;
        }
        else {
            message.price = "";
        }
        if (object.entry !== undefined && object.entry !== null) {
            message.entry = OrderEntry.fromPartial(object.entry);
        }
        else {
            message.entry = undefined;
        }
        return message;
    },
};
