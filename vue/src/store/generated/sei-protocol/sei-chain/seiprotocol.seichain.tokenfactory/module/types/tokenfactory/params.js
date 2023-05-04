/* eslint-disable */
import { Coin } from "../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "seiprotocol.seichain.tokenfactory";
const baseParams = {};
export const Params = {
    encode(message, writer = Writer.create()) {
        for (const v of message.denomCreationFee) {
            Coin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseParams };
        message.denomCreationFee = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.denomCreationFee.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseParams };
        message.denomCreationFee = [];
        if (object.denomCreationFee !== undefined &&
            object.denomCreationFee !== null) {
            for (const e of object.denomCreationFee) {
                message.denomCreationFee.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.denomCreationFee) {
            obj.denomCreationFee = message.denomCreationFee.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.denomCreationFee = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseParams };
        message.denomCreationFee = [];
        if (object.denomCreationFee !== undefined &&
            object.denomCreationFee !== null) {
            for (const e of object.denomCreationFee) {
                message.denomCreationFee.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
