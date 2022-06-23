/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Grant } from "../../../cosmos/authz/v1beta1/authz";
import { Any } from "../../../google/protobuf/any";
export const protobufPackage = "cosmos.authz.v1beta1";
const baseMsgGrant = { granter: "", grantee: "" };
export const MsgGrant = {
    encode(message, writer = Writer.create()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.grant !== undefined) {
            Grant.encode(message.grant, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgGrant };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.granter = reader.string();
                    break;
                case 2:
                    message.grantee = reader.string();
                    break;
                case 3:
                    message.grant = Grant.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgGrant };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = String(object.granter);
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = String(object.grantee);
        }
        else {
            message.grantee = "";
        }
        if (object.grant !== undefined && object.grant !== null) {
            message.grant = Grant.fromJSON(object.grant);
        }
        else {
            message.grant = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.granter !== undefined && (obj.granter = message.granter);
        message.grantee !== undefined && (obj.grantee = message.grantee);
        message.grant !== undefined &&
            (obj.grant = message.grant ? Grant.toJSON(message.grant) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgGrant };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = object.granter;
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = object.grantee;
        }
        else {
            message.grantee = "";
        }
        if (object.grant !== undefined && object.grant !== null) {
            message.grant = Grant.fromPartial(object.grant);
        }
        else {
            message.grant = undefined;
        }
        return message;
    },
};
const baseMsgExecResponse = {};
export const MsgExecResponse = {
    encode(message, writer = Writer.create()) {
        for (const v of message.results) {
            writer.uint32(10).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgExecResponse };
        message.results = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.results.push(reader.bytes());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgExecResponse };
        message.results = [];
        if (object.results !== undefined && object.results !== null) {
            for (const e of object.results) {
                message.results.push(bytesFromBase64(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.results) {
            obj.results = message.results.map((e) => base64FromBytes(e !== undefined ? e : new Uint8Array()));
        }
        else {
            obj.results = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgExecResponse };
        message.results = [];
        if (object.results !== undefined && object.results !== null) {
            for (const e of object.results) {
                message.results.push(e);
            }
        }
        return message;
    },
};
const baseMsgExec = { grantee: "" };
export const MsgExec = {
    encode(message, writer = Writer.create()) {
        if (message.grantee !== "") {
            writer.uint32(10).string(message.grantee);
        }
        for (const v of message.msgs) {
            Any.encode(v, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgExec };
        message.msgs = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.grantee = reader.string();
                    break;
                case 2:
                    message.msgs.push(Any.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgExec };
        message.msgs = [];
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = String(object.grantee);
        }
        else {
            message.grantee = "";
        }
        if (object.msgs !== undefined && object.msgs !== null) {
            for (const e of object.msgs) {
                message.msgs.push(Any.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.grantee !== undefined && (obj.grantee = message.grantee);
        if (message.msgs) {
            obj.msgs = message.msgs.map((e) => (e ? Any.toJSON(e) : undefined));
        }
        else {
            obj.msgs = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgExec };
        message.msgs = [];
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = object.grantee;
        }
        else {
            message.grantee = "";
        }
        if (object.msgs !== undefined && object.msgs !== null) {
            for (const e of object.msgs) {
                message.msgs.push(Any.fromPartial(e));
            }
        }
        return message;
    },
};
const baseMsgGrantResponse = {};
export const MsgGrantResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgGrantResponse };
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
        const message = { ...baseMsgGrantResponse };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseMsgGrantResponse };
        return message;
    },
};
const baseMsgRevoke = { granter: "", grantee: "", msgTypeUrl: "" };
export const MsgRevoke = {
    encode(message, writer = Writer.create()) {
        if (message.granter !== "") {
            writer.uint32(10).string(message.granter);
        }
        if (message.grantee !== "") {
            writer.uint32(18).string(message.grantee);
        }
        if (message.msgTypeUrl !== "") {
            writer.uint32(26).string(message.msgTypeUrl);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgRevoke };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.granter = reader.string();
                    break;
                case 2:
                    message.grantee = reader.string();
                    break;
                case 3:
                    message.msgTypeUrl = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgRevoke };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = String(object.granter);
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = String(object.grantee);
        }
        else {
            message.grantee = "";
        }
        if (object.msgTypeUrl !== undefined && object.msgTypeUrl !== null) {
            message.msgTypeUrl = String(object.msgTypeUrl);
        }
        else {
            message.msgTypeUrl = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.granter !== undefined && (obj.granter = message.granter);
        message.grantee !== undefined && (obj.grantee = message.grantee);
        message.msgTypeUrl !== undefined && (obj.msgTypeUrl = message.msgTypeUrl);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgRevoke };
        if (object.granter !== undefined && object.granter !== null) {
            message.granter = object.granter;
        }
        else {
            message.granter = "";
        }
        if (object.grantee !== undefined && object.grantee !== null) {
            message.grantee = object.grantee;
        }
        else {
            message.grantee = "";
        }
        if (object.msgTypeUrl !== undefined && object.msgTypeUrl !== null) {
            message.msgTypeUrl = object.msgTypeUrl;
        }
        else {
            message.msgTypeUrl = "";
        }
        return message;
    },
};
const baseMsgRevokeResponse = {};
export const MsgRevokeResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgRevokeResponse };
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
        const message = { ...baseMsgRevokeResponse };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseMsgRevokeResponse };
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    Grant(request) {
        const data = MsgGrant.encode(request).finish();
        const promise = this.rpc.request("cosmos.authz.v1beta1.Msg", "Grant", data);
        return promise.then((data) => MsgGrantResponse.decode(new Reader(data)));
    }
    Exec(request) {
        const data = MsgExec.encode(request).finish();
        const promise = this.rpc.request("cosmos.authz.v1beta1.Msg", "Exec", data);
        return promise.then((data) => MsgExecResponse.decode(new Reader(data)));
    }
    Revoke(request) {
        const data = MsgRevoke.encode(request).finish();
        const promise = this.rpc.request("cosmos.authz.v1beta1.Msg", "Revoke", data);
        return promise.then((data) => MsgRevokeResponse.decode(new Reader(data)));
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
