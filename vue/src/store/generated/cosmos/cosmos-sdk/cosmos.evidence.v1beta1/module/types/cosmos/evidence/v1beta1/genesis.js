/* eslint-disable */
import { Any } from "../../../google/protobuf/any";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.evidence.v1beta1";
const baseGenesisState = {};
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        for (const v of message.evidence) {
            Any.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.evidence = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.evidence.push(Any.decode(reader, reader.uint32()));
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
        message.evidence = [];
        if (object.evidence !== undefined && object.evidence !== null) {
            for (const e of object.evidence) {
                message.evidence.push(Any.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.evidence) {
            obj.evidence = message.evidence.map((e) => e ? Any.toJSON(e) : undefined);
        }
        else {
            obj.evidence = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.evidence = [];
        if (object.evidence !== undefined && object.evidence !== null) {
            for (const e of object.evidence) {
                message.evidence.push(Any.fromPartial(e));
            }
        }
        return message;
    },
};
