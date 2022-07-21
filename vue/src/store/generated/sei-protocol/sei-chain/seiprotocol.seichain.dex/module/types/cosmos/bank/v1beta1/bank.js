/* eslint-disable */
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.bank.v1beta1";
const baseParams = { defaultSendEnabled: false };
export const Params = {
    encode(message, writer = Writer.create()) {
        for (const v of message.sendEnabled) {
            SendEnabled.encode(v, writer.uint32(10).fork()).ldelim();
        }
        if (message.defaultSendEnabled === true) {
            writer.uint32(16).bool(message.defaultSendEnabled);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseParams };
        message.sendEnabled = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sendEnabled.push(SendEnabled.decode(reader, reader.uint32()));
                    break;
                case 2:
                    message.defaultSendEnabled = reader.bool();
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
        message.sendEnabled = [];
        if (object.sendEnabled !== undefined && object.sendEnabled !== null) {
            for (const e of object.sendEnabled) {
                message.sendEnabled.push(SendEnabled.fromJSON(e));
            }
        }
        if (object.defaultSendEnabled !== undefined &&
            object.defaultSendEnabled !== null) {
            message.defaultSendEnabled = Boolean(object.defaultSendEnabled);
        }
        else {
            message.defaultSendEnabled = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.sendEnabled) {
            obj.sendEnabled = message.sendEnabled.map((e) => e ? SendEnabled.toJSON(e) : undefined);
        }
        else {
            obj.sendEnabled = [];
        }
        message.defaultSendEnabled !== undefined &&
            (obj.defaultSendEnabled = message.defaultSendEnabled);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseParams };
        message.sendEnabled = [];
        if (object.sendEnabled !== undefined && object.sendEnabled !== null) {
            for (const e of object.sendEnabled) {
                message.sendEnabled.push(SendEnabled.fromPartial(e));
            }
        }
        if (object.defaultSendEnabled !== undefined &&
            object.defaultSendEnabled !== null) {
            message.defaultSendEnabled = object.defaultSendEnabled;
        }
        else {
            message.defaultSendEnabled = false;
        }
        return message;
    },
};
const baseSendEnabled = { denom: "", enabled: false };
export const SendEnabled = {
    encode(message, writer = Writer.create()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.enabled === true) {
            writer.uint32(16).bool(message.enabled);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseSendEnabled };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denom = reader.string();
                    break;
                case 2:
                    message.enabled = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseSendEnabled };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        if (object.enabled !== undefined && object.enabled !== null) {
            message.enabled = Boolean(object.enabled);
        }
        else {
            message.enabled = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.denom !== undefined && (obj.denom = message.denom);
        message.enabled !== undefined && (obj.enabled = message.enabled);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseSendEnabled };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        if (object.enabled !== undefined && object.enabled !== null) {
            message.enabled = object.enabled;
        }
        else {
            message.enabled = false;
        }
        return message;
    },
};
const baseInput = { address: "" };
export const Input = {
    encode(message, writer = Writer.create()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.coins) {
            Coin.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseInput };
        message.coins = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.address = reader.string();
                    break;
                case 2:
                    message.coins.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseInput };
        message.coins = [];
        if (object.address !== undefined && object.address !== null) {
            message.address = String(object.address);
        }
        else {
            message.address = "";
        }
        if (object.coins !== undefined && object.coins !== null) {
            for (const e of object.coins) {
                message.coins.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.address !== undefined && (obj.address = message.address);
        if (message.coins) {
            obj.coins = message.coins.map((e) => (e ? Coin.toJSON(e) : undefined));
        }
        else {
            obj.coins = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseInput };
        message.coins = [];
        if (object.address !== undefined && object.address !== null) {
            message.address = object.address;
        }
        else {
            message.address = "";
        }
        if (object.coins !== undefined && object.coins !== null) {
            for (const e of object.coins) {
                message.coins.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseOutput = { address: "" };
export const Output = {
    encode(message, writer = Writer.create()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.coins) {
            Coin.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseOutput };
        message.coins = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.address = reader.string();
                    break;
                case 2:
                    message.coins.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseOutput };
        message.coins = [];
        if (object.address !== undefined && object.address !== null) {
            message.address = String(object.address);
        }
        else {
            message.address = "";
        }
        if (object.coins !== undefined && object.coins !== null) {
            for (const e of object.coins) {
                message.coins.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.address !== undefined && (obj.address = message.address);
        if (message.coins) {
            obj.coins = message.coins.map((e) => (e ? Coin.toJSON(e) : undefined));
        }
        else {
            obj.coins = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseOutput };
        message.coins = [];
        if (object.address !== undefined && object.address !== null) {
            message.address = object.address;
        }
        else {
            message.address = "";
        }
        if (object.coins !== undefined && object.coins !== null) {
            for (const e of object.coins) {
                message.coins.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseSupply = {};
export const Supply = {
    encode(message, writer = Writer.create()) {
        for (const v of message.total) {
            Coin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseSupply };
        message.total = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.total.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseSupply };
        message.total = [];
        if (object.total !== undefined && object.total !== null) {
            for (const e of object.total) {
                message.total.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.total) {
            obj.total = message.total.map((e) => (e ? Coin.toJSON(e) : undefined));
        }
        else {
            obj.total = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseSupply };
        message.total = [];
        if (object.total !== undefined && object.total !== null) {
            for (const e of object.total) {
                message.total.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseDenomUnit = { denom: "", exponent: 0, aliases: "" };
export const DenomUnit = {
    encode(message, writer = Writer.create()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.exponent !== 0) {
            writer.uint32(16).uint32(message.exponent);
        }
        for (const v of message.aliases) {
            writer.uint32(26).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseDenomUnit };
        message.aliases = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denom = reader.string();
                    break;
                case 2:
                    message.exponent = reader.uint32();
                    break;
                case 3:
                    message.aliases.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseDenomUnit };
        message.aliases = [];
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        if (object.exponent !== undefined && object.exponent !== null) {
            message.exponent = Number(object.exponent);
        }
        else {
            message.exponent = 0;
        }
        if (object.aliases !== undefined && object.aliases !== null) {
            for (const e of object.aliases) {
                message.aliases.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.denom !== undefined && (obj.denom = message.denom);
        message.exponent !== undefined && (obj.exponent = message.exponent);
        if (message.aliases) {
            obj.aliases = message.aliases.map((e) => e);
        }
        else {
            obj.aliases = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseDenomUnit };
        message.aliases = [];
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        if (object.exponent !== undefined && object.exponent !== null) {
            message.exponent = object.exponent;
        }
        else {
            message.exponent = 0;
        }
        if (object.aliases !== undefined && object.aliases !== null) {
            for (const e of object.aliases) {
                message.aliases.push(e);
            }
        }
        return message;
    },
};
const baseMetadata = {
    description: "",
    base: "",
    display: "",
    name: "",
    symbol: "",
};
export const Metadata = {
    encode(message, writer = Writer.create()) {
        if (message.description !== "") {
            writer.uint32(10).string(message.description);
        }
        for (const v of message.denomUnits) {
            DenomUnit.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.base !== "") {
            writer.uint32(26).string(message.base);
        }
        if (message.display !== "") {
            writer.uint32(34).string(message.display);
        }
        if (message.name !== "") {
            writer.uint32(42).string(message.name);
        }
        if (message.symbol !== "") {
            writer.uint32(50).string(message.symbol);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMetadata };
        message.denomUnits = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.description = reader.string();
                    break;
                case 2:
                    message.denomUnits.push(DenomUnit.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.base = reader.string();
                    break;
                case 4:
                    message.display = reader.string();
                    break;
                case 5:
                    message.name = reader.string();
                    break;
                case 6:
                    message.symbol = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMetadata };
        message.denomUnits = [];
        if (object.description !== undefined && object.description !== null) {
            message.description = String(object.description);
        }
        else {
            message.description = "";
        }
        if (object.denomUnits !== undefined && object.denomUnits !== null) {
            for (const e of object.denomUnits) {
                message.denomUnits.push(DenomUnit.fromJSON(e));
            }
        }
        if (object.base !== undefined && object.base !== null) {
            message.base = String(object.base);
        }
        else {
            message.base = "";
        }
        if (object.display !== undefined && object.display !== null) {
            message.display = String(object.display);
        }
        else {
            message.display = "";
        }
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.symbol !== undefined && object.symbol !== null) {
            message.symbol = String(object.symbol);
        }
        else {
            message.symbol = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.description !== undefined &&
            (obj.description = message.description);
        if (message.denomUnits) {
            obj.denomUnits = message.denomUnits.map((e) => e ? DenomUnit.toJSON(e) : undefined);
        }
        else {
            obj.denomUnits = [];
        }
        message.base !== undefined && (obj.base = message.base);
        message.display !== undefined && (obj.display = message.display);
        message.name !== undefined && (obj.name = message.name);
        message.symbol !== undefined && (obj.symbol = message.symbol);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMetadata };
        message.denomUnits = [];
        if (object.description !== undefined && object.description !== null) {
            message.description = object.description;
        }
        else {
            message.description = "";
        }
        if (object.denomUnits !== undefined && object.denomUnits !== null) {
            for (const e of object.denomUnits) {
                message.denomUnits.push(DenomUnit.fromPartial(e));
            }
        }
        if (object.base !== undefined && object.base !== null) {
            message.base = object.base;
        }
        else {
            message.base = "";
        }
        if (object.display !== undefined && object.display !== null) {
            message.display = object.display;
        }
        else {
            message.display = "";
        }
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.symbol !== undefined && object.symbol !== null) {
            message.symbol = object.symbol;
        }
        else {
            message.symbol = "";
        }
        return message;
    },
};
