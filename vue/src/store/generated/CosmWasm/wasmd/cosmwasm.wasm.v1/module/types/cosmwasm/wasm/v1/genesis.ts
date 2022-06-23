/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import {
  Params,
  CodeInfo,
  ContractInfo,
  Model,
} from "../../../cosmwasm/wasm/v1/types";
import {
  MsgStoreCode,
  MsgInstantiateContract,
  MsgExecuteContract,
} from "../../../cosmwasm/wasm/v1/tx";

export const protobufPackage = "cosmwasm.wasm.v1";

/** GenesisState - genesis state of x/wasm */
export interface GenesisState {
  params: Params | undefined;
  codes: Code[];
  contracts: Contract[];
  sequences: Sequence[];
  genMsgs: GenesisState_GenMsgs[];
}

/**
 * GenMsgs define the messages that can be executed during genesis phase in
 * order. The intention is to have more human readable data that is auditable.
 */
export interface GenesisState_GenMsgs {
  storeCode: MsgStoreCode | undefined;
  instantiateContract: MsgInstantiateContract | undefined;
  executeContract: MsgExecuteContract | undefined;
}

/** Code struct encompasses CodeInfo and CodeBytes */
export interface Code {
  codeId: number;
  codeInfo: CodeInfo | undefined;
  codeBytes: Uint8Array;
  /** Pinned to wasmvm cache */
  pinned: boolean;
}

/** Contract struct encompasses ContractAddress, ContractInfo, and ContractState */
export interface Contract {
  contractAddress: string;
  contractInfo: ContractInfo | undefined;
  contractState: Model[];
}

/** Sequence key and value of an id generation counter */
export interface Sequence {
  idKey: Uint8Array;
  value: number;
}

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.codes) {
      Code.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.contracts) {
      Contract.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.sequences) {
      Sequence.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.genMsgs) {
      GenesisState_GenMsgs.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.codes = [];
    message.contracts = [];
    message.sequences = [];
    message.genMsgs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.codes.push(Code.decode(reader, reader.uint32()));
          break;
        case 3:
          message.contracts.push(Contract.decode(reader, reader.uint32()));
          break;
        case 4:
          message.sequences.push(Sequence.decode(reader, reader.uint32()));
          break;
        case 5:
          message.genMsgs.push(
            GenesisState_GenMsgs.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.codes = [];
    message.contracts = [];
    message.sequences = [];
    message.genMsgs = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.codes !== undefined && object.codes !== null) {
      for (const e of object.codes) {
        message.codes.push(Code.fromJSON(e));
      }
    }
    if (object.contracts !== undefined && object.contracts !== null) {
      for (const e of object.contracts) {
        message.contracts.push(Contract.fromJSON(e));
      }
    }
    if (object.sequences !== undefined && object.sequences !== null) {
      for (const e of object.sequences) {
        message.sequences.push(Sequence.fromJSON(e));
      }
    }
    if (object.genMsgs !== undefined && object.genMsgs !== null) {
      for (const e of object.genMsgs) {
        message.genMsgs.push(GenesisState_GenMsgs.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.codes) {
      obj.codes = message.codes.map((e) => (e ? Code.toJSON(e) : undefined));
    } else {
      obj.codes = [];
    }
    if (message.contracts) {
      obj.contracts = message.contracts.map((e) =>
        e ? Contract.toJSON(e) : undefined
      );
    } else {
      obj.contracts = [];
    }
    if (message.sequences) {
      obj.sequences = message.sequences.map((e) =>
        e ? Sequence.toJSON(e) : undefined
      );
    } else {
      obj.sequences = [];
    }
    if (message.genMsgs) {
      obj.genMsgs = message.genMsgs.map((e) =>
        e ? GenesisState_GenMsgs.toJSON(e) : undefined
      );
    } else {
      obj.genMsgs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.codes = [];
    message.contracts = [];
    message.sequences = [];
    message.genMsgs = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.codes !== undefined && object.codes !== null) {
      for (const e of object.codes) {
        message.codes.push(Code.fromPartial(e));
      }
    }
    if (object.contracts !== undefined && object.contracts !== null) {
      for (const e of object.contracts) {
        message.contracts.push(Contract.fromPartial(e));
      }
    }
    if (object.sequences !== undefined && object.sequences !== null) {
      for (const e of object.sequences) {
        message.sequences.push(Sequence.fromPartial(e));
      }
    }
    if (object.genMsgs !== undefined && object.genMsgs !== null) {
      for (const e of object.genMsgs) {
        message.genMsgs.push(GenesisState_GenMsgs.fromPartial(e));
      }
    }
    return message;
  },
};

const baseGenesisState_GenMsgs: object = {};

export const GenesisState_GenMsgs = {
  encode(
    message: GenesisState_GenMsgs,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.storeCode !== undefined) {
      MsgStoreCode.encode(message.storeCode, writer.uint32(10).fork()).ldelim();
    }
    if (message.instantiateContract !== undefined) {
      MsgInstantiateContract.encode(
        message.instantiateContract,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.executeContract !== undefined) {
      MsgExecuteContract.encode(
        message.executeContract,
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState_GenMsgs {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState_GenMsgs } as GenesisState_GenMsgs;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.storeCode = MsgStoreCode.decode(reader, reader.uint32());
          break;
        case 2:
          message.instantiateContract = MsgInstantiateContract.decode(
            reader,
            reader.uint32()
          );
          break;
        case 3:
          message.executeContract = MsgExecuteContract.decode(
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

  fromJSON(object: any): GenesisState_GenMsgs {
    const message = { ...baseGenesisState_GenMsgs } as GenesisState_GenMsgs;
    if (object.storeCode !== undefined && object.storeCode !== null) {
      message.storeCode = MsgStoreCode.fromJSON(object.storeCode);
    } else {
      message.storeCode = undefined;
    }
    if (
      object.instantiateContract !== undefined &&
      object.instantiateContract !== null
    ) {
      message.instantiateContract = MsgInstantiateContract.fromJSON(
        object.instantiateContract
      );
    } else {
      message.instantiateContract = undefined;
    }
    if (
      object.executeContract !== undefined &&
      object.executeContract !== null
    ) {
      message.executeContract = MsgExecuteContract.fromJSON(
        object.executeContract
      );
    } else {
      message.executeContract = undefined;
    }
    return message;
  },

  toJSON(message: GenesisState_GenMsgs): unknown {
    const obj: any = {};
    message.storeCode !== undefined &&
      (obj.storeCode = message.storeCode
        ? MsgStoreCode.toJSON(message.storeCode)
        : undefined);
    message.instantiateContract !== undefined &&
      (obj.instantiateContract = message.instantiateContract
        ? MsgInstantiateContract.toJSON(message.instantiateContract)
        : undefined);
    message.executeContract !== undefined &&
      (obj.executeContract = message.executeContract
        ? MsgExecuteContract.toJSON(message.executeContract)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState_GenMsgs>): GenesisState_GenMsgs {
    const message = { ...baseGenesisState_GenMsgs } as GenesisState_GenMsgs;
    if (object.storeCode !== undefined && object.storeCode !== null) {
      message.storeCode = MsgStoreCode.fromPartial(object.storeCode);
    } else {
      message.storeCode = undefined;
    }
    if (
      object.instantiateContract !== undefined &&
      object.instantiateContract !== null
    ) {
      message.instantiateContract = MsgInstantiateContract.fromPartial(
        object.instantiateContract
      );
    } else {
      message.instantiateContract = undefined;
    }
    if (
      object.executeContract !== undefined &&
      object.executeContract !== null
    ) {
      message.executeContract = MsgExecuteContract.fromPartial(
        object.executeContract
      );
    } else {
      message.executeContract = undefined;
    }
    return message;
  },
};

const baseCode: object = { codeId: 0, pinned: false };

export const Code = {
  encode(message: Code, writer: Writer = Writer.create()): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    if (message.codeInfo !== undefined) {
      CodeInfo.encode(message.codeInfo, writer.uint32(18).fork()).ldelim();
    }
    if (message.codeBytes.length !== 0) {
      writer.uint32(26).bytes(message.codeBytes);
    }
    if (message.pinned === true) {
      writer.uint32(32).bool(message.pinned);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Code {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCode } as Code;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.codeInfo = CodeInfo.decode(reader, reader.uint32());
          break;
        case 3:
          message.codeBytes = reader.bytes();
          break;
        case 4:
          message.pinned = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Code {
    const message = { ...baseCode } as Code;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.codeInfo !== undefined && object.codeInfo !== null) {
      message.codeInfo = CodeInfo.fromJSON(object.codeInfo);
    } else {
      message.codeInfo = undefined;
    }
    if (object.codeBytes !== undefined && object.codeBytes !== null) {
      message.codeBytes = bytesFromBase64(object.codeBytes);
    }
    if (object.pinned !== undefined && object.pinned !== null) {
      message.pinned = Boolean(object.pinned);
    } else {
      message.pinned = false;
    }
    return message;
  },

  toJSON(message: Code): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.codeInfo !== undefined &&
      (obj.codeInfo = message.codeInfo
        ? CodeInfo.toJSON(message.codeInfo)
        : undefined);
    message.codeBytes !== undefined &&
      (obj.codeBytes = base64FromBytes(
        message.codeBytes !== undefined ? message.codeBytes : new Uint8Array()
      ));
    message.pinned !== undefined && (obj.pinned = message.pinned);
    return obj;
  },

  fromPartial(object: DeepPartial<Code>): Code {
    const message = { ...baseCode } as Code;
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.codeInfo !== undefined && object.codeInfo !== null) {
      message.codeInfo = CodeInfo.fromPartial(object.codeInfo);
    } else {
      message.codeInfo = undefined;
    }
    if (object.codeBytes !== undefined && object.codeBytes !== null) {
      message.codeBytes = object.codeBytes;
    } else {
      message.codeBytes = new Uint8Array();
    }
    if (object.pinned !== undefined && object.pinned !== null) {
      message.pinned = object.pinned;
    } else {
      message.pinned = false;
    }
    return message;
  },
};

const baseContract: object = { contractAddress: "" };

export const Contract = {
  encode(message: Contract, writer: Writer = Writer.create()): Writer {
    if (message.contractAddress !== "") {
      writer.uint32(10).string(message.contractAddress);
    }
    if (message.contractInfo !== undefined) {
      ContractInfo.encode(
        message.contractInfo,
        writer.uint32(18).fork()
      ).ldelim();
    }
    for (const v of message.contractState) {
      Model.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Contract {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContract } as Contract;
    message.contractState = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddress = reader.string();
          break;
        case 2:
          message.contractInfo = ContractInfo.decode(reader, reader.uint32());
          break;
        case 3:
          message.contractState.push(Model.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Contract {
    const message = { ...baseContract } as Contract;
    message.contractState = [];
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
    }
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfo.fromJSON(object.contractInfo);
    } else {
      message.contractInfo = undefined;
    }
    if (object.contractState !== undefined && object.contractState !== null) {
      for (const e of object.contractState) {
        message.contractState.push(Model.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Contract): unknown {
    const obj: any = {};
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    message.contractInfo !== undefined &&
      (obj.contractInfo = message.contractInfo
        ? ContractInfo.toJSON(message.contractInfo)
        : undefined);
    if (message.contractState) {
      obj.contractState = message.contractState.map((e) =>
        e ? Model.toJSON(e) : undefined
      );
    } else {
      obj.contractState = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Contract>): Contract {
    const message = { ...baseContract } as Contract;
    message.contractState = [];
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
    }
    if (object.contractInfo !== undefined && object.contractInfo !== null) {
      message.contractInfo = ContractInfo.fromPartial(object.contractInfo);
    } else {
      message.contractInfo = undefined;
    }
    if (object.contractState !== undefined && object.contractState !== null) {
      for (const e of object.contractState) {
        message.contractState.push(Model.fromPartial(e));
      }
    }
    return message;
  },
};

const baseSequence: object = { value: 0 };

export const Sequence = {
  encode(message: Sequence, writer: Writer = Writer.create()): Writer {
    if (message.idKey.length !== 0) {
      writer.uint32(10).bytes(message.idKey);
    }
    if (message.value !== 0) {
      writer.uint32(16).uint64(message.value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Sequence {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSequence } as Sequence;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.idKey = reader.bytes();
          break;
        case 2:
          message.value = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Sequence {
    const message = { ...baseSequence } as Sequence;
    if (object.idKey !== undefined && object.idKey !== null) {
      message.idKey = bytesFromBase64(object.idKey);
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = Number(object.value);
    } else {
      message.value = 0;
    }
    return message;
  },

  toJSON(message: Sequence): unknown {
    const obj: any = {};
    message.idKey !== undefined &&
      (obj.idKey = base64FromBytes(
        message.idKey !== undefined ? message.idKey : new Uint8Array()
      ));
    message.value !== undefined && (obj.value = message.value);
    return obj;
  },

  fromPartial(object: DeepPartial<Sequence>): Sequence {
    const message = { ...baseSequence } as Sequence;
    if (object.idKey !== undefined && object.idKey !== null) {
      message.idKey = object.idKey;
    } else {
      message.idKey = new Uint8Array();
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = 0;
    }
    return message;
  },
};

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
