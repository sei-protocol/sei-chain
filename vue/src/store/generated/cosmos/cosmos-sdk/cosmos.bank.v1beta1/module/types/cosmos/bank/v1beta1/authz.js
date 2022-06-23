/* eslint-disable */
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "cosmos.bank.v1beta1";
const baseSendAuthorization = {};
export const SendAuthorization = {
    encode(message, writer = Writer.create()) {
        for (const v of message.spendLimit) {
            Coin.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseSendAuthorization };
        message.spendLimit = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.spendLimit.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseSendAuthorization };
        message.spendLimit = [];
        if (object.spendLimit !== undefined && object.spendLimit !== null) {
            for (const e of object.spendLimit) {
                message.spendLimit.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.spendLimit) {
            obj.spendLimit = message.spendLimit.map((e) => e ? Coin.toJSON(e) : undefined);
        }
        else {
            obj.spendLimit = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseSendAuthorization };
        message.spendLimit = [];
        if (object.spendLimit !== undefined && object.spendLimit !== null) {
            for (const e of object.spendLimit) {
                message.spendLimit.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
