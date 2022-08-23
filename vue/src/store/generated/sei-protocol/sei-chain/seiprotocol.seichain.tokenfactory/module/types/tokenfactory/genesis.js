/* eslint-disable */
import { Params } from "../tokenfactory/params";
import { DenomAuthorityMetadata } from "../tokenfactory/authorityMetadata";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.tokenfactory";
const baseGenesisState = {};
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).ldelim();
        }
        for (const v of message.factoryDenoms) {
            GenesisDenom.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.factoryDenoms = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.params = Params.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.factoryDenoms.push(GenesisDenom.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGenesisState };
        message.factoryDenoms = [];
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromJSON(object.params);
        }
        else {
            message.params = undefined;
        }
        if (object.factoryDenoms !== undefined && object.factoryDenoms !== null) {
            for (const e of object.factoryDenoms) {
                message.factoryDenoms.push(GenesisDenom.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.params !== undefined &&
            (obj.params = message.params ? Params.toJSON(message.params) : undefined);
        if (message.factoryDenoms) {
            obj.factoryDenoms = message.factoryDenoms.map((e) => e ? GenesisDenom.toJSON(e) : undefined);
        }
        else {
            obj.factoryDenoms = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.factoryDenoms = [];
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromPartial(object.params);
        }
        else {
            message.params = undefined;
        }
        if (object.factoryDenoms !== undefined && object.factoryDenoms !== null) {
            for (const e of object.factoryDenoms) {
                message.factoryDenoms.push(GenesisDenom.fromPartial(e));
            }
        }
        return message;
    },
};
const baseGenesisDenom = { denom: "" };
export const GenesisDenom = {
    encode(message, writer = Writer.create()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.authorityMetadata !== undefined) {
            DenomAuthorityMetadata.encode(message.authorityMetadata, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisDenom };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denom = reader.string();
                    break;
                case 2:
                    message.authorityMetadata = DenomAuthorityMetadata.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGenesisDenom };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        if (object.authorityMetadata !== undefined &&
            object.authorityMetadata !== null) {
            message.authorityMetadata = DenomAuthorityMetadata.fromJSON(object.authorityMetadata);
        }
        else {
            message.authorityMetadata = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.denom !== undefined && (obj.denom = message.denom);
        message.authorityMetadata !== undefined &&
            (obj.authorityMetadata = message.authorityMetadata
                ? DenomAuthorityMetadata.toJSON(message.authorityMetadata)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisDenom };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        if (object.authorityMetadata !== undefined &&
            object.authorityMetadata !== null) {
            message.authorityMetadata = DenomAuthorityMetadata.fromPartial(object.authorityMetadata);
        }
        else {
            message.authorityMetadata = undefined;
        }
        return message;
    },
};
