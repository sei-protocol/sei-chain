/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "tendermint.crypto";
const baseProof = { total: 0, index: 0 };
export const Proof = {
    encode(message, writer = Writer.create()) {
        if (message.total !== 0) {
            writer.uint32(8).int64(message.total);
        }
        if (message.index !== 0) {
            writer.uint32(16).int64(message.index);
        }
        if (message.leafHash.length !== 0) {
            writer.uint32(26).bytes(message.leafHash);
        }
        for (const v of message.aunts) {
            writer.uint32(34).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseProof };
        message.aunts = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.total = longToNumber(reader.int64());
                    break;
                case 2:
                    message.index = longToNumber(reader.int64());
                    break;
                case 3:
                    message.leafHash = reader.bytes();
                    break;
                case 4:
                    message.aunts.push(reader.bytes());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseProof };
        message.aunts = [];
        if (object.total !== undefined && object.total !== null) {
            message.total = Number(object.total);
        }
        else {
            message.total = 0;
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = Number(object.index);
        }
        else {
            message.index = 0;
        }
        if (object.leafHash !== undefined && object.leafHash !== null) {
            message.leafHash = bytesFromBase64(object.leafHash);
        }
        if (object.aunts !== undefined && object.aunts !== null) {
            for (const e of object.aunts) {
                message.aunts.push(bytesFromBase64(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.total !== undefined && (obj.total = message.total);
        message.index !== undefined && (obj.index = message.index);
        message.leafHash !== undefined &&
            (obj.leafHash = base64FromBytes(message.leafHash !== undefined ? message.leafHash : new Uint8Array()));
        if (message.aunts) {
            obj.aunts = message.aunts.map((e) => base64FromBytes(e !== undefined ? e : new Uint8Array()));
        }
        else {
            obj.aunts = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseProof };
        message.aunts = [];
        if (object.total !== undefined && object.total !== null) {
            message.total = object.total;
        }
        else {
            message.total = 0;
        }
        if (object.index !== undefined && object.index !== null) {
            message.index = object.index;
        }
        else {
            message.index = 0;
        }
        if (object.leafHash !== undefined && object.leafHash !== null) {
            message.leafHash = object.leafHash;
        }
        else {
            message.leafHash = new Uint8Array();
        }
        if (object.aunts !== undefined && object.aunts !== null) {
            for (const e of object.aunts) {
                message.aunts.push(e);
            }
        }
        return message;
    },
};
const baseValueOp = {};
export const ValueOp = {
    encode(message, writer = Writer.create()) {
        if (message.key.length !== 0) {
            writer.uint32(10).bytes(message.key);
        }
        if (message.proof !== undefined) {
            Proof.encode(message.proof, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseValueOp };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.key = reader.bytes();
                    break;
                case 2:
                    message.proof = Proof.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseValueOp };
        if (object.key !== undefined && object.key !== null) {
            message.key = bytesFromBase64(object.key);
        }
        if (object.proof !== undefined && object.proof !== null) {
            message.proof = Proof.fromJSON(object.proof);
        }
        else {
            message.proof = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.key !== undefined &&
            (obj.key = base64FromBytes(message.key !== undefined ? message.key : new Uint8Array()));
        message.proof !== undefined &&
            (obj.proof = message.proof ? Proof.toJSON(message.proof) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseValueOp };
        if (object.key !== undefined && object.key !== null) {
            message.key = object.key;
        }
        else {
            message.key = new Uint8Array();
        }
        if (object.proof !== undefined && object.proof !== null) {
            message.proof = Proof.fromPartial(object.proof);
        }
        else {
            message.proof = undefined;
        }
        return message;
    },
};
const baseDominoOp = { key: "", input: "", output: "" };
export const DominoOp = {
    encode(message, writer = Writer.create()) {
        if (message.key !== "") {
            writer.uint32(10).string(message.key);
        }
        if (message.input !== "") {
            writer.uint32(18).string(message.input);
        }
        if (message.output !== "") {
            writer.uint32(26).string(message.output);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseDominoOp };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.key = reader.string();
                    break;
                case 2:
                    message.input = reader.string();
                    break;
                case 3:
                    message.output = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseDominoOp };
        if (object.key !== undefined && object.key !== null) {
            message.key = String(object.key);
        }
        else {
            message.key = "";
        }
        if (object.input !== undefined && object.input !== null) {
            message.input = String(object.input);
        }
        else {
            message.input = "";
        }
        if (object.output !== undefined && object.output !== null) {
            message.output = String(object.output);
        }
        else {
            message.output = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.key !== undefined && (obj.key = message.key);
        message.input !== undefined && (obj.input = message.input);
        message.output !== undefined && (obj.output = message.output);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseDominoOp };
        if (object.key !== undefined && object.key !== null) {
            message.key = object.key;
        }
        else {
            message.key = "";
        }
        if (object.input !== undefined && object.input !== null) {
            message.input = object.input;
        }
        else {
            message.input = "";
        }
        if (object.output !== undefined && object.output !== null) {
            message.output = object.output;
        }
        else {
            message.output = "";
        }
        return message;
    },
};
const baseProofOp = { type: "" };
export const ProofOp = {
    encode(message, writer = Writer.create()) {
        if (message.type !== "") {
            writer.uint32(10).string(message.type);
        }
        if (message.key.length !== 0) {
            writer.uint32(18).bytes(message.key);
        }
        if (message.data.length !== 0) {
            writer.uint32(26).bytes(message.data);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseProofOp };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.type = reader.string();
                    break;
                case 2:
                    message.key = reader.bytes();
                    break;
                case 3:
                    message.data = reader.bytes();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseProofOp };
        if (object.type !== undefined && object.type !== null) {
            message.type = String(object.type);
        }
        else {
            message.type = "";
        }
        if (object.key !== undefined && object.key !== null) {
            message.key = bytesFromBase64(object.key);
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = bytesFromBase64(object.data);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.type !== undefined && (obj.type = message.type);
        message.key !== undefined &&
            (obj.key = base64FromBytes(message.key !== undefined ? message.key : new Uint8Array()));
        message.data !== undefined &&
            (obj.data = base64FromBytes(message.data !== undefined ? message.data : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseProofOp };
        if (object.type !== undefined && object.type !== null) {
            message.type = object.type;
        }
        else {
            message.type = "";
        }
        if (object.key !== undefined && object.key !== null) {
            message.key = object.key;
        }
        else {
            message.key = new Uint8Array();
        }
        if (object.data !== undefined && object.data !== null) {
            message.data = object.data;
        }
        else {
            message.data = new Uint8Array();
        }
        return message;
    },
};
const baseProofOps = {};
export const ProofOps = {
    encode(message, writer = Writer.create()) {
        for (const v of message.ops) {
            ProofOp.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseProofOps };
        message.ops = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.ops.push(ProofOp.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseProofOps };
        message.ops = [];
        if (object.ops !== undefined && object.ops !== null) {
            for (const e of object.ops) {
                message.ops.push(ProofOp.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.ops) {
            obj.ops = message.ops.map((e) => (e ? ProofOp.toJSON(e) : undefined));
        }
        else {
            obj.ops = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseProofOps };
        message.ops = [];
        if (object.ops !== undefined && object.ops !== null) {
            for (const e of object.ops) {
                message.ops.push(ProofOp.fromPartial(e));
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
const atob = globalThis.atob ||
    ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64) {
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; ++i) {
        arr[i] = bin.charCodeAt(i);
    }
    return arr;
}
const btoa = globalThis.btoa ||
    ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr) {
    const bin = [];
    for (let i = 0; i < arr.byteLength; ++i) {
        bin.push(String.fromCharCode(arr[i]));
    }
    return btoa(bin.join(""));
}
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
