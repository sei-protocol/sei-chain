/* eslint-disable */
import {
  PointerType,
  pointerTypeFromJSON,
  pointerTypeToJSON,
} from "../../evm/v1/enums";
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { Any } from "../../google/protobuf/any";
import { Coin } from "../../cosmos/base/v1beta1/coin";

export const protobufPackage = "sei.evm.v1";

export interface MsgEVMTransaction {
  data: Any | undefined;
  derived: Uint8Array;
}

export interface MsgEVMTransactionResponse {
  gasUsed: number;
  vmError: string;
  returnData: Uint8Array;
  hash: string;
}

export interface MsgInternalEVMCall {
  sender: string;
  value: string;
  to: string;
  data: Uint8Array;
}

export interface MsgInternalEVMCallResponse {}

export interface MsgInternalEVMDelegateCall {
  sender: string;
  codeHash: Uint8Array;
  to: string;
  data: Uint8Array;
  fromContract: string;
}

export interface MsgInternalEVMDelegateCallResponse {}

export interface MsgSend {
  fromAddress: string;
  toAddress: string;
  amount: Coin[];
}

export interface MsgSendResponse {}

export interface MsgRegisterPointer {
  sender: string;
  pointerType: PointerType;
  ercAddress: string;
}

export interface MsgRegisterPointerResponse {
  pointerAddress: string;
}

export interface MsgAssociateContractAddress {
  sender: string;
  address: string;
}

export interface MsgAssociateContractAddressResponse {}

export interface MsgAssociate {
  sender: string;
  customMessage: string;
}

export interface MsgAssociateResponse {}

const baseMsgEVMTransaction: object = {};

export const MsgEVMTransaction = {
  encode(message: MsgEVMTransaction, writer: Writer = Writer.create()): Writer {
    if (message.data !== undefined) {
      Any.encode(message.data, writer.uint32(10).fork()).ldelim();
    }
    if (message.derived.length !== 0) {
      writer.uint32(18).bytes(message.derived);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgEVMTransaction {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgEVMTransaction } as MsgEVMTransaction;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.data = Any.decode(reader, reader.uint32());
          break;
        case 2:
          message.derived = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgEVMTransaction {
    const message = { ...baseMsgEVMTransaction } as MsgEVMTransaction;
    if (object.data !== undefined && object.data !== null) {
      message.data = Any.fromJSON(object.data);
    } else {
      message.data = undefined;
    }
    if (object.derived !== undefined && object.derived !== null) {
      message.derived = bytesFromBase64(object.derived);
    }
    return message;
  },

  toJSON(message: MsgEVMTransaction): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = message.data ? Any.toJSON(message.data) : undefined);
    message.derived !== undefined &&
      (obj.derived = base64FromBytes(
        message.derived !== undefined ? message.derived : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<MsgEVMTransaction>): MsgEVMTransaction {
    const message = { ...baseMsgEVMTransaction } as MsgEVMTransaction;
    if (object.data !== undefined && object.data !== null) {
      message.data = Any.fromPartial(object.data);
    } else {
      message.data = undefined;
    }
    if (object.derived !== undefined && object.derived !== null) {
      message.derived = object.derived;
    } else {
      message.derived = new Uint8Array();
    }
    return message;
  },
};

const baseMsgEVMTransactionResponse: object = {
  gasUsed: 0,
  vmError: "",
  hash: "",
};

export const MsgEVMTransactionResponse = {
  encode(
    message: MsgEVMTransactionResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.gasUsed !== 0) {
      writer.uint32(8).uint64(message.gasUsed);
    }
    if (message.vmError !== "") {
      writer.uint32(18).string(message.vmError);
    }
    if (message.returnData.length !== 0) {
      writer.uint32(26).bytes(message.returnData);
    }
    if (message.hash !== "") {
      writer.uint32(34).string(message.hash);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgEVMTransactionResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgEVMTransactionResponse,
    } as MsgEVMTransactionResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.gasUsed = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.vmError = reader.string();
          break;
        case 3:
          message.returnData = reader.bytes();
          break;
        case 4:
          message.hash = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgEVMTransactionResponse {
    const message = {
      ...baseMsgEVMTransactionResponse,
    } as MsgEVMTransactionResponse;
    if (object.gasUsed !== undefined && object.gasUsed !== null) {
      message.gasUsed = Number(object.gasUsed);
    } else {
      message.gasUsed = 0;
    }
    if (object.vmError !== undefined && object.vmError !== null) {
      message.vmError = String(object.vmError);
    } else {
      message.vmError = "";
    }
    if (object.returnData !== undefined && object.returnData !== null) {
      message.returnData = bytesFromBase64(object.returnData);
    }
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = String(object.hash);
    } else {
      message.hash = "";
    }
    return message;
  },

  toJSON(message: MsgEVMTransactionResponse): unknown {
    const obj: any = {};
    message.gasUsed !== undefined && (obj.gasUsed = message.gasUsed);
    message.vmError !== undefined && (obj.vmError = message.vmError);
    message.returnData !== undefined &&
      (obj.returnData = base64FromBytes(
        message.returnData !== undefined ? message.returnData : new Uint8Array()
      ));
    message.hash !== undefined && (obj.hash = message.hash);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgEVMTransactionResponse>
  ): MsgEVMTransactionResponse {
    const message = {
      ...baseMsgEVMTransactionResponse,
    } as MsgEVMTransactionResponse;
    if (object.gasUsed !== undefined && object.gasUsed !== null) {
      message.gasUsed = object.gasUsed;
    } else {
      message.gasUsed = 0;
    }
    if (object.vmError !== undefined && object.vmError !== null) {
      message.vmError = object.vmError;
    } else {
      message.vmError = "";
    }
    if (object.returnData !== undefined && object.returnData !== null) {
      message.returnData = object.returnData;
    } else {
      message.returnData = new Uint8Array();
    }
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = "";
    }
    return message;
  },
};

const baseMsgInternalEVMCall: object = { sender: "", value: "", to: "" };

export const MsgInternalEVMCall = {
  encode(
    message: MsgInternalEVMCall,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.value !== "") {
      writer.uint32(18).string(message.value);
    }
    if (message.to !== "") {
      writer.uint32(26).string(message.to);
    }
    if (message.data.length !== 0) {
      writer.uint32(34).bytes(message.data);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgInternalEVMCall {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgInternalEVMCall } as MsgInternalEVMCall;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.value = reader.string();
          break;
        case 3:
          message.to = reader.string();
          break;
        case 4:
          message.data = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgInternalEVMCall {
    const message = { ...baseMsgInternalEVMCall } as MsgInternalEVMCall;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = String(object.value);
    } else {
      message.value = "";
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = String(object.to);
    } else {
      message.to = "";
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: MsgInternalEVMCall): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.value !== undefined && (obj.value = message.value);
    message.to !== undefined && (obj.to = message.to);
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<MsgInternalEVMCall>): MsgInternalEVMCall {
    const message = { ...baseMsgInternalEVMCall } as MsgInternalEVMCall;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = "";
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = object.to;
    } else {
      message.to = "";
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseMsgInternalEVMCallResponse: object = {};

export const MsgInternalEVMCallResponse = {
  encode(
    _: MsgInternalEVMCallResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgInternalEVMCallResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgInternalEVMCallResponse,
    } as MsgInternalEVMCallResponse;
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

  fromJSON(_: any): MsgInternalEVMCallResponse {
    const message = {
      ...baseMsgInternalEVMCallResponse,
    } as MsgInternalEVMCallResponse;
    return message;
  },

  toJSON(_: MsgInternalEVMCallResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgInternalEVMCallResponse>
  ): MsgInternalEVMCallResponse {
    const message = {
      ...baseMsgInternalEVMCallResponse,
    } as MsgInternalEVMCallResponse;
    return message;
  },
};

const baseMsgInternalEVMDelegateCall: object = {
  sender: "",
  to: "",
  fromContract: "",
};

export const MsgInternalEVMDelegateCall = {
  encode(
    message: MsgInternalEVMDelegateCall,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.codeHash.length !== 0) {
      writer.uint32(18).bytes(message.codeHash);
    }
    if (message.to !== "") {
      writer.uint32(26).string(message.to);
    }
    if (message.data.length !== 0) {
      writer.uint32(34).bytes(message.data);
    }
    if (message.fromContract !== "") {
      writer.uint32(42).string(message.fromContract);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgInternalEVMDelegateCall {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgInternalEVMDelegateCall,
    } as MsgInternalEVMDelegateCall;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.codeHash = reader.bytes();
          break;
        case 3:
          message.to = reader.string();
          break;
        case 4:
          message.data = reader.bytes();
          break;
        case 5:
          message.fromContract = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgInternalEVMDelegateCall {
    const message = {
      ...baseMsgInternalEVMDelegateCall,
    } as MsgInternalEVMDelegateCall;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.codeHash !== undefined && object.codeHash !== null) {
      message.codeHash = bytesFromBase64(object.codeHash);
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = String(object.to);
    } else {
      message.to = "";
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    if (object.fromContract !== undefined && object.fromContract !== null) {
      message.fromContract = String(object.fromContract);
    } else {
      message.fromContract = "";
    }
    return message;
  },

  toJSON(message: MsgInternalEVMDelegateCall): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.codeHash !== undefined &&
      (obj.codeHash = base64FromBytes(
        message.codeHash !== undefined ? message.codeHash : new Uint8Array()
      ));
    message.to !== undefined && (obj.to = message.to);
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    message.fromContract !== undefined &&
      (obj.fromContract = message.fromContract);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgInternalEVMDelegateCall>
  ): MsgInternalEVMDelegateCall {
    const message = {
      ...baseMsgInternalEVMDelegateCall,
    } as MsgInternalEVMDelegateCall;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.codeHash !== undefined && object.codeHash !== null) {
      message.codeHash = object.codeHash;
    } else {
      message.codeHash = new Uint8Array();
    }
    if (object.to !== undefined && object.to !== null) {
      message.to = object.to;
    } else {
      message.to = "";
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    if (object.fromContract !== undefined && object.fromContract !== null) {
      message.fromContract = object.fromContract;
    } else {
      message.fromContract = "";
    }
    return message;
  },
};

const baseMsgInternalEVMDelegateCallResponse: object = {};

export const MsgInternalEVMDelegateCallResponse = {
  encode(
    _: MsgInternalEVMDelegateCallResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgInternalEVMDelegateCallResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgInternalEVMDelegateCallResponse,
    } as MsgInternalEVMDelegateCallResponse;
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

  fromJSON(_: any): MsgInternalEVMDelegateCallResponse {
    const message = {
      ...baseMsgInternalEVMDelegateCallResponse,
    } as MsgInternalEVMDelegateCallResponse;
    return message;
  },

  toJSON(_: MsgInternalEVMDelegateCallResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgInternalEVMDelegateCallResponse>
  ): MsgInternalEVMDelegateCallResponse {
    const message = {
      ...baseMsgInternalEVMDelegateCallResponse,
    } as MsgInternalEVMDelegateCallResponse;
    return message;
  },
};

const baseMsgSend: object = { fromAddress: "", toAddress: "" };

export const MsgSend = {
  encode(message: MsgSend, writer: Writer = Writer.create()): Writer {
    if (message.fromAddress !== "") {
      writer.uint32(10).string(message.fromAddress);
    }
    if (message.toAddress !== "") {
      writer.uint32(18).string(message.toAddress);
    }
    for (const v of message.amount) {
      Coin.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgSend {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgSend } as MsgSend;
    message.amount = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.fromAddress = reader.string();
          break;
        case 2:
          message.toAddress = reader.string();
          break;
        case 3:
          message.amount.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgSend {
    const message = { ...baseMsgSend } as MsgSend;
    message.amount = [];
    if (object.fromAddress !== undefined && object.fromAddress !== null) {
      message.fromAddress = String(object.fromAddress);
    } else {
      message.fromAddress = "";
    }
    if (object.toAddress !== undefined && object.toAddress !== null) {
      message.toAddress = String(object.toAddress);
    } else {
      message.toAddress = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgSend): unknown {
    const obj: any = {};
    message.fromAddress !== undefined &&
      (obj.fromAddress = message.fromAddress);
    message.toAddress !== undefined && (obj.toAddress = message.toAddress);
    if (message.amount) {
      obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.amount = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MsgSend>): MsgSend {
    const message = { ...baseMsgSend } as MsgSend;
    message.amount = [];
    if (object.fromAddress !== undefined && object.fromAddress !== null) {
      message.fromAddress = object.fromAddress;
    } else {
      message.fromAddress = "";
    }
    if (object.toAddress !== undefined && object.toAddress !== null) {
      message.toAddress = object.toAddress;
    } else {
      message.toAddress = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgSendResponse: object = {};

export const MsgSendResponse = {
  encode(_: MsgSendResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgSendResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgSendResponse } as MsgSendResponse;
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

  fromJSON(_: any): MsgSendResponse {
    const message = { ...baseMsgSendResponse } as MsgSendResponse;
    return message;
  },

  toJSON(_: MsgSendResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgSendResponse>): MsgSendResponse {
    const message = { ...baseMsgSendResponse } as MsgSendResponse;
    return message;
  },
};

const baseMsgRegisterPointer: object = {
  sender: "",
  pointerType: 0,
  ercAddress: "",
};

export const MsgRegisterPointer = {
  encode(
    message: MsgRegisterPointer,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.pointerType !== 0) {
      writer.uint32(16).int32(message.pointerType);
    }
    if (message.ercAddress !== "") {
      writer.uint32(26).string(message.ercAddress);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgRegisterPointer {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgRegisterPointer } as MsgRegisterPointer;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.pointerType = reader.int32() as any;
          break;
        case 3:
          message.ercAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRegisterPointer {
    const message = { ...baseMsgRegisterPointer } as MsgRegisterPointer;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.pointerType !== undefined && object.pointerType !== null) {
      message.pointerType = pointerTypeFromJSON(object.pointerType);
    } else {
      message.pointerType = 0;
    }
    if (object.ercAddress !== undefined && object.ercAddress !== null) {
      message.ercAddress = String(object.ercAddress);
    } else {
      message.ercAddress = "";
    }
    return message;
  },

  toJSON(message: MsgRegisterPointer): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.pointerType !== undefined &&
      (obj.pointerType = pointerTypeToJSON(message.pointerType));
    message.ercAddress !== undefined && (obj.ercAddress = message.ercAddress);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgRegisterPointer>): MsgRegisterPointer {
    const message = { ...baseMsgRegisterPointer } as MsgRegisterPointer;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.pointerType !== undefined && object.pointerType !== null) {
      message.pointerType = object.pointerType;
    } else {
      message.pointerType = 0;
    }
    if (object.ercAddress !== undefined && object.ercAddress !== null) {
      message.ercAddress = object.ercAddress;
    } else {
      message.ercAddress = "";
    }
    return message;
  },
};

const baseMsgRegisterPointerResponse: object = { pointerAddress: "" };

export const MsgRegisterPointerResponse = {
  encode(
    message: MsgRegisterPointerResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.pointerAddress !== "") {
      writer.uint32(10).string(message.pointerAddress);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgRegisterPointerResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgRegisterPointerResponse,
    } as MsgRegisterPointerResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pointerAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgRegisterPointerResponse {
    const message = {
      ...baseMsgRegisterPointerResponse,
    } as MsgRegisterPointerResponse;
    if (object.pointerAddress !== undefined && object.pointerAddress !== null) {
      message.pointerAddress = String(object.pointerAddress);
    } else {
      message.pointerAddress = "";
    }
    return message;
  },

  toJSON(message: MsgRegisterPointerResponse): unknown {
    const obj: any = {};
    message.pointerAddress !== undefined &&
      (obj.pointerAddress = message.pointerAddress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgRegisterPointerResponse>
  ): MsgRegisterPointerResponse {
    const message = {
      ...baseMsgRegisterPointerResponse,
    } as MsgRegisterPointerResponse;
    if (object.pointerAddress !== undefined && object.pointerAddress !== null) {
      message.pointerAddress = object.pointerAddress;
    } else {
      message.pointerAddress = "";
    }
    return message;
  },
};

const baseMsgAssociateContractAddress: object = { sender: "", address: "" };

export const MsgAssociateContractAddress = {
  encode(
    message: MsgAssociateContractAddress,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.address !== "") {
      writer.uint32(18).string(message.address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgAssociateContractAddress {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgAssociateContractAddress,
    } as MsgAssociateContractAddress;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgAssociateContractAddress {
    const message = {
      ...baseMsgAssociateContractAddress,
    } as MsgAssociateContractAddress;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    return message;
  },

  toJSON(message: MsgAssociateContractAddress): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.address !== undefined && (obj.address = message.address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgAssociateContractAddress>
  ): MsgAssociateContractAddress {
    const message = {
      ...baseMsgAssociateContractAddress,
    } as MsgAssociateContractAddress;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    return message;
  },
};

const baseMsgAssociateContractAddressResponse: object = {};

export const MsgAssociateContractAddressResponse = {
  encode(
    _: MsgAssociateContractAddressResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgAssociateContractAddressResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgAssociateContractAddressResponse,
    } as MsgAssociateContractAddressResponse;
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

  fromJSON(_: any): MsgAssociateContractAddressResponse {
    const message = {
      ...baseMsgAssociateContractAddressResponse,
    } as MsgAssociateContractAddressResponse;
    return message;
  },

  toJSON(_: MsgAssociateContractAddressResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgAssociateContractAddressResponse>
  ): MsgAssociateContractAddressResponse {
    const message = {
      ...baseMsgAssociateContractAddressResponse,
    } as MsgAssociateContractAddressResponse;
    return message;
  },
};

const baseMsgAssociate: object = { sender: "", customMessage: "" };

export const MsgAssociate = {
  encode(message: MsgAssociate, writer: Writer = Writer.create()): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.customMessage !== "") {
      writer.uint32(18).string(message.customMessage);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgAssociate {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgAssociate } as MsgAssociate;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.customMessage = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgAssociate {
    const message = { ...baseMsgAssociate } as MsgAssociate;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.customMessage !== undefined && object.customMessage !== null) {
      message.customMessage = String(object.customMessage);
    } else {
      message.customMessage = "";
    }
    return message;
  },

  toJSON(message: MsgAssociate): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.customMessage !== undefined &&
      (obj.customMessage = message.customMessage);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgAssociate>): MsgAssociate {
    const message = { ...baseMsgAssociate } as MsgAssociate;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.customMessage !== undefined && object.customMessage !== null) {
      message.customMessage = object.customMessage;
    } else {
      message.customMessage = "";
    }
    return message;
  },
};

const baseMsgAssociateResponse: object = {};

export const MsgAssociateResponse = {
  encode(_: MsgAssociateResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgAssociateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgAssociateResponse } as MsgAssociateResponse;
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

  fromJSON(_: any): MsgAssociateResponse {
    const message = { ...baseMsgAssociateResponse } as MsgAssociateResponse;
    return message;
  },

  toJSON(_: MsgAssociateResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgAssociateResponse>): MsgAssociateResponse {
    const message = { ...baseMsgAssociateResponse } as MsgAssociateResponse;
    return message;
  },
};

export interface Msg {
  EVMTransaction(
    request: MsgEVMTransaction
  ): Promise<MsgEVMTransactionResponse>;
  Send(request: MsgSend): Promise<MsgSendResponse>;
  RegisterPointer(
    request: MsgRegisterPointer
  ): Promise<MsgRegisterPointerResponse>;
  AssociateContractAddress(
    request: MsgAssociateContractAddress
  ): Promise<MsgAssociateContractAddressResponse>;
  Associate(request: MsgAssociate): Promise<MsgAssociateResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  EVMTransaction(
    request: MsgEVMTransaction
  ): Promise<MsgEVMTransactionResponse> {
    const data = MsgEVMTransaction.encode(request).finish();
    const promise = this.rpc.request("sei.evm.v1.Msg", "EVMTransaction", data);
    return promise.then((data) =>
      MsgEVMTransactionResponse.decode(new Reader(data))
    );
  }

  Send(request: MsgSend): Promise<MsgSendResponse> {
    const data = MsgSend.encode(request).finish();
    const promise = this.rpc.request("sei.evm.v1.Msg", "Send", data);
    return promise.then((data) => MsgSendResponse.decode(new Reader(data)));
  }

  RegisterPointer(
    request: MsgRegisterPointer
  ): Promise<MsgRegisterPointerResponse> {
    const data = MsgRegisterPointer.encode(request).finish();
    const promise = this.rpc.request("sei.evm.v1.Msg", "RegisterPointer", data);
    return promise.then((data) =>
      MsgRegisterPointerResponse.decode(new Reader(data))
    );
  }

  AssociateContractAddress(
    request: MsgAssociateContractAddress
  ): Promise<MsgAssociateContractAddressResponse> {
    const data = MsgAssociateContractAddress.encode(request).finish();
    const promise = this.rpc.request(
      "sei.evm.v1.Msg",
      "AssociateContractAddress",
      data
    );
    return promise.then((data) =>
      MsgAssociateContractAddressResponse.decode(new Reader(data))
    );
  }

  Associate(request: MsgAssociate): Promise<MsgAssociateResponse> {
    const data = MsgAssociate.encode(request).finish();
    const promise = this.rpc.request("sei.evm.v1.Msg", "Associate", data);
    return promise.then((data) =>
      MsgAssociateResponse.decode(new Reader(data))
    );
  }
}

interface Rpc {
  request(
    service: string,
    method: string,
    data: Uint8Array
  ): Promise<Uint8Array>;
}

declare var self: any | undefined;
declare var window: any | undefined;
var globalThis: any = (() => {
  if (typeof globalThis !== "undefined") return globalThis;
  if (typeof self !== "undefined") return self;
  if (typeof window !== "undefined") return window;
  if (typeof global !== "undefined") return global;
  throw "Unable to locate global object";
})();

const atob: (b64: string) => string =
  globalThis.atob ||
  ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64: string): Uint8Array {
  const bin = atob(b64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; ++i) {
    arr[i] = bin.charCodeAt(i);
  }
  return arr;
}

const btoa: (bin: string) => string =
  globalThis.btoa ||
  ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr: Uint8Array): string {
  const bin: string[] = [];
  for (let i = 0; i < arr.byteLength; ++i) {
    bin.push(String.fromCharCode(arr[i]));
  }
  return btoa(bin.join(""));
}

type Builtin = Date | Function | Uint8Array | string | number | undefined;
export type DeepPartial<T> = T extends Builtin
  ? T
  : T extends Array<infer U>
  ? Array<DeepPartial<U>>
  : T extends ReadonlyArray<infer U>
  ? ReadonlyArray<DeepPartial<U>>
  : T extends {}
  ? { [K in keyof T]?: DeepPartial<T[K]> }
  : Partial<T>;

function longToNumber(long: Long): number {
  if (long.gt(Number.MAX_SAFE_INTEGER)) {
    throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
  }
  return long.toNumber();
}

if (util.Long !== Long) {
  util.Long = Long as any;
  configure();
}
