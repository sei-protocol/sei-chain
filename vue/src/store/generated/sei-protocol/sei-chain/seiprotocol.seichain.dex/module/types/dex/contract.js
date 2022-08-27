/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.dex";
const baseContractInfo = {
    codeId: 0,
    contractAddr: "",
    needHook: false,
    needOrderMatching: false,
    numIncomingDependencies: 0,
};
export const ContractInfo = {
    encode(message, writer = Writer.create()) {
        if (message.codeId !== 0) {
            writer.uint32(8).uint64(message.codeId);
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.needHook === true) {
            writer.uint32(24).bool(message.needHook);
        }
        if (message.needOrderMatching === true) {
            writer.uint32(32).bool(message.needOrderMatching);
        }
        for (const v of message.dependencies) {
            ContractDependencyInfo.encode(v, writer.uint32(42).fork()).ldelim();
        }
        if (message.numIncomingDependencies !== 0) {
            writer.uint32(48).int64(message.numIncomingDependencies);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseContractInfo };
        message.dependencies = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.codeId = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.contractAddr = reader.string();
                    break;
                case 3:
                    message.needHook = reader.bool();
                    break;
                case 4:
                    message.needOrderMatching = reader.bool();
                    break;
                case 5:
                    message.dependencies.push(ContractDependencyInfo.decode(reader, reader.uint32()));
                    break;
                case 6:
                    message.numIncomingDependencies = longToNumber(reader.int64());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseContractInfo };
        message.dependencies = [];
        if (object.codeId !== undefined && object.codeId !== null) {
            message.codeId = Number(object.codeId);
        }
        else {
            message.codeId = 0;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.needHook !== undefined && object.needHook !== null) {
            message.needHook = Boolean(object.needHook);
        }
        else {
            message.needHook = false;
        }
        if (object.needOrderMatching !== undefined &&
            object.needOrderMatching !== null) {
            message.needOrderMatching = Boolean(object.needOrderMatching);
        }
        else {
            message.needOrderMatching = false;
        }
        if (object.dependencies !== undefined && object.dependencies !== null) {
            for (const e of object.dependencies) {
                message.dependencies.push(ContractDependencyInfo.fromJSON(e));
            }
        }
        if (object.numIncomingDependencies !== undefined &&
            object.numIncomingDependencies !== null) {
            message.numIncomingDependencies = Number(object.numIncomingDependencies);
        }
        else {
            message.numIncomingDependencies = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.codeId !== undefined && (obj.codeId = message.codeId);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.needHook !== undefined && (obj.needHook = message.needHook);
        message.needOrderMatching !== undefined &&
            (obj.needOrderMatching = message.needOrderMatching);
        if (message.dependencies) {
            obj.dependencies = message.dependencies.map((e) => e ? ContractDependencyInfo.toJSON(e) : undefined);
        }
        else {
            obj.dependencies = [];
        }
        message.numIncomingDependencies !== undefined &&
            (obj.numIncomingDependencies = message.numIncomingDependencies);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseContractInfo };
        message.dependencies = [];
        if (object.codeId !== undefined && object.codeId !== null) {
            message.codeId = object.codeId;
        }
        else {
            message.codeId = 0;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.needHook !== undefined && object.needHook !== null) {
            message.needHook = object.needHook;
        }
        else {
            message.needHook = false;
        }
        if (object.needOrderMatching !== undefined &&
            object.needOrderMatching !== null) {
            message.needOrderMatching = object.needOrderMatching;
        }
        else {
            message.needOrderMatching = false;
        }
        if (object.dependencies !== undefined && object.dependencies !== null) {
            for (const e of object.dependencies) {
                message.dependencies.push(ContractDependencyInfo.fromPartial(e));
            }
        }
        if (object.numIncomingDependencies !== undefined &&
            object.numIncomingDependencies !== null) {
            message.numIncomingDependencies = object.numIncomingDependencies;
        }
        else {
            message.numIncomingDependencies = 0;
        }
        return message;
    },
};
const baseContractDependencyInfo = {
    dependency: "",
    immediateElderSibling: "",
    immediateYoungerSibling: "",
};
export const ContractDependencyInfo = {
    encode(message, writer = Writer.create()) {
        if (message.dependency !== "") {
            writer.uint32(10).string(message.dependency);
        }
        if (message.immediateElderSibling !== "") {
            writer.uint32(18).string(message.immediateElderSibling);
        }
        if (message.immediateYoungerSibling !== "") {
            writer.uint32(26).string(message.immediateYoungerSibling);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseContractDependencyInfo };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.dependency = reader.string();
                    break;
                case 2:
                    message.immediateElderSibling = reader.string();
                    break;
                case 3:
                    message.immediateYoungerSibling = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseContractDependencyInfo };
        if (object.dependency !== undefined && object.dependency !== null) {
            message.dependency = String(object.dependency);
        }
        else {
            message.dependency = "";
        }
        if (object.immediateElderSibling !== undefined &&
            object.immediateElderSibling !== null) {
            message.immediateElderSibling = String(object.immediateElderSibling);
        }
        else {
            message.immediateElderSibling = "";
        }
        if (object.immediateYoungerSibling !== undefined &&
            object.immediateYoungerSibling !== null) {
            message.immediateYoungerSibling = String(object.immediateYoungerSibling);
        }
        else {
            message.immediateYoungerSibling = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.dependency !== undefined && (obj.dependency = message.dependency);
        message.immediateElderSibling !== undefined &&
            (obj.immediateElderSibling = message.immediateElderSibling);
        message.immediateYoungerSibling !== undefined &&
            (obj.immediateYoungerSibling = message.immediateYoungerSibling);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseContractDependencyInfo };
        if (object.dependency !== undefined && object.dependency !== null) {
            message.dependency = object.dependency;
        }
        else {
            message.dependency = "";
        }
        if (object.immediateElderSibling !== undefined &&
            object.immediateElderSibling !== null) {
            message.immediateElderSibling = object.immediateElderSibling;
        }
        else {
            message.immediateElderSibling = "";
        }
        if (object.immediateYoungerSibling !== undefined &&
            object.immediateYoungerSibling !== null) {
            message.immediateYoungerSibling = object.immediateYoungerSibling;
        }
        else {
            message.immediateYoungerSibling = "";
        }
        return message;
    },
};
const baseLegacyContractInfo = {
    codeId: 0,
    contractAddr: "",
    needHook: false,
    needOrderMatching: false,
    dependentContractAddrs: "",
};
export const LegacyContractInfo = {
    encode(message, writer = Writer.create()) {
        if (message.codeId !== 0) {
            writer.uint32(8).uint64(message.codeId);
        }
        if (message.contractAddr !== "") {
            writer.uint32(18).string(message.contractAddr);
        }
        if (message.needHook === true) {
            writer.uint32(24).bool(message.needHook);
        }
        if (message.needOrderMatching === true) {
            writer.uint32(32).bool(message.needOrderMatching);
        }
        for (const v of message.dependentContractAddrs) {
            writer.uint32(42).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseLegacyContractInfo };
        message.dependentContractAddrs = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.codeId = longToNumber(reader.uint64());
                    break;
                case 2:
                    message.contractAddr = reader.string();
                    break;
                case 3:
                    message.needHook = reader.bool();
                    break;
                case 4:
                    message.needOrderMatching = reader.bool();
                    break;
                case 5:
                    message.dependentContractAddrs.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseLegacyContractInfo };
        message.dependentContractAddrs = [];
        if (object.codeId !== undefined && object.codeId !== null) {
            message.codeId = Number(object.codeId);
        }
        else {
            message.codeId = 0;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.needHook !== undefined && object.needHook !== null) {
            message.needHook = Boolean(object.needHook);
        }
        else {
            message.needHook = false;
        }
        if (object.needOrderMatching !== undefined &&
            object.needOrderMatching !== null) {
            message.needOrderMatching = Boolean(object.needOrderMatching);
        }
        else {
            message.needOrderMatching = false;
        }
        if (object.dependentContractAddrs !== undefined &&
            object.dependentContractAddrs !== null) {
            for (const e of object.dependentContractAddrs) {
                message.dependentContractAddrs.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.codeId !== undefined && (obj.codeId = message.codeId);
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        message.needHook !== undefined && (obj.needHook = message.needHook);
        message.needOrderMatching !== undefined &&
            (obj.needOrderMatching = message.needOrderMatching);
        if (message.dependentContractAddrs) {
            obj.dependentContractAddrs = message.dependentContractAddrs.map((e) => e);
        }
        else {
            obj.dependentContractAddrs = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseLegacyContractInfo };
        message.dependentContractAddrs = [];
        if (object.codeId !== undefined && object.codeId !== null) {
            message.codeId = object.codeId;
        }
        else {
            message.codeId = 0;
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.needHook !== undefined && object.needHook !== null) {
            message.needHook = object.needHook;
        }
        else {
            message.needHook = false;
        }
        if (object.needOrderMatching !== undefined &&
            object.needOrderMatching !== null) {
            message.needOrderMatching = object.needOrderMatching;
        }
        else {
            message.needOrderMatching = false;
        }
        if (object.dependentContractAddrs !== undefined &&
            object.dependentContractAddrs !== null) {
            for (const e of object.dependentContractAddrs) {
                message.dependentContractAddrs.push(e);
            }
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
