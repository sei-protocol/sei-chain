/* eslint-disable */
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.crisis.v1beta1";
const baseGenesisState = {};
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        if (message.constantFee !== undefined) {
            Coin.encode(message.constantFee, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 3:
                    message.constantFee = Coin.decode(reader, reader.uint32());
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
        if (object.constantFee !== undefined && object.constantFee !== null) {
            message.constantFee = Coin.fromJSON(object.constantFee);
        }
        else {
            message.constantFee = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.constantFee !== undefined &&
            (obj.constantFee = message.constantFee
                ? Coin.toJSON(message.constantFee)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        if (object.constantFee !== undefined && object.constantFee !== null) {
            message.constantFee = Coin.fromPartial(object.constantFee);
        }
        else {
            message.constantFee = undefined;
        }
        return message;
    },
};
