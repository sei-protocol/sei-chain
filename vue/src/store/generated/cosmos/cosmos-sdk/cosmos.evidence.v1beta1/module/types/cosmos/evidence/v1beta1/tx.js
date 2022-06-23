/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Any } from "../../../google/protobuf/any";
export const protobufPackage = "cosmos.evidence.v1beta1";
const baseMsgSubmitEvidence = { submitter: "" };
export const MsgSubmitEvidence = {
    encode(message, writer = Writer.create()) {
        if (message.submitter !== "") {
            writer.uint32(10).string(message.submitter);
        }
        if (message.evidence !== undefined) {
            Any.encode(message.evidence, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgSubmitEvidence };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.submitter = reader.string();
                    break;
                case 2:
                    message.evidence = Any.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgSubmitEvidence };
        if (object.submitter !== undefined && object.submitter !== null) {
            message.submitter = String(object.submitter);
        }
        else {
            message.submitter = "";
        }
        if (object.evidence !== undefined && object.evidence !== null) {
            message.evidence = Any.fromJSON(object.evidence);
        }
        else {
            message.evidence = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.submitter !== undefined && (obj.submitter = message.submitter);
        message.evidence !== undefined &&
            (obj.evidence = message.evidence
                ? Any.toJSON(message.evidence)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgSubmitEvidence };
        if (object.submitter !== undefined && object.submitter !== null) {
            message.submitter = object.submitter;
        }
        else {
            message.submitter = "";
        }
        if (object.evidence !== undefined && object.evidence !== null) {
            message.evidence = Any.fromPartial(object.evidence);
        }
        else {
            message.evidence = undefined;
        }
        return message;
    },
};
const baseMsgSubmitEvidenceResponse = {};
export const MsgSubmitEvidenceResponse = {
    encode(message, writer = Writer.create()) {
        if (message.hash.length !== 0) {
            writer.uint32(34).bytes(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgSubmitEvidenceResponse,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 4:
                    message.hash = reader.bytes();
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
            ...baseMsgSubmitEvidenceResponse,
        };
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = bytesFromBase64(object.hash);
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.hash !== undefined &&
            (obj.hash = base64FromBytes(message.hash !== undefined ? message.hash : new Uint8Array()));
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseMsgSubmitEvidenceResponse,
        };
        if (object.hash !== undefined && object.hash !== null) {
            message.hash = object.hash;
        }
        else {
            message.hash = new Uint8Array();
        }
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    SubmitEvidence(request) {
        const data = MsgSubmitEvidence.encode(request).finish();
        const promise = this.rpc.request("cosmos.evidence.v1beta1.Msg", "SubmitEvidence", data);
        return promise.then((data) => MsgSubmitEvidenceResponse.decode(new Reader(data)));
    }
}
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
