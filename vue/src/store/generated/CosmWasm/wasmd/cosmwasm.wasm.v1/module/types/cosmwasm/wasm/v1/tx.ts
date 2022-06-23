/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import { AccessConfig } from "../../../cosmwasm/wasm/v1/types";
import { Coin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmwasm.wasm.v1";

/** MsgStoreCode submit Wasm code to the system */
export interface MsgStoreCode {
  /** Sender is the that actor that signed the messages */
  sender: string;
  /** WASMByteCode can be raw or gzip compressed */
  wasmByteCode: Uint8Array;
  /**
   * InstantiatePermission access control to apply on contract creation,
   * optional
   */
  instantiatePermission: AccessConfig | undefined;
}

/** MsgStoreCodeResponse returns store result data. */
export interface MsgStoreCodeResponse {
  /** CodeID is the reference to the stored WASM code */
  codeId: number;
}

/**
 * MsgInstantiateContract create a new smart contract instance for the given
 * code id.
 */
export interface MsgInstantiateContract {
  /** Sender is the that actor that signed the messages */
  sender: string;
  /** Admin is an optional address that can execute migrations */
  admin: string;
  /** CodeID is the reference to the stored WASM code */
  codeId: number;
  /** Label is optional metadata to be stored with a contract instance. */
  label: string;
  /** Msg json encoded message to be passed to the contract on instantiation */
  msg: Uint8Array;
  /** Funds coins that are transferred to the contract on instantiation */
  funds: Coin[];
}

/** MsgInstantiateContractResponse return instantiation result data */
export interface MsgInstantiateContractResponse {
  /** Address is the bech32 address of the new contract instance. */
  address: string;
  /** Data contains base64-encoded bytes to returned from the contract */
  data: Uint8Array;
}

/** MsgExecuteContract submits the given message data to a smart contract */
export interface MsgExecuteContract {
  /** Sender is the that actor that signed the messages */
  sender: string;
  /** Contract is the address of the smart contract */
  contract: string;
  /** Msg json encoded message to be passed to the contract */
  msg: Uint8Array;
  /** Funds coins that are transferred to the contract on execution */
  funds: Coin[];
}

/** MsgExecuteContractResponse returns execution result data. */
export interface MsgExecuteContractResponse {
  /** Data contains base64-encoded bytes to returned from the contract */
  data: Uint8Array;
}

/** MsgMigrateContract runs a code upgrade/ downgrade for a smart contract */
export interface MsgMigrateContract {
  /** Sender is the that actor that signed the messages */
  sender: string;
  /** Contract is the address of the smart contract */
  contract: string;
  /** CodeID references the new WASM code */
  codeId: number;
  /** Msg json encoded message to be passed to the contract on migration */
  msg: Uint8Array;
}

/** MsgMigrateContractResponse returns contract migration result data. */
export interface MsgMigrateContractResponse {
  /**
   * Data contains same raw bytes returned as data from the wasm contract.
   * (May be empty)
   */
  data: Uint8Array;
}

/** MsgUpdateAdmin sets a new admin for a smart contract */
export interface MsgUpdateAdmin {
  /** Sender is the that actor that signed the messages */
  sender: string;
  /** NewAdmin address to be set */
  newAdmin: string;
  /** Contract is the address of the smart contract */
  contract: string;
}

/** MsgUpdateAdminResponse returns empty data */
export interface MsgUpdateAdminResponse {}

/** MsgClearAdmin removes any admin stored for a smart contract */
export interface MsgClearAdmin {
  /** Sender is the that actor that signed the messages */
  sender: string;
  /** Contract is the address of the smart contract */
  contract: string;
}

/** MsgClearAdminResponse returns empty data */
export interface MsgClearAdminResponse {}

const baseMsgStoreCode: object = { sender: "" };

export const MsgStoreCode = {
  encode(message: MsgStoreCode, writer: Writer = Writer.create()): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.wasmByteCode.length !== 0) {
      writer.uint32(18).bytes(message.wasmByteCode);
    }
    if (message.instantiatePermission !== undefined) {
      AccessConfig.encode(
        message.instantiatePermission,
        writer.uint32(42).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgStoreCode {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgStoreCode } as MsgStoreCode;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.wasmByteCode = reader.bytes();
          break;
        case 5:
          message.instantiatePermission = AccessConfig.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgStoreCode {
    const message = { ...baseMsgStoreCode } as MsgStoreCode;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.wasmByteCode !== undefined && object.wasmByteCode !== null) {
      message.wasmByteCode = bytesFromBase64(object.wasmByteCode);
    }
    if (
      object.instantiatePermission !== undefined &&
      object.instantiatePermission !== null
    ) {
      message.instantiatePermission = AccessConfig.fromJSON(
        object.instantiatePermission
      );
    } else {
      message.instantiatePermission = undefined;
    }
    return message;
  },

  toJSON(message: MsgStoreCode): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.wasmByteCode !== undefined &&
      (obj.wasmByteCode = base64FromBytes(
        message.wasmByteCode !== undefined
          ? message.wasmByteCode
          : new Uint8Array()
      ));
    message.instantiatePermission !== undefined &&
      (obj.instantiatePermission = message.instantiatePermission
        ? AccessConfig.toJSON(message.instantiatePermission)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgStoreCode>): MsgStoreCode {
    const message = { ...baseMsgStoreCode } as MsgStoreCode;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.wasmByteCode !== undefined && object.wasmByteCode !== null) {
      message.wasmByteCode = object.wasmByteCode;
    } else {
      message.wasmByteCode = new Uint8Array();
    }
    if (
      object.instantiatePermission !== undefined &&
      object.instantiatePermission !== null
    ) {
      message.instantiatePermission = AccessConfig.fromPartial(
        object.instantiatePermission
      );
    } else {
      message.instantiatePermission = undefined;
    }
    return message;
  },
};

const baseMsgStoreCodeResponse: object = { codeId: 0 };

export const MsgStoreCodeResponse = {
  encode(
    message: MsgStoreCodeResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgStoreCodeResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgStoreCodeResponse } as MsgStoreCodeResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgStoreCodeResponse {
    const message = { ...baseMsgStoreCodeResponse } as MsgStoreCodeResponse;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    return message;
  },

  toJSON(message: MsgStoreCodeResponse): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgStoreCodeResponse>): MsgStoreCodeResponse {
    const message = { ...baseMsgStoreCodeResponse } as MsgStoreCodeResponse;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    return message;
  },
};

const baseMsgInstantiateContract: object = {
  sender: "",
  admin: "",
  codeId: 0,
  label: "",
};

export const MsgInstantiateContract = {
  encode(
    message: MsgInstantiateContract,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.admin !== "") {
      writer.uint32(18).string(message.admin);
    }
    if (message.codeId !== 0) {
      writer.uint32(24).uint64(message.codeId);
    }
    if (message.label !== "") {
      writer.uint32(34).string(message.label);
    }
    if (message.msg.length !== 0) {
      writer.uint32(42).bytes(message.msg);
    }
    for (const v of message.funds) {
      Coin.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgInstantiateContract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgInstantiateContract } as MsgInstantiateContract;
    message.funds = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.admin = reader.string();
          break;
        case 3:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.label = reader.string();
          break;
        case 5:
          message.msg = reader.bytes();
          break;
        case 6:
          message.funds.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgInstantiateContract {
    const message = { ...baseMsgInstantiateContract } as MsgInstantiateContract;
    message.funds = [];
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.admin !== undefined && object.admin !== null) {
      message.admin = String(object.admin);
    } else {
      message.admin = "";
    }
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.label !== undefined && object.label !== null) {
      message.label = String(object.label);
    } else {
      message.label = "";
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = bytesFromBase64(object.msg);
    }
    if (object.funds !== undefined && object.funds !== null) {
      for (const e of object.funds) {
        message.funds.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgInstantiateContract): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.admin !== undefined && (obj.admin = message.admin);
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.label !== undefined && (obj.label = message.label);
    message.msg !== undefined &&
      (obj.msg = base64FromBytes(
        message.msg !== undefined ? message.msg : new Uint8Array()
      ));
    if (message.funds) {
      obj.funds = message.funds.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.funds = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgInstantiateContract>
  ): MsgInstantiateContract {
    const message = { ...baseMsgInstantiateContract } as MsgInstantiateContract;
    message.funds = [];
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.admin !== undefined && object.admin !== null) {
      message.admin = object.admin;
    } else {
      message.admin = "";
    }
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.label !== undefined && object.label !== null) {
      message.label = object.label;
    } else {
      message.label = "";
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = object.msg;
    } else {
      message.msg = new Uint8Array();
    }
    if (object.funds !== undefined && object.funds !== null) {
      for (const e of object.funds) {
        message.funds.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgInstantiateContractResponse: object = { address: "" };

export const MsgInstantiateContractResponse = {
  encode(
    message: MsgInstantiateContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.data.length !== 0) {
      writer.uint32(18).bytes(message.data);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgInstantiateContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgInstantiateContractResponse,
    } as MsgInstantiateContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.data = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgInstantiateContractResponse {
    const message = {
      ...baseMsgInstantiateContractResponse,
    } as MsgInstantiateContractResponse;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: MsgInstantiateContractResponse): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgInstantiateContractResponse>
  ): MsgInstantiateContractResponse {
    const message = {
      ...baseMsgInstantiateContractResponse,
    } as MsgInstantiateContractResponse;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseMsgExecuteContract: object = { sender: "", contract: "" };

export const MsgExecuteContract = {
  encode(
    message: MsgExecuteContract,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.contract !== "") {
      writer.uint32(18).string(message.contract);
    }
    if (message.msg.length !== 0) {
      writer.uint32(26).bytes(message.msg);
    }
    for (const v of message.funds) {
      Coin.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgExecuteContract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgExecuteContract } as MsgExecuteContract;
    message.funds = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.contract = reader.string();
          break;
        case 3:
          message.msg = reader.bytes();
          break;
        case 5:
          message.funds.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgExecuteContract {
    const message = { ...baseMsgExecuteContract } as MsgExecuteContract;
    message.funds = [];
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = String(object.contract);
    } else {
      message.contract = "";
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = bytesFromBase64(object.msg);
    }
    if (object.funds !== undefined && object.funds !== null) {
      for (const e of object.funds) {
        message.funds.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MsgExecuteContract): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.contract !== undefined && (obj.contract = message.contract);
    message.msg !== undefined &&
      (obj.msg = base64FromBytes(
        message.msg !== undefined ? message.msg : new Uint8Array()
      ));
    if (message.funds) {
      obj.funds = message.funds.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.funds = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MsgExecuteContract>): MsgExecuteContract {
    const message = { ...baseMsgExecuteContract } as MsgExecuteContract;
    message.funds = [];
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = object.contract;
    } else {
      message.contract = "";
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = object.msg;
    } else {
      message.msg = new Uint8Array();
    }
    if (object.funds !== undefined && object.funds !== null) {
      for (const e of object.funds) {
        message.funds.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMsgExecuteContractResponse: object = {};

export const MsgExecuteContractResponse = {
  encode(
    message: MsgExecuteContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.data.length !== 0) {
      writer.uint32(10).bytes(message.data);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgExecuteContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgExecuteContractResponse,
    } as MsgExecuteContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.data = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgExecuteContractResponse {
    const message = {
      ...baseMsgExecuteContractResponse,
    } as MsgExecuteContractResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: MsgExecuteContractResponse): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgExecuteContractResponse>
  ): MsgExecuteContractResponse {
    const message = {
      ...baseMsgExecuteContractResponse,
    } as MsgExecuteContractResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseMsgMigrateContract: object = { sender: "", contract: "", codeId: 0 };

export const MsgMigrateContract = {
  encode(
    message: MsgMigrateContract,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.contract !== "") {
      writer.uint32(18).string(message.contract);
    }
    if (message.codeId !== 0) {
      writer.uint32(24).uint64(message.codeId);
    }
    if (message.msg.length !== 0) {
      writer.uint32(34).bytes(message.msg);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgMigrateContract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgMigrateContract } as MsgMigrateContract;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.contract = reader.string();
          break;
        case 3:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.msg = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgMigrateContract {
    const message = { ...baseMsgMigrateContract } as MsgMigrateContract;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = String(object.contract);
    } else {
      message.contract = "";
    }
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = bytesFromBase64(object.msg);
    }
    return message;
  },

  toJSON(message: MsgMigrateContract): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.contract !== undefined && (obj.contract = message.contract);
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.msg !== undefined &&
      (obj.msg = base64FromBytes(
        message.msg !== undefined ? message.msg : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<MsgMigrateContract>): MsgMigrateContract {
    const message = { ...baseMsgMigrateContract } as MsgMigrateContract;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = object.contract;
    } else {
      message.contract = "";
    }
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = object.msg;
    } else {
      message.msg = new Uint8Array();
    }
    return message;
  },
};

const baseMsgMigrateContractResponse: object = {};

export const MsgMigrateContractResponse = {
  encode(
    message: MsgMigrateContractResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.data.length !== 0) {
      writer.uint32(10).bytes(message.data);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgMigrateContractResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgMigrateContractResponse,
    } as MsgMigrateContractResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.data = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgMigrateContractResponse {
    const message = {
      ...baseMsgMigrateContractResponse,
    } as MsgMigrateContractResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    return message;
  },

  toJSON(message: MsgMigrateContractResponse): unknown {
    const obj: any = {};
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgMigrateContractResponse>
  ): MsgMigrateContractResponse {
    const message = {
      ...baseMsgMigrateContractResponse,
    } as MsgMigrateContractResponse;
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    return message;
  },
};

const baseMsgUpdateAdmin: object = { sender: "", newAdmin: "", contract: "" };

export const MsgUpdateAdmin = {
  encode(message: MsgUpdateAdmin, writer: Writer = Writer.create()): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.newAdmin !== "") {
      writer.uint32(18).string(message.newAdmin);
    }
    if (message.contract !== "") {
      writer.uint32(26).string(message.contract);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUpdateAdmin {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUpdateAdmin } as MsgUpdateAdmin;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 2:
          message.newAdmin = reader.string();
          break;
        case 3:
          message.contract = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUpdateAdmin {
    const message = { ...baseMsgUpdateAdmin } as MsgUpdateAdmin;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.newAdmin !== undefined && object.newAdmin !== null) {
      message.newAdmin = String(object.newAdmin);
    } else {
      message.newAdmin = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = String(object.contract);
    } else {
      message.contract = "";
    }
    return message;
  },

  toJSON(message: MsgUpdateAdmin): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.newAdmin !== undefined && (obj.newAdmin = message.newAdmin);
    message.contract !== undefined && (obj.contract = message.contract);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgUpdateAdmin>): MsgUpdateAdmin {
    const message = { ...baseMsgUpdateAdmin } as MsgUpdateAdmin;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.newAdmin !== undefined && object.newAdmin !== null) {
      message.newAdmin = object.newAdmin;
    } else {
      message.newAdmin = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = object.contract;
    } else {
      message.contract = "";
    }
    return message;
  },
};

const baseMsgUpdateAdminResponse: object = {};

export const MsgUpdateAdminResponse = {
  encode(_: MsgUpdateAdminResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUpdateAdminResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUpdateAdminResponse } as MsgUpdateAdminResponse;
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

  fromJSON(_: any): MsgUpdateAdminResponse {
    const message = { ...baseMsgUpdateAdminResponse } as MsgUpdateAdminResponse;
    return message;
  },

  toJSON(_: MsgUpdateAdminResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgUpdateAdminResponse>): MsgUpdateAdminResponse {
    const message = { ...baseMsgUpdateAdminResponse } as MsgUpdateAdminResponse;
    return message;
  },
};

const baseMsgClearAdmin: object = { sender: "", contract: "" };

export const MsgClearAdmin = {
  encode(message: MsgClearAdmin, writer: Writer = Writer.create()): Writer {
    if (message.sender !== "") {
      writer.uint32(10).string(message.sender);
    }
    if (message.contract !== "") {
      writer.uint32(26).string(message.contract);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgClearAdmin {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgClearAdmin } as MsgClearAdmin;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.sender = reader.string();
          break;
        case 3:
          message.contract = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgClearAdmin {
    const message = { ...baseMsgClearAdmin } as MsgClearAdmin;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = String(object.sender);
    } else {
      message.sender = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = String(object.contract);
    } else {
      message.contract = "";
    }
    return message;
  },

  toJSON(message: MsgClearAdmin): unknown {
    const obj: any = {};
    message.sender !== undefined && (obj.sender = message.sender);
    message.contract !== undefined && (obj.contract = message.contract);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgClearAdmin>): MsgClearAdmin {
    const message = { ...baseMsgClearAdmin } as MsgClearAdmin;
    if (object.sender !== undefined && object.sender !== null) {
      message.sender = object.sender;
    } else {
      message.sender = "";
    }
    if (object.contract !== undefined && object.contract !== null) {
      message.contract = object.contract;
    } else {
      message.contract = "";
    }
    return message;
  },
};

const baseMsgClearAdminResponse: object = {};

export const MsgClearAdminResponse = {
  encode(_: MsgClearAdminResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgClearAdminResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgClearAdminResponse } as MsgClearAdminResponse;
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

  fromJSON(_: any): MsgClearAdminResponse {
    const message = { ...baseMsgClearAdminResponse } as MsgClearAdminResponse;
    return message;
  },

  toJSON(_: MsgClearAdminResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgClearAdminResponse>): MsgClearAdminResponse {
    const message = { ...baseMsgClearAdminResponse } as MsgClearAdminResponse;
    return message;
  },
};

/** Msg defines the wasm Msg service. */
export interface Msg {
  /** StoreCode to submit Wasm code to the system */
  StoreCode(request: MsgStoreCode): Promise<MsgStoreCodeResponse>;
  /** Instantiate creates a new smart contract instance for the given code id. */
  InstantiateContract(
    request: MsgInstantiateContract
  ): Promise<MsgInstantiateContractResponse>;
  /** Execute submits the given message data to a smart contract */
  ExecuteContract(
    request: MsgExecuteContract
  ): Promise<MsgExecuteContractResponse>;
  /** Migrate runs a code upgrade/ downgrade for a smart contract */
  MigrateContract(
    request: MsgMigrateContract
  ): Promise<MsgMigrateContractResponse>;
  /** UpdateAdmin sets a new   admin for a smart contract */
  UpdateAdmin(request: MsgUpdateAdmin): Promise<MsgUpdateAdminResponse>;
  /** ClearAdmin removes any admin stored for a smart contract */
  ClearAdmin(request: MsgClearAdmin): Promise<MsgClearAdminResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  StoreCode(request: MsgStoreCode): Promise<MsgStoreCodeResponse> {
    const data = MsgStoreCode.encode(request).finish();
    const promise = this.rpc.request("cosmwasm.wasm.v1.Msg", "StoreCode", data);
    return promise.then((data) =>
      MsgStoreCodeResponse.decode(new Reader(data))
    );
  }

  InstantiateContract(
    request: MsgInstantiateContract
  ): Promise<MsgInstantiateContractResponse> {
    const data = MsgInstantiateContract.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Msg",
      "InstantiateContract",
      data
    );
    return promise.then((data) =>
      MsgInstantiateContractResponse.decode(new Reader(data))
    );
  }

  ExecuteContract(
    request: MsgExecuteContract
  ): Promise<MsgExecuteContractResponse> {
    const data = MsgExecuteContract.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Msg",
      "ExecuteContract",
      data
    );
    return promise.then((data) =>
      MsgExecuteContractResponse.decode(new Reader(data))
    );
  }

  MigrateContract(
    request: MsgMigrateContract
  ): Promise<MsgMigrateContractResponse> {
    const data = MsgMigrateContract.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Msg",
      "MigrateContract",
      data
    );
    return promise.then((data) =>
      MsgMigrateContractResponse.decode(new Reader(data))
    );
  }

  UpdateAdmin(request: MsgUpdateAdmin): Promise<MsgUpdateAdminResponse> {
    const data = MsgUpdateAdmin.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Msg",
      "UpdateAdmin",
      data
    );
    return promise.then((data) =>
      MsgUpdateAdminResponse.decode(new Reader(data))
    );
  }

  ClearAdmin(request: MsgClearAdmin): Promise<MsgClearAdminResponse> {
    const data = MsgClearAdmin.encode(request).finish();
    const promise = this.rpc.request(
      "cosmwasm.wasm.v1.Msg",
      "ClearAdmin",
      data
    );
    return promise.then((data) =>
      MsgClearAdminResponse.decode(new Reader(data))
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
