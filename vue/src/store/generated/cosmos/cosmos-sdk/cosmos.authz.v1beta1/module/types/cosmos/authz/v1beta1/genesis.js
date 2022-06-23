/* eslint-disable */
import { GrantAuthorization } from "../../../cosmos/authz/v1beta1/authz";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.authz.v1beta1";
const baseGenesisState = {};
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        for (const v of message.authorization) {
            GrantAuthorization.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.authorization = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.authorization.push(GrantAuthorization.decode(reader, reader.uint32()));
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
        message.authorization = [];
        if (object.authorization !== undefined && object.authorization !== null) {
            for (const e of object.authorization) {
                message.authorization.push(GrantAuthorization.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.authorization) {
            obj.authorization = message.authorization.map((e) => e ? GrantAuthorization.toJSON(e) : undefined);
        }
        else {
            obj.authorization = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.authorization = [];
        if (object.authorization !== undefined && object.authorization !== null) {
            for (const e of object.authorization) {
                message.authorization.push(GrantAuthorization.fromPartial(e));
            }
        }
        return message;
    },
};
