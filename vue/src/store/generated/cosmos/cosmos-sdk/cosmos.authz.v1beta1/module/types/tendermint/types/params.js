/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Duration } from "../../google/protobuf/duration";
export const protobufPackage = "tendermint.types";
const baseConsensusParams = {};
export const ConsensusParams = {
    encode(message, writer = Writer.create()) {
        if (message.block !== undefined) {
            BlockParams.encode(message.block, writer.uint32(10).fork()).ldelim();
        }
        if (message.evidence !== undefined) {
            EvidenceParams.encode(message.evidence, writer.uint32(18).fork()).ldelim();
        }
        if (message.validator !== undefined) {
            ValidatorParams.encode(message.validator, writer.uint32(26).fork()).ldelim();
        }
        if (message.version !== undefined) {
            VersionParams.encode(message.version, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseConsensusParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.block = BlockParams.decode(reader, reader.uint32());
                    break;
                case 2:
                    message.evidence = EvidenceParams.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.validator = ValidatorParams.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.version = VersionParams.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseConsensusParams };
        if (object.block !== undefined && object.block !== null) {
            message.block = BlockParams.fromJSON(object.block);
        }
        else {
            message.block = undefined;
        }
        if (object.evidence !== undefined && object.evidence !== null) {
            message.evidence = EvidenceParams.fromJSON(object.evidence);
        }
        else {
            message.evidence = undefined;
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = ValidatorParams.fromJSON(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.version !== undefined && object.version !== null) {
            message.version = VersionParams.fromJSON(object.version);
        }
        else {
            message.version = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.block !== undefined &&
            (obj.block = message.block
                ? BlockParams.toJSON(message.block)
                : undefined);
        message.evidence !== undefined &&
            (obj.evidence = message.evidence
                ? EvidenceParams.toJSON(message.evidence)
                : undefined);
        message.validator !== undefined &&
            (obj.validator = message.validator
                ? ValidatorParams.toJSON(message.validator)
                : undefined);
        message.version !== undefined &&
            (obj.version = message.version
                ? VersionParams.toJSON(message.version)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseConsensusParams };
        if (object.block !== undefined && object.block !== null) {
            message.block = BlockParams.fromPartial(object.block);
        }
        else {
            message.block = undefined;
        }
        if (object.evidence !== undefined && object.evidence !== null) {
            message.evidence = EvidenceParams.fromPartial(object.evidence);
        }
        else {
            message.evidence = undefined;
        }
        if (object.validator !== undefined && object.validator !== null) {
            message.validator = ValidatorParams.fromPartial(object.validator);
        }
        else {
            message.validator = undefined;
        }
        if (object.version !== undefined && object.version !== null) {
            message.version = VersionParams.fromPartial(object.version);
        }
        else {
            message.version = undefined;
        }
        return message;
    },
};
const baseBlockParams = { maxBytes: 0, maxGas: 0, timeIotaMs: 0 };
export const BlockParams = {
    encode(message, writer = Writer.create()) {
        if (message.maxBytes !== 0) {
            writer.uint32(8).int64(message.maxBytes);
        }
        if (message.maxGas !== 0) {
            writer.uint32(16).int64(message.maxGas);
        }
        if (message.timeIotaMs !== 0) {
            writer.uint32(24).int64(message.timeIotaMs);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseBlockParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.maxBytes = longToNumber(reader.int64());
                    break;
                case 2:
                    message.maxGas = longToNumber(reader.int64());
                    break;
                case 3:
                    message.timeIotaMs = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseBlockParams };
        if (object.maxBytes !== undefined && object.maxBytes !== null) {
            message.maxBytes = Number(object.maxBytes);
        }
        else {
            message.maxBytes = 0;
        }
        if (object.maxGas !== undefined && object.maxGas !== null) {
            message.maxGas = Number(object.maxGas);
        }
        else {
            message.maxGas = 0;
        }
        if (object.timeIotaMs !== undefined && object.timeIotaMs !== null) {
            message.timeIotaMs = Number(object.timeIotaMs);
        }
        else {
            message.timeIotaMs = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.maxBytes !== undefined && (obj.maxBytes = message.maxBytes);
        message.maxGas !== undefined && (obj.maxGas = message.maxGas);
        message.timeIotaMs !== undefined && (obj.timeIotaMs = message.timeIotaMs);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseBlockParams };
        if (object.maxBytes !== undefined && object.maxBytes !== null) {
            message.maxBytes = object.maxBytes;
        }
        else {
            message.maxBytes = 0;
        }
        if (object.maxGas !== undefined && object.maxGas !== null) {
            message.maxGas = object.maxGas;
        }
        else {
            message.maxGas = 0;
        }
        if (object.timeIotaMs !== undefined && object.timeIotaMs !== null) {
            message.timeIotaMs = object.timeIotaMs;
        }
        else {
            message.timeIotaMs = 0;
        }
        return message;
    },
};
const baseEvidenceParams = { maxAgeNumBlocks: 0, maxBytes: 0 };
export const EvidenceParams = {
    encode(message, writer = Writer.create()) {
        if (message.maxAgeNumBlocks !== 0) {
            writer.uint32(8).int64(message.maxAgeNumBlocks);
        }
        if (message.maxAgeDuration !== undefined) {
            Duration.encode(message.maxAgeDuration, writer.uint32(18).fork()).ldelim();
        }
        if (message.maxBytes !== 0) {
            writer.uint32(24).int64(message.maxBytes);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEvidenceParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.maxAgeNumBlocks = longToNumber(reader.int64());
                    break;
                case 2:
                    message.maxAgeDuration = Duration.decode(reader, reader.uint32());
                    break;
                case 3:
                    message.maxBytes = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEvidenceParams };
        if (object.maxAgeNumBlocks !== undefined &&
            object.maxAgeNumBlocks !== null) {
            message.maxAgeNumBlocks = Number(object.maxAgeNumBlocks);
        }
        else {
            message.maxAgeNumBlocks = 0;
        }
        if (object.maxAgeDuration !== undefined && object.maxAgeDuration !== null) {
            message.maxAgeDuration = Duration.fromJSON(object.maxAgeDuration);
        }
        else {
            message.maxAgeDuration = undefined;
        }
        if (object.maxBytes !== undefined && object.maxBytes !== null) {
            message.maxBytes = Number(object.maxBytes);
        }
        else {
            message.maxBytes = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.maxAgeNumBlocks !== undefined &&
            (obj.maxAgeNumBlocks = message.maxAgeNumBlocks);
        message.maxAgeDuration !== undefined &&
            (obj.maxAgeDuration = message.maxAgeDuration
                ? Duration.toJSON(message.maxAgeDuration)
                : undefined);
        message.maxBytes !== undefined && (obj.maxBytes = message.maxBytes);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEvidenceParams };
        if (object.maxAgeNumBlocks !== undefined &&
            object.maxAgeNumBlocks !== null) {
            message.maxAgeNumBlocks = object.maxAgeNumBlocks;
        }
        else {
            message.maxAgeNumBlocks = 0;
        }
        if (object.maxAgeDuration !== undefined && object.maxAgeDuration !== null) {
            message.maxAgeDuration = Duration.fromPartial(object.maxAgeDuration);
        }
        else {
            message.maxAgeDuration = undefined;
        }
        if (object.maxBytes !== undefined && object.maxBytes !== null) {
            message.maxBytes = object.maxBytes;
        }
        else {
            message.maxBytes = 0;
        }
        return message;
    },
};
const baseValidatorParams = { pubKeyTypes: "" };
export const ValidatorParams = {
    encode(message, writer = Writer.create()) {
        for (const v of message.pubKeyTypes) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseValidatorParams };
        message.pubKeyTypes = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.pubKeyTypes.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseValidatorParams };
        message.pubKeyTypes = [];
        if (object.pubKeyTypes !== undefined && object.pubKeyTypes !== null) {
            for (const e of object.pubKeyTypes) {
                message.pubKeyTypes.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.pubKeyTypes) {
            obj.pubKeyTypes = message.pubKeyTypes.map((e) => e);
        }
        else {
            obj.pubKeyTypes = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseValidatorParams };
        message.pubKeyTypes = [];
        if (object.pubKeyTypes !== undefined && object.pubKeyTypes !== null) {
            for (const e of object.pubKeyTypes) {
                message.pubKeyTypes.push(e);
            }
        }
        return message;
    },
};
const baseVersionParams = { appVersion: 0 };
export const VersionParams = {
    encode(message, writer = Writer.create()) {
        if (message.appVersion !== 0) {
            writer.uint32(8).uint64(message.appVersion);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseVersionParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.appVersion = longToNumber(reader.uint64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseVersionParams };
        if (object.appVersion !== undefined && object.appVersion !== null) {
            message.appVersion = Number(object.appVersion);
        }
        else {
            message.appVersion = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.appVersion !== undefined && (obj.appVersion = message.appVersion);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseVersionParams };
        if (object.appVersion !== undefined && object.appVersion !== null) {
            message.appVersion = object.appVersion;
        }
        else {
            message.appVersion = 0;
        }
        return message;
    },
};
const baseHashedParams = { blockMaxBytes: 0, blockMaxGas: 0 };
export const HashedParams = {
    encode(message, writer = Writer.create()) {
        if (message.blockMaxBytes !== 0) {
            writer.uint32(8).int64(message.blockMaxBytes);
        }
        if (message.blockMaxGas !== 0) {
            writer.uint32(16).int64(message.blockMaxGas);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseHashedParams };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.blockMaxBytes = longToNumber(reader.int64());
                    break;
                case 2:
                    message.blockMaxGas = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseHashedParams };
        if (object.blockMaxBytes !== undefined && object.blockMaxBytes !== null) {
            message.blockMaxBytes = Number(object.blockMaxBytes);
        }
        else {
            message.blockMaxBytes = 0;
        }
        if (object.blockMaxGas !== undefined && object.blockMaxGas !== null) {
            message.blockMaxGas = Number(object.blockMaxGas);
        }
        else {
            message.blockMaxGas = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.blockMaxBytes !== undefined &&
            (obj.blockMaxBytes = message.blockMaxBytes);
        message.blockMaxGas !== undefined &&
            (obj.blockMaxGas = message.blockMaxGas);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseHashedParams };
        if (object.blockMaxBytes !== undefined && object.blockMaxBytes !== null) {
            message.blockMaxBytes = object.blockMaxBytes;
        }
        else {
            message.blockMaxBytes = 0;
        }
        if (object.blockMaxGas !== undefined && object.blockMaxGas !== null) {
            message.blockMaxGas = object.blockMaxGas;
        }
        else {
            message.blockMaxGas = 0;
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
