/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
export const protobufPackage = "cosmos.crisis.v1beta1";
const baseMsgVerifyInvariant = {
    sender: "",
    invariantModuleName: "",
    invariantRoute: "",
};
export const MsgVerifyInvariant = {
    encode(message, writer = Writer.create()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.invariantModuleName !== "") {
            writer.uint32(18).string(message.invariantModuleName);
        }
        if (message.invariantRoute !== "") {
            writer.uint32(26).string(message.invariantRoute);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgVerifyInvariant };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.sender = reader.string();
                    break;
                case 2:
                    message.invariantModuleName = reader.string();
                    break;
                case 3:
                    message.invariantRoute = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgVerifyInvariant };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = String(object.sender);
        }
        else {
            message.sender = "";
        }
        if (object.invariantModuleName !== undefined &&
            object.invariantModuleName !== null) {
            message.invariantModuleName = String(object.invariantModuleName);
        }
        else {
            message.invariantModuleName = "";
        }
        if (object.invariantRoute !== undefined && object.invariantRoute !== null) {
            message.invariantRoute = String(object.invariantRoute);
        }
        else {
            message.invariantRoute = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.sender !== undefined && (obj.sender = message.sender);
        message.invariantModuleName !== undefined &&
            (obj.invariantModuleName = message.invariantModuleName);
        message.invariantRoute !== undefined &&
            (obj.invariantRoute = message.invariantRoute);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgVerifyInvariant };
        if (object.sender !== undefined && object.sender !== null) {
            message.sender = object.sender;
        }
        else {
            message.sender = "";
        }
        if (object.invariantModuleName !== undefined &&
            object.invariantModuleName !== null) {
            message.invariantModuleName = object.invariantModuleName;
        }
        else {
            message.invariantModuleName = "";
        }
        if (object.invariantRoute !== undefined && object.invariantRoute !== null) {
            message.invariantRoute = object.invariantRoute;
        }
        else {
            message.invariantRoute = "";
        }
        return message;
    },
};
const baseMsgVerifyInvariantResponse = {};
export const MsgVerifyInvariantResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgVerifyInvariantResponse,
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
            ...baseMsgVerifyInvariantResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgVerifyInvariantResponse,
        };
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    VerifyInvariant(request) {
        const data = MsgVerifyInvariant.encode(request).finish();
        const promise = this.rpc.request("cosmos.crisis.v1beta1.Msg", "VerifyInvariant", data);
        return promise.then((data) => MsgVerifyInvariantResponse.decode(new Reader(data)));
    }
}
