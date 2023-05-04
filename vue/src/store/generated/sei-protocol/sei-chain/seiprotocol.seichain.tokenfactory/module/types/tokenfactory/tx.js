/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Coin } from "../cosmos/base/v1beta1/coin";
export const protobufPackage = "seiprotocol.seichain.tokenfactory";
const baseMsgCreateDenom = { sender: "", subdenom: "" };
export const MsgCreateDenom = {
    encode(message, writer = Writer.create()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.subdenom !== "") {
            writer.uint32(18).string(message.subdenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgCreateDenom };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sender = reader.string();
                    break;
                case 2:
                    message.subdenom = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgCreateDenom };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = String(object.sender);
        }
        else {
            message.sender = "";
        }
        if (object.subdenom !== undefined && object.subdenom !== null) {
            message.subdenom = String(object.subdenom);
        }
        else {
            message.subdenom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sender !== undefined && (obj.sender = message.sender);
        message.subdenom !== undefined && (obj.subdenom = message.subdenom);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgCreateDenom };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = object.sender;
        }
        else {
            message.sender = "";
        }
        if (object.subdenom !== undefined && object.subdenom !== null) {
            message.subdenom = object.subdenom;
        }
        else {
            message.subdenom = "";
        }
        return message;
    },
};
const baseMsgCreateDenomResponse = { newTokenDenom: "" };
export const MsgCreateDenomResponse = {
    encode(message, writer = Writer.create()) {
        if (message.newTokenDenom !== "") {
            writer.uint32(10).string(message.newTokenDenom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgCreateDenomResponse };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.newTokenDenom = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgCreateDenomResponse };
        if (object.newTokenDenom !== undefined && object.newTokenDenom !== null) {
            message.newTokenDenom = String(object.newTokenDenom);
        }
        else {
            message.newTokenDenom = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.newTokenDenom !== undefined &&
            (obj.newTokenDenom = message.newTokenDenom);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgCreateDenomResponse };
        if (object.newTokenDenom !== undefined && object.newTokenDenom !== null) {
            message.newTokenDenom = object.newTokenDenom;
        }
        else {
            message.newTokenDenom = "";
        }
        return message;
    },
};
const baseMsgMint = { sender: "" };
export const MsgMint = {
    encode(message, writer = Writer.create()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgMint };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sender = reader.string();
                    break;
                case 2:
                    message.amount = Coin.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgMint };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = String(object.sender);
        }
        else {
            message.sender = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            message.amount = Coin.fromJSON(object.amount);
        }
        else {
            message.amount = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sender !== undefined && (obj.sender = message.sender);
        message.amount !== undefined &&
            (obj.amount = message.amount ? Coin.toJSON(message.amount) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgMint };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = object.sender;
        }
        else {
            message.sender = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            message.amount = Coin.fromPartial(object.amount);
        }
        else {
            message.amount = undefined;
        }
        return message;
    },
};
const baseMsgMintResponse = {};
export const MsgMintResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgMintResponse };
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
        const message = { ...baseMsgMintResponse };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseMsgMintResponse };
        return message;
    },
};
const baseMsgBurn = { sender: "" };
export const MsgBurn = {
    encode(message, writer = Writer.create()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgBurn };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sender = reader.string();
                    break;
                case 2:
                    message.amount = Coin.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgBurn };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = String(object.sender);
        }
        else {
            message.sender = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            message.amount = Coin.fromJSON(object.amount);
        }
        else {
            message.amount = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sender !== undefined && (obj.sender = message.sender);
        message.amount !== undefined &&
            (obj.amount = message.amount ? Coin.toJSON(message.amount) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgBurn };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = object.sender;
        }
        else {
            message.sender = "";
        }
        if (object.amount !== undefined && object.amount !== null) {
            message.amount = Coin.fromPartial(object.amount);
        }
        else {
            message.amount = undefined;
        }
        return message;
    },
};
const baseMsgBurnResponse = {};
export const MsgBurnResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgBurnResponse };
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
        const message = { ...baseMsgBurnResponse };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseMsgBurnResponse };
        return message;
    },
};
const baseMsgChangeAdmin = { sender: "", denom: "", newAdmin: "" };
export const MsgChangeAdmin = {
    encode(message, writer = Writer.create()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.denom !== "") {
            writer.uint32(18).string(message.denom);
        }
        if (message.newAdmin !== "") {
            writer.uint32(26).string(message.newAdmin);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgChangeAdmin };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sender = reader.string();
                    break;
                case 2:
                    message.denom = reader.string();
                    break;
                case 3:
                    message.newAdmin = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgChangeAdmin };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = String(object.sender);
        }
        else {
            message.sender = "";
        }
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = String(object.denom);
        }
        else {
            message.denom = "";
        }
        if (object.newAdmin !== undefined && object.newAdmin !== null) {
            message.newAdmin = String(object.newAdmin);
        }
        else {
            message.newAdmin = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sender !== undefined && (obj.sender = message.sender);
        message.denom !== undefined && (obj.denom = message.denom);
        message.newAdmin !== undefined && (obj.newAdmin = message.newAdmin);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgChangeAdmin };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = object.sender;
        }
        else {
            message.sender = "";
        }
        if (object.denom !== undefined && object.denom !== null) {
            message.denom = object.denom;
        }
        else {
            message.denom = "";
        }
        if (object.newAdmin !== undefined && object.newAdmin !== null) {
            message.newAdmin = object.newAdmin;
        }
        else {
            message.newAdmin = "";
        }
        return message;
    },
};
const baseMsgChangeAdminResponse = {};
export const MsgChangeAdminResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgChangeAdminResponse };
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
        const message = { ...baseMsgChangeAdminResponse };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseMsgChangeAdminResponse };
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    CreateDenom(request) {
        const data = MsgCreateDenom.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Msg", "CreateDenom", data);
        return promise.then((data) => MsgCreateDenomResponse.decode(new Reader(data)));
    }
    Mint(request) {
        const data = MsgMint.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Msg", "Mint", data);
        return promise.then((data) => MsgMintResponse.decode(new Reader(data)));
    }
    Burn(request) {
        const data = MsgBurn.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Msg", "Burn", data);
        return promise.then((data) => MsgBurnResponse.decode(new Reader(data)));
    }
    ChangeAdmin(request) {
        const data = MsgChangeAdmin.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.tokenfactory.Msg", "ChangeAdmin", data);
        return promise.then((data) => MsgChangeAdminResponse.decode(new Reader(data)));
    }
}
