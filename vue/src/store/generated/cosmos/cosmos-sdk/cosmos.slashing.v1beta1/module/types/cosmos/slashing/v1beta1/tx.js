/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
export const protobufPackage = "cosmos.slashing.v1beta1";
const baseMsgUnjail = { validatorAddr: "" };
export const MsgUnjail = {
    encode(message, writer = Writer.create()) {
        if (message.validatorAddr !== "") {
            writer.uint32(10).string(message.validatorAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgUnjail };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.validatorAddr = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgUnjail };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = String(object.validatorAddr);
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.validatorAddr !== undefined &&
            (obj.validatorAddr = message.validatorAddr);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgUnjail };
        if (object.validatorAddr !== undefined && object.validatorAddr !== null) {
            message.validatorAddr = object.validatorAddr;
        }
        else {
            message.validatorAddr = "";
        }
        return message;
    },
};
const baseMsgUnjailResponse = {};
export const MsgUnjailResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgUnjailResponse };
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
        const message = { ...baseMsgUnjailResponse };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = { ...baseMsgUnjailResponse };
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    Unjail(request) {
        const data = MsgUnjail.encode(request).finish();
        const promise = this.rpc.request("cosmos.slashing.v1beta1.Msg", "Unjail", data);
        return promise.then((data) => MsgUnjailResponse.decode(new Reader(data)));
    }
}
