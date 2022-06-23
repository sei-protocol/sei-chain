/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "ibc.applications.transfer.v1";
const baseDenomTrace = { path: "", baseDenom: "" };
export const DenomTrace = {
    encode(message, writer = Writer.create()) {
        if (message.path !== "") {
            writer.uint32(10).string(message.path);
        }
        if (message.baseDenom !== "") {
            writer.uint32(18).string(message.baseDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseDenomTrace };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.path = reader.string();
                    break;
                case 2:
                    message.baseDenom = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseDenomTrace };
        if (object.path !== undefined && object.path !== null) {
            message.path = String(object.path);
        }
        else {
            message.path = "";
        }
        if (object.baseDenom !== undefined && object.baseDenom !== null) {
            message.baseDenom = String(object.baseDenom);
        }
        else {
            message.baseDenom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.path !== undefined && (obj.path = message.path);
        message.baseDenom !== undefined && (obj.baseDenom = message.baseDenom);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseDenomTrace };
        if (object.path !== undefined && object.path !== null) {
            message.path = object.path;
        }
        else {
            message.path = "";
        }
        if (object.baseDenom !== undefined && object.baseDenom !== null) {
            message.baseDenom = object.baseDenom;
        }
        else {
            message.baseDenom = "";
        }
        return message;
    },
};
const baseParams = { sendEnabled: false, receiveEnabled: false };
export const Params = {
    encode(message, writer = Writer.create()) {
        if (message.sendEnabled === true) {
            writer.uint32(8).bool(message.sendEnabled);
        }
        if (message.receiveEnabled === true) {
            writer.uint32(16).bool(message.receiveEnabled);
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
                    message.sendEnabled = reader.bool();
                    break;
                case 2:
                    message.receiveEnabled = reader.bool();
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
        if (object.sendEnabled !== undefined && object.sendEnabled !== null) {
            message.sendEnabled = Boolean(object.sendEnabled);
        }
        else {
            message.sendEnabled = false;
        }
        if (object.receiveEnabled !== undefined && object.receiveEnabled !== null) {
            message.receiveEnabled = Boolean(object.receiveEnabled);
        }
        else {
            message.receiveEnabled = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sendEnabled !== undefined &&
            (obj.sendEnabled = message.sendEnabled);
        message.receiveEnabled !== undefined &&
            (obj.receiveEnabled = message.receiveEnabled);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseParams };
        if (object.sendEnabled !== undefined && object.sendEnabled !== null) {
            message.sendEnabled = object.sendEnabled;
        }
        else {
            message.sendEnabled = false;
        }
        if (object.receiveEnabled !== undefined && object.receiveEnabled !== null) {
            message.receiveEnabled = object.receiveEnabled;
        }
        else {
            message.receiveEnabled = false;
        }
        return message;
    },
};
