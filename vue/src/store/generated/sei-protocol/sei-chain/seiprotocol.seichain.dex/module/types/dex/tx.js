/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Order, Cancellation } from "../dex/order";
import { Coin } from "../cosmos/base/v1beta1/coin";
import { ContractInfo } from "../dex/contract";
export const protobufPackage = "seiprotocol.seichain.dex";
const baseMsgPlaceOrders = { creator: "", contractAddr: "" };
export const MsgPlaceOrders = {
    encode(message, writer = Writer.create()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        for (const v of message.orders) {
            Order.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.contractAddr !== "") {
            writer.uint32(26).string(message.contractAddr);
        }
        for (const v of message.funds) {
            Coin.encode(v, writer.uint32(34).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgPlaceOrders };
        message.orders = [];
        message.funds = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.creator = reader.string();
                    break;
                case 2:
                    message.orders.push(Order.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.contractAddr = reader.string();
                    break;
                case 4:
                    message.funds.push(Coin.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgPlaceOrders };
        message.orders = [];
        message.funds = [];
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = String(object.creator);
        }
        else {
            message.creator = "";
        }
        if (object.orders !== undefined && object.orders !== null) {
            for (const e of object.orders) {
                message.orders.push(Order.fromJSON(e));
            }
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        if (object.funds !== undefined && object.funds !== null) {
            for (const e of object.funds) {
                message.funds.push(Coin.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.creator !== undefined && (obj.creator = message.creator);
        if (message.orders) {
            obj.orders = message.orders.map((e) => (e ? Order.toJSON(e) : undefined));
        }
        else {
            obj.orders = [];
        }
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        if (message.funds) {
            obj.funds = message.funds.map((e) => (e ? Coin.toJSON(e) : undefined));
        }
        else {
            obj.funds = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgPlaceOrders };
        message.orders = [];
        message.funds = [];
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = object.creator;
        }
        else {
            message.creator = "";
        }
        if (object.orders !== undefined && object.orders !== null) {
            for (const e of object.orders) {
                message.orders.push(Order.fromPartial(e));
            }
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        if (object.funds !== undefined && object.funds !== null) {
            for (const e of object.funds) {
                message.funds.push(Coin.fromPartial(e));
            }
        }
        return message;
    },
};
const baseMsgPlaceOrdersResponse = { orderIds: 0 };
export const MsgPlaceOrdersResponse = {
    encode(message, writer = Writer.create()) {
        writer.uint32(10).fork();
        for (const v of message.orderIds) {
            writer.uint64(v);
        }
        writer.ldelim();
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgPlaceOrdersResponse };
        message.orderIds = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.orderIds.push(longToNumber(reader.uint64()));
                        }
                    }
                    else {
                        message.orderIds.push(longToNumber(reader.uint64()));
                    }
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgPlaceOrdersResponse };
        message.orderIds = [];
        if (object.orderIds !== undefined && object.orderIds !== null) {
            for (const e of object.orderIds) {
                message.orderIds.push(Number(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.orderIds) {
            obj.orderIds = message.orderIds.map((e) => e);
        }
        else {
            obj.orderIds = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgPlaceOrdersResponse };
        message.orderIds = [];
        if (object.orderIds !== undefined && object.orderIds !== null) {
            for (const e of object.orderIds) {
                message.orderIds.push(e);
            }
        }
        return message;
    },
};
const baseMsgCancelOrders = { creator: "", contractAddr: "" };
export const MsgCancelOrders = {
    encode(message, writer = Writer.create()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        for (const v of message.cancellations) {
            Cancellation.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.contractAddr !== "") {
            writer.uint32(26).string(message.contractAddr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgCancelOrders };
        message.cancellations = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.creator = reader.string();
                    break;
                case 2:
                    message.cancellations.push(Cancellation.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.contractAddr = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgCancelOrders };
        message.cancellations = [];
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = String(object.creator);
        }
        else {
            message.creator = "";
        }
        if (object.cancellations !== undefined && object.cancellations !== null) {
            for (const e of object.cancellations) {
                message.cancellations.push(Cancellation.fromJSON(e));
            }
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = String(object.contractAddr);
        }
        else {
            message.contractAddr = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.creator !== undefined && (obj.creator = message.creator);
        if (message.cancellations) {
            obj.cancellations = message.cancellations.map((e) => e ? Cancellation.toJSON(e) : undefined);
        }
        else {
            obj.cancellations = [];
        }
        message.contractAddr !== undefined &&
            (obj.contractAddr = message.contractAddr);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgCancelOrders };
        message.cancellations = [];
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = object.creator;
        }
        else {
            message.creator = "";
        }
        if (object.cancellations !== undefined && object.cancellations !== null) {
            for (const e of object.cancellations) {
                message.cancellations.push(Cancellation.fromPartial(e));
            }
        }
        if (object.contractAddr !== undefined && object.contractAddr !== null) {
            message.contractAddr = object.contractAddr;
        }
        else {
            message.contractAddr = "";
        }
        return message;
    },
};
const baseMsgCancelOrdersResponse = {};
export const MsgCancelOrdersResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgCancelOrdersResponse,
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
            ...baseMsgCancelOrdersResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgCancelOrdersResponse,
        };
        return message;
    },
};
const baseMsgRegisterContract = { creator: "" };
export const MsgRegisterContract = {
    encode(message, writer = Writer.create()) {
        if (message.creator !== "") {
            writer.uint32(10).string(message.creator);
        }
        if (message.contract !== undefined) {
            ContractInfo.encode(message.contract, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMsgRegisterContract };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.creator = reader.string();
                    break;
                case 2:
                    message.contract = ContractInfo.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMsgRegisterContract };
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = String(object.creator);
        }
        else {
            message.creator = "";
        }
        if (object.contract !== undefined && object.contract !== null) {
            message.contract = ContractInfo.fromJSON(object.contract);
        }
        else {
            message.contract = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.creator !== undefined && (obj.creator = message.creator);
        message.contract !== undefined &&
            (obj.contract = message.contract
                ? ContractInfo.toJSON(message.contract)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMsgRegisterContract };
        if (object.creator !== undefined && object.creator !== null) {
            message.creator = object.creator;
        }
        else {
            message.creator = "";
        }
        if (object.contract !== undefined && object.contract !== null) {
            message.contract = ContractInfo.fromPartial(object.contract);
        }
        else {
            message.contract = undefined;
        }
        return message;
    },
};
const baseMsgRegisterContractResponse = {};
export const MsgRegisterContractResponse = {
    encode(_, writer = Writer.create()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseMsgRegisterContractResponse,
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
            ...baseMsgRegisterContractResponse,
        };
        return message;
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    fromPartial(_) {
        const message = {
            ...baseMsgRegisterContractResponse,
        };
        return message;
    },
};
export class MsgClientImpl {
    constructor(rpc) {
        this.rpc = rpc;
    }
    PlaceOrders(request) {
        const data = MsgPlaceOrders.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Msg", "PlaceOrders", data);
        return promise.then((data) => MsgPlaceOrdersResponse.decode(new Reader(data)));
    }
    CancelOrders(request) {
        const data = MsgCancelOrders.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Msg", "CancelOrders", data);
        return promise.then((data) => MsgCancelOrdersResponse.decode(new Reader(data)));
    }
    RegisterContract(request) {
        const data = MsgRegisterContract.encode(request).finish();
        const promise = this.rpc.request("seiprotocol.seichain.dex.Msg", "RegisterContract", data);
        return promise.then((data) => MsgRegisterContractResponse.decode(new Reader(data)));
    }
}
var globalThis = (() => {
    if (typeof globalThis !== "undefined")
        return globalThis;
    if (typeof self !== "undefined")
        return self;
    if (typeof window !== "undefined")
        return window;
    if (typeof global !== "undefined")
        return global;
    throw "Unable to locate global object";
})();
function longToNumber(long) {
    if (long.gt(Number.MAX_SAFE_INTEGER)) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    return long.toNumber();
}
if (util.Long !== Long) {
    util.Long = Long;
    configure();
}
