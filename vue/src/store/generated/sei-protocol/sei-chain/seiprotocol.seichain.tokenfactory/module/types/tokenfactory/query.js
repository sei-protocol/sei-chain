/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../tokenfactory/params";
import { DenomAuthorityMetadata } from "../tokenfactory/authorityMetadata";
export const protobufPackage = "seiprotocol.seichain.tokenfactory";
const baseQueryParamsRequest = {};
export const QueryParamsRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryParamsRequest };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(_) {
        const message = { ...baseQueryParamsRequest };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseQueryParamsRequest };
        return message;
    },
};
const baseQueryParamsResponse = {};
export const QueryParamsResponse = {
    encode(message, writer = Writer.create()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseQueryParamsResponse };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.params = Params.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseQueryParamsResponse };
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromJSON(object.params);
        }
        else {
            message.params = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.params !== undefined &&
            (obj.params = message.params ? Params.toJSON(message.params) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseQueryParamsResponse };
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromPartial(object.params);
        }
        else {
            message.params = undefined;
        }
        return message;
    },
};
const baseQueryDenomAuthorityMetadataRequest = { denom: "" };
export const QueryDenomAuthorityMetadataRequest = {
    encode(message, writer = Writer.create()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDenomAuthorityMetadataRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denom = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryDenomAuthorityMetadataRequest,
        };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.denom !== undefined && (obj.denom = message.denom);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDenomAuthorityMetadataRequest,
        };
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        return message;
    },
};
const baseQueryDenomAuthorityMetadataResponse = {};
export const QueryDenomAuthorityMetadataResponse = {
    encode(message, writer = Writer.create()) {
        if (message.authorityMetadata !== undefined) {
            DenomAuthorityMetadata.encode(message.authorityMetadata, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDenomAuthorityMetadataResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
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
        const message = {
            ...baseQueryDenomAuthorityMetadataResponse,
        };
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
        message.authorityMetadata !== undefined &&
            (obj.authorityMetadata = message.authorityMetadata
                ? DenomAuthorityMetadata.toJSON(message.authorityMetadata)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDenomAuthorityMetadataResponse,
        };
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
const baseQueryDenomsFromCreatorRequest = { creator: "" };
export const QueryDenomsFromCreatorRequest = {
    encode(message, writer = Writer.create()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDenomsFromCreatorRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.creator = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryDenomsFromCreatorRequest,
        };
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = String(object.creator);
        }
        else {
            message.creator = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.creator !== undefined && (obj.creator = message.creator);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDenomsFromCreatorRequest,
        };
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = object.creator;
        }
        else {
            message.creator = "";
        }
        return message;
    },
};
const baseQueryDenomsFromCreatorResponse = { denoms: "" };
export const QueryDenomsFromCreatorResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.denoms) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDenomsFromCreatorResponse,
        };
        message.denoms = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denoms.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryDenomsFromCreatorResponse,
        };
        message.denoms = [];
        if (object.denoms !== undefined && object.denoms !== null) {
            for (const e of object.denoms) {
                message.denoms.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.denoms) {
            obj.denoms = message.denoms.map((e) => e);
        }
        else {
            obj.denoms = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDenomsFromCreatorResponse,
        };
        message.denoms = [];
        if (object.denoms !== undefined && object.denoms !== null) {
            for (const e of object.denoms) {
                message.denoms.push(e);
            }
        }
        return message;
    },
};
const baseQueryDenomCreationFeeWhitelistRequest = {};
export const QueryDenomCreationFeeWhitelistRequest = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDenomCreationFeeWhitelistRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(_) {
        const message = {
            ...baseQueryDenomCreationFeeWhitelistRequest,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseQueryDenomCreationFeeWhitelistRequest,
        };
        return message;
    },
};
const baseQueryDenomCreationFeeWhitelistResponse = { creators: "" };
export const QueryDenomCreationFeeWhitelistResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.creators) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryDenomCreationFeeWhitelistResponse,
        };
        message.creators = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.creators.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryDenomCreationFeeWhitelistResponse,
        };
        message.creators = [];
        if (object.creators !== undefined && object.creators !== null) {
            for (const e of object.creators) {
                message.creators.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.creators) {
            obj.creators = message.creators.map((e) => e);
        }
        else {
            obj.creators = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryDenomCreationFeeWhitelistResponse,
        };
        message.creators = [];
        if (object.creators !== undefined && object.creators !== null) {
            for (const e of object.creators) {
                message.creators.push(e);
            }
        }
        return message;
    },
};
const baseQueryCreatorInDenomFeeWhitelistRequest = { creator: "" };
export const QueryCreatorInDenomFeeWhitelistRequest = {
    encode(message, writer = Writer.create()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryCreatorInDenomFeeWhitelistRequest,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.creator = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryCreatorInDenomFeeWhitelistRequest,
        };
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = String(object.creator);
        }
        else {
            message.creator = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.creator !== undefined && (obj.creator = message.creator);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryCreatorInDenomFeeWhitelistRequest,
        };
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = object.creator;
        }
        else {
            message.creator = "";
        }
        return message;
    },
};
const baseQueryCreatorInDenomFeeWhitelistResponse = {
    whitelisted: false,
};
export const QueryCreatorInDenomFeeWhitelistResponse = {
    encode(message, writer = Writer.create()) {
        if (message.whitelisted === true) {
            writer.uint32(8).bool(message.whitelisted);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseQueryCreatorInDenomFeeWhitelistResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.whitelisted = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseQueryCreatorInDenomFeeWhitelistResponse,
        };
        if (object.whitelisted !== undefined && object.whitelisted !== null) {
            message.whitelisted = Boolean(object.whitelisted);
        }
        else {
            message.whitelisted = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.whitelisted !== undefined &&
            (obj.whitelisted = message.whitelisted);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseQueryCreatorInDenomFeeWhitelistResponse,
        };
        if (object.whitelisted !== undefined && object.whitelisted !== null) {
            message.whitelisted = object.whitelisted;
        }
        else {
            message.whitelisted = false;
        }
        return message;
    },
};
export class QueryClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    Params(request) {
        const data = QueryParamsRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Query", "Params", data);
        return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
    }
    DenomAuthorityMetadata(request) {
        const data = QueryDenomAuthorityMetadataRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Query", "DenomAuthorityMetadata", data);
        return promise.then((data) => QueryDenomAuthorityMetadataResponse.decode(new Reader(data)));
    }
    DenomsFromCreator(request) {
        const data = QueryDenomsFromCreatorRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Query", "DenomsFromCreator", data);
        return promise.then((data) => QueryDenomsFromCreatorResponse.decode(new Reader(data)));
    }
    DenomCreationFeeWhitelist(request) {
        const data = QueryDenomCreationFeeWhitelistRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Query", "DenomCreationFeeWhitelist", data);
        return promise.then((data) => QueryDenomCreationFeeWhitelistResponse.decode(new Reader(data)));
    }
    CreatorInDenomFeeWhitelist(request) {
        const data = QueryCreatorInDenomFeeWhitelistRequest.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Query", "CreatorInDenomFeeWhitelist", data);
        return promise.then((data) => QueryCreatorInDenomFeeWhitelistResponse.decode(new Reader(data)));
    }
}
