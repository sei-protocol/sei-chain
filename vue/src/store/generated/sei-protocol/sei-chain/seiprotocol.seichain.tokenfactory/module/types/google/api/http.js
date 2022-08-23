/* eslint-disable */
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "google.api";
const baseHttp = { fullyDecodeReservedExpansion: false };
export const Http = {
    encode(message, writer = Writer.create()) {
        for (const v of message.rules) {
            HttpRule.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.fullyDecodeReservedExpansion === true) {
            writer.uint32(16).bool(message.fullyDecodeReservedExpansion);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseHttp };
        message.rules = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.rules.push(HttpRule.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.fullyDecodeReservedExpansion = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseHttp };
        message.rules = [];
        if (object.rules !== undefined && object.rules !== null) {
            for (const e of object.rules) {
                message.rules.push(HttpRule.fromJSON(e));
            }
        }
        if (object.fullyDecodeReservedExpansion !== undefined &&
            object.fullyDecodeReservedExpansion !== null) {
            message.fullyDecodeReservedExpansion = Boolean(object.fullyDecodeReservedExpansion);
        }
        else {
            message.fullyDecodeReservedExpansion = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.rules) {
            obj.rules = message.rules.map((e) => e ? HttpRule.toJSON(e) : undefined);
        }
        else {
            obj.rules = [];
        }
        message.fullyDecodeReservedExpansion !== undefined &&
            (obj.fullyDecodeReservedExpansion = message.fullyDecodeReservedExpansion);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseHttp };
        message.rules = [];
        if (object.rules !== undefined && object.rules !== null) {
            for (const e of object.rules) {
                message.rules.push(HttpRule.fromPartial(e));
            }
        }
        if (object.fullyDecodeReservedExpansion !== undefined &&
            object.fullyDecodeReservedExpansion !== null) {
            message.fullyDecodeReservedExpansion =
                object.fullyDecodeReservedExpansion;
        }
        else {
            message.fullyDecodeReservedExpansion = false;
        }
        return message;
    },
};
const baseHttpRule = { selector: "", body: "", responseBody: "" };
export const HttpRule = {
    encode(message, writer = Writer.create()) {
        if (message.selector !== "") {
            writer.uint32(10).string(message.selector);
        }
        if (message.get !== undefined) {
            writer.uint32(18).string(message.get);
        }
        if (message.put !== undefined) {
            writer.uint32(26).string(message.put);
        }
        if (message.post !== undefined) {
            writer.uint32(34).string(message.post);
        }
        if (message.delete !== undefined) {
            writer.uint32(42).string(message.delete);
        }
        if (message.patch !== undefined) {
            writer.uint32(50).string(message.patch);
        }
        if (message.custom !== undefined) {
            CustomHttpPattern.encode(message.custom, writer.uint32(66).fork()).ldelim();
        }
        if (message.body !== "") {
            writer.uint32(58).string(message.body);
        }
        if (message.responseBody !== "") {
            writer.uint32(98).string(message.responseBody);
        }
        for (const v of message.additionalBindings) {
            HttpRule.encode(v, writer.uint32(90).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseHttpRule };
        message.additionalBindings = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.selector = reader.string();
                    break;
                case 2:
                    message.get = reader.string();
                    break;
                case 3:
                    message.put = reader.string();
                    break;
                case 4:
                    message.post = reader.string();
                    break;
                case 5:
                    message.delete = reader.string();
                    break;
                case 6:
                    message.patch = reader.string();
                    break;
                case 8:
                    message.custom = CustomHttpPattern.decode(reader, reader.uint32());
                    break;
                case 7:
                    message.body = reader.string();
                    break;
                case 12:
                    message.responseBody = reader.string();
                    break;
                case 11:
                    message.additionalBindings.push(HttpRule.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseHttpRule };
        message.additionalBindings = [];
        if (object.selector !== undefined && object.selector !== null) {
            message.selector = String(object.selector);
        }
        else {
            message.selector = "";
        }
        if (object.get !== undefined && object.get !== null) {
            message.get = String(object.get);
        }
        else {
            message.get = undefined;
        }
        if (object.put !== undefined && object.put !== null) {
            message.put = String(object.put);
        }
        else {
            message.put = undefined;
        }
        if (object.post !== undefined && object.post !== null) {
            message.post = String(object.post);
        }
        else {
            message.post = undefined;
        }
        if (object.delete !== undefined && object.delete !== null) {
            message.delete = String(object.delete);
        }
        else {
            message.delete = undefined;
        }
        if (object.patch !== undefined && object.patch !== null) {
            message.patch = String(object.patch);
        }
        else {
            message.patch = undefined;
        }
        if (object.custom !== undefined && object.custom !== null) {
            message.custom = CustomHttpPattern.fromJSON(object.custom);
        }
        else {
            message.custom = undefined;
        }
        if (object.body !== undefined && object.body !== null) {
            message.body = String(object.body);
        }
        else {
            message.body = "";
        }
        if (object.responseBody !== undefined && object.responseBody !== null) {
            message.responseBody = String(object.responseBody);
        }
        else {
            message.responseBody = "";
        }
        if (object.additionalBindings !== undefined &&
            object.additionalBindings !== null) {
            for (const e of object.additionalBindings) {
                message.additionalBindings.push(HttpRule.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.selector !== undefined && (obj.selector = message.selector);
        message.get !== undefined && (obj.get = message.get);
        message.put !== undefined && (obj.put = message.put);
        message.post !== undefined && (obj.post = message.post);
        message.delete !== undefined && (obj.delete = message.delete);
        message.patch !== undefined && (obj.patch = message.patch);
        message.custom !== undefined &&
            (obj.custom = message.custom
                ? CustomHttpPattern.toJSON(message.custom)
                : undefined);
        message.body !== undefined && (obj.body = message.body);
        message.responseBody !== undefined &&
            (obj.responseBody = message.responseBody);
        if (message.additionalBindings) {
            obj.additionalBindings = message.additionalBindings.map((e) => e ? HttpRule.toJSON(e) : undefined);
        }
        else {
            obj.additionalBindings = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseHttpRule };
        message.additionalBindings = [];
        if (object.selector !== undefined && object.selector !== null) {
            message.selector = object.selector;
        }
        else {
            message.selector = "";
        }
        if (object.get !== undefined && object.get !== null) {
            message.get = object.get;
        }
        else {
            message.get = undefined;
        }
        if (object.put !== undefined && object.put !== null) {
            message.put = object.put;
        }
        else {
            message.put = undefined;
        }
        if (object.post !== undefined && object.post !== null) {
            message.post = object.post;
        }
        else {
            message.post = undefined;
        }
        if (object.delete !== undefined && object.delete !== null) {
            message.delete = object.delete;
        }
        else {
            message.delete = undefined;
        }
        if (object.patch !== undefined && object.patch !== null) {
            message.patch = object.patch;
        }
        else {
            message.patch = undefined;
        }
        if (object.custom !== undefined && object.custom !== null) {
            message.custom = CustomHttpPattern.fromPartial(object.custom);
        }
        else {
            message.custom = undefined;
        }
        if (object.body !== undefined && object.body !== null) {
            message.body = object.body;
        }
        else {
            message.body = "";
        }
        if (object.responseBody !== undefined && object.responseBody !== null) {
            message.responseBody = object.responseBody;
        }
        else {
            message.responseBody = "";
        }
        if (object.additionalBindings !== undefined &&
            object.additionalBindings !== null) {
            for (const e of object.additionalBindings) {
                message.additionalBindings.push(HttpRule.fromPartial(e));
            }
        }
        return message;
    },
};
const baseCustomHttpPattern = { kind: "", path: "" };
export const CustomHttpPattern = {
    encode(message, writer = Writer.create()) {
        if (message.kind !== "") {
            writer.uint32(10).string(message.kind);
        }
        if (message.path !== "") {
            writer.uint32(18).string(message.path);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseCustomHttpPattern };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.kind = reader.string();
                    break;
                case 2:
                    message.path = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseCustomHttpPattern };
        if (object.kind !== undefined && object.kind !== null) {
            message.kind = String(object.kind);
        }
        else {
            message.kind = "";
        }
        if (object.path !== undefined && object.path !== null) {
            message.path = String(object.path);
        }
        else {
            message.path = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.kind !== undefined && (obj.kind = message.kind);
        message.path !== undefined && (obj.path = message.path);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseCustomHttpPattern };
        if (object.kind !== undefined && object.kind !== null) {
            message.kind = object.kind;
        }
        else {
            message.kind = "";
        }
        if (object.path !== undefined && object.path !== null) {
            message.path = object.path;
        }
        else {
            message.path = "";
        }
        return message;
    },
};
