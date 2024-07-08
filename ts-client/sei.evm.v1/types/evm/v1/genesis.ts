/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Params } from "../../evm/v1/params";

export const protobufPackage = "sei.evm.v1";

/** AddressAssociation represents an association between a Cosmos and an Ethereum address. */
export interface AddressAssociation {
  /** Sei address */
  seiAddress: string;
  /** Ethereum address */
  ethAddress: string;
}

export interface Code {
  address: string;
  code: Uint8Array;
}

export interface ContractState {
  address: string;
  key: Uint8Array;
  value: Uint8Array;
}

export interface Nonce {
  address: string;
  nonce: number;
}

export interface Serialized {
  prefix: Uint8Array;
  key: Uint8Array;
  value: Uint8Array;
}

/** GenesisState defines the evm module's genesis state. */
export interface GenesisState {
  params: Params | undefined;
  /** List of address associations */
  addressAssociations: AddressAssociation[];
  /** List of stored code */
  codes: Code[];
  /** List of contract state */
  states: ContractState[];
  nonces: Nonce[];
  serialized: Serialized[];
}

const baseAddressAssociation: object = { seiAddress: "", ethAddress: "" };

export const AddressAssociation = {
  encode(
    message: AddressAssociation,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.seiAddress !== "") {
      writer.uint32(10).string(message.seiAddress);
    }
    if (message.ethAddress !== "") {
      writer.uint32(18).string(message.ethAddress);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): AddressAssociation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseAddressAssociation } as AddressAssociation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.seiAddress = reader.string();
          break;
        case 2:
          message.ethAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AddressAssociation {
    const message = { ...baseAddressAssociation } as AddressAssociation;
    if (object.seiAddress !== undefined && object.seiAddress !== null) {
      message.seiAddress = String(object.seiAddress);
    } else {
      message.seiAddress = "";
    }
    if (object.ethAddress !== undefined && object.ethAddress !== null) {
      message.ethAddress = String(object.ethAddress);
    } else {
      message.ethAddress = "";
    }
    return message;
  },

  toJSON(message: AddressAssociation): unknown {
    const obj: any = {};
    message.seiAddress !== undefined && (obj.seiAddress = message.seiAddress);
    message.ethAddress !== undefined && (obj.ethAddress = message.ethAddress);
    return obj;
  },

  fromPartial(object: DeepPartial<AddressAssociation>): AddressAssociation {
    const message = { ...baseAddressAssociation } as AddressAssociation;
    if (object.seiAddress !== undefined && object.seiAddress !== null) {
      message.seiAddress = object.seiAddress;
    } else {
      message.seiAddress = "";
    }
    if (object.ethAddress !== undefined && object.ethAddress !== null) {
      message.ethAddress = object.ethAddress;
    } else {
      message.ethAddress = "";
    }
    return message;
  },
};

const baseCode: object = { address: "" };

export const Code = {
  encode(message: Code, writer: Writer = Writer.create()): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.code.length !== 0) {
      writer.uint32(18).bytes(message.code);
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
          message.address = reader.string();
          break;
        case 2:
          message.code = reader.bytes();
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
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.code !== undefined && object.code !== null) {
      message.code = bytesFromBase64(object.code);
    }
    return message;
  },

  toJSON(message: Code): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.code !== undefined &&
      (obj.code = base64FromBytes(
        message.code !== undefined ? message.code : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Code>): Code {
    const message = { ...baseCode } as Code;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.code !== undefined && object.code !== null) {
      message.code = object.code;
    } else {
      message.code = new Uint8Array();
    }
    return message;
  },
};

const baseContractState: object = { address: "" };

export const ContractState = {
  encode(message: ContractState, writer: Writer = Writer.create()): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.key.length !== 0) {
      writer.uint32(18).bytes(message.key);
    }
    if (message.value.length !== 0) {
      writer.uint32(26).bytes(message.value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractState } as ContractState;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.key = reader.bytes();
          break;
        case 3:
          message.value = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractState {
    const message = { ...baseContractState } as ContractState;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.key !== undefined && object.key !== null) {
      message.key = bytesFromBase64(object.key);
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = bytesFromBase64(object.value);
    }
    return message;
  },

  toJSON(message: ContractState): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.key !== undefined &&
      (obj.key = base64FromBytes(
        message.key !== undefined ? message.key : new Uint8Array()
      ));
    message.value !== undefined &&
      (obj.value = base64FromBytes(
        message.value !== undefined ? message.value : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<ContractState>): ContractState {
    const message = { ...baseContractState } as ContractState;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.key !== undefined && object.key !== null) {
      message.key = object.key;
    } else {
      message.key = new Uint8Array();
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = new Uint8Array();
    }
    return message;
  },
};

const baseNonce: object = { address: "", nonce: 0 };

export const Nonce = {
  encode(message: Nonce, writer: Writer = Writer.create()): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.nonce !== 0) {
      writer.uint32(16).uint64(message.nonce);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Nonce {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseNonce } as Nonce;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.nonce = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Nonce {
    const message = { ...baseNonce } as Nonce;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.nonce !== undefined && object.nonce !== null) {
      message.nonce = Number(object.nonce);
    } else {
      message.nonce = 0;
    }
    return message;
  },

  toJSON(message: Nonce): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.nonce !== undefined && (obj.nonce = message.nonce);
    return obj;
  },

  fromPartial(object: DeepPartial<Nonce>): Nonce {
    const message = { ...baseNonce } as Nonce;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.nonce !== undefined && object.nonce !== null) {
      message.nonce = object.nonce;
    } else {
      message.nonce = 0;
    }
    return message;
  },
};

const baseSerialized: object = {};

export const Serialized = {
  encode(message: Serialized, writer: Writer = Writer.create()): Writer {
    if (message.prefix.length !== 0) {
      writer.uint32(10).bytes(message.prefix);
    }
    if (message.key.length !== 0) {
      writer.uint32(18).bytes(message.key);
    }
    if (message.value.length !== 0) {
      writer.uint32(26).bytes(message.value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Serialized {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSerialized } as Serialized;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.prefix = reader.bytes();
          break;
        case 2:
          message.key = reader.bytes();
          break;
        case 3:
          message.value = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Serialized {
    const message = { ...baseSerialized } as Serialized;
    if (object.prefix !== undefined && object.prefix !== null) {
      message.prefix = bytesFromBase64(object.prefix);
    }
    if (object.key !== undefined && object.key !== null) {
      message.key = bytesFromBase64(object.key);
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = bytesFromBase64(object.value);
    }
    return message;
  },

  toJSON(message: Serialized): unknown {
    const obj: any = {};
    message.prefix !== undefined &&
      (obj.prefix = base64FromBytes(
        message.prefix !== undefined ? message.prefix : new Uint8Array()
      ));
    message.key !== undefined &&
      (obj.key = base64FromBytes(
        message.key !== undefined ? message.key : new Uint8Array()
      ));
    message.value !== undefined &&
      (obj.value = base64FromBytes(
        message.value !== undefined ? message.value : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Serialized>): Serialized {
    const message = { ...baseSerialized } as Serialized;
    if (object.prefix !== undefined && object.prefix !== null) {
      message.prefix = object.prefix;
    } else {
      message.prefix = new Uint8Array();
    }
    if (object.key !== undefined && object.key !== null) {
      message.key = object.key;
    } else {
      message.key = new Uint8Array();
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = new Uint8Array();
    }
    return message;
  },
};

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.addressAssociations) {
      AddressAssociation.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.codes) {
      Code.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.states) {
      ContractState.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.nonces) {
      Nonce.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    for (const v of message.serialized) {
      Serialized.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.addressAssociations = [];
    message.codes = [];
    message.states = [];
    message.nonces = [];
    message.serialized = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.addressAssociations.push(
            AddressAssociation.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.codes.push(Code.decode(reader, reader.uint32()));
          break;
        case 4:
          message.states.push(ContractState.decode(reader, reader.uint32()));
          break;
        case 5:
          message.nonces.push(Nonce.decode(reader, reader.uint32()));
          break;
        case 6:
          message.serialized.push(Serialized.decode(reader, reader.uint32()));
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
    message.addressAssociations = [];
    message.codes = [];
    message.states = [];
    message.nonces = [];
    message.serialized = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.addressAssociations !== undefined &&
      object.addressAssociations !== null
    ) {
      for (const e of object.addressAssociations) {
        message.addressAssociations.push(AddressAssociation.fromJSON(e));
      }
    }
    if (object.codes !== undefined && object.codes !== null) {
      for (const e of object.codes) {
        message.codes.push(Code.fromJSON(e));
      }
    }
    if (object.states !== undefined && object.states !== null) {
      for (const e of object.states) {
        message.states.push(ContractState.fromJSON(e));
      }
    }
    if (object.nonces !== undefined && object.nonces !== null) {
      for (const e of object.nonces) {
        message.nonces.push(Nonce.fromJSON(e));
      }
    }
    if (object.serialized !== undefined && object.serialized !== null) {
      for (const e of object.serialized) {
        message.serialized.push(Serialized.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.addressAssociations) {
      obj.addressAssociations = message.addressAssociations.map((e) =>
        e ? AddressAssociation.toJSON(e) : undefined
      );
    } else {
      obj.addressAssociations = [];
    }
    if (message.codes) {
      obj.codes = message.codes.map((e) => (e ? Code.toJSON(e) : undefined));
    } else {
      obj.codes = [];
    }
    if (message.states) {
      obj.states = message.states.map((e) =>
        e ? ContractState.toJSON(e) : undefined
      );
    } else {
      obj.states = [];
    }
    if (message.nonces) {
      obj.nonces = message.nonces.map((e) => (e ? Nonce.toJSON(e) : undefined));
    } else {
      obj.nonces = [];
    }
    if (message.serialized) {
      obj.serialized = message.serialized.map((e) =>
        e ? Serialized.toJSON(e) : undefined
      );
    } else {
      obj.serialized = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.addressAssociations = [];
    message.codes = [];
    message.states = [];
    message.nonces = [];
    message.serialized = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.addressAssociations !== undefined &&
      object.addressAssociations !== null
    ) {
      for (const e of object.addressAssociations) {
        message.addressAssociations.push(AddressAssociation.fromPartial(e));
      }
    }
    if (object.codes !== undefined && object.codes !== null) {
      for (const e of object.codes) {
        message.codes.push(Code.fromPartial(e));
      }
    }
    if (object.states !== undefined && object.states !== null) {
      for (const e of object.states) {
        message.states.push(ContractState.fromPartial(e));
      }
    }
    if (object.nonces !== undefined && object.nonces !== null) {
      for (const e of object.nonces) {
        message.nonces.push(Nonce.fromPartial(e));
      }
    }
    if (object.serialized !== undefined && object.serialized !== null) {
      for (const e of object.serialized) {
        message.serialized.push(Serialized.fromPartial(e));
      }
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
