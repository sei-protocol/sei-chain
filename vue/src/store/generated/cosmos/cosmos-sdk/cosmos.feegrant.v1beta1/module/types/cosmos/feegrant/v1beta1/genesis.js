/* eslint-disable */
import { Grant } from "../../../cosmos/feegrant/v1beta1/feegrant";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.feegrant.v1beta1";
const baseGenesisState = {};
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        for (const v of message.allowances) {
            Grant.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.allowances = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.allowances.push(Grant.decode(reader, reader.uint32()));
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
        message.allowances = [];
        if (object.allowances !== undefined && object.allowances !== null) {
            for (const e of object.allowances) {
                message.allowances.push(Grant.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.allowances) {
            obj.allowances = message.allowances.map((e) => e ? Grant.toJSON(e) : undefined);
        }
        else {
            obj.allowances = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.allowances = [];
        if (object.allowances !== undefined && object.allowances !== null) {
            for (const e of object.allowances) {
                message.allowances.push(Grant.fromPartial(e));
            }
        }
        return message;
    },
};
