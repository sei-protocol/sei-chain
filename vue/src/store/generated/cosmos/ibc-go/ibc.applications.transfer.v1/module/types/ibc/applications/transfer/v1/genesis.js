/* eslint-disable */
import { DenomTrace, Params, } from "../../../../ibc/applications/transfer/v1/transfer";
import { Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "ibc.applications.transfer.v1";
const baseGenesisState = { portId: "" };
export const GenesisState = {
    encode(message, writer = Writer.create()) {
        if (message.portId !== "") {
            writer.uint32(10).string(message.portId);
        }
        for (const v of message.denomTraces) {
            DenomTrace.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGenesisState };
        message.denomTraces = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.portId = reader.string();
                    break;
                case 2:
                    message.denomTraces.push(DenomTrace.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.params = Params.decode(reader, reader.uint32());
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
        message.denomTraces = [];
        if (object.portId !== undefined && object.portId !== null) {
            message.portId = String(object.portId);
        }
        else {
            message.portId = "";
        }
        if (object.denomTraces !== undefined && object.denomTraces !== null) {
            for (const e of object.denomTraces) {
                message.denomTraces.push(DenomTrace.fromJSON(e));
            }
        }
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromJSON(object.params);
        }
        else {
            message.params = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.portId !== undefined && (obj.portId = message.portId);
        if (message.denomTraces) {
            obj.denomTraces = message.denomTraces.map((e) => e ? DenomTrace.toJSON(e) : undefined);
        }
        else {
            obj.denomTraces = [];
        }
        message.params !== undefined &&
            (obj.params = message.params ? Params.toJSON(message.params) : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGenesisState };
        message.denomTraces = [];
        if (object.portId !== undefined && object.portId !== null) {
            message.portId = object.portId;
        }
        else {
            message.portId = "";
        }
        if (object.denomTraces !== undefined && object.denomTraces !== null) {
            for (const e of object.denomTraces) {
                message.denomTraces.push(DenomTrace.fromPartial(e));
            }
        }
        if (object.params !== undefined && object.params !== null) {
            message.params = Params.fromPartial(object.params);
        }
        else {
            message.params = undefined;
        }
        return message;
    },
};
