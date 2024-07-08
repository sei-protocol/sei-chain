/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.dex";

export interface ContractInfo {
  codeId: number;
  contractAddr: string;
  needHook: boolean;
  needOrderMatching: boolean;
  dependencies: ContractDependencyInfo[];
  numIncomingDependencies: number;
}

export interface ContractInfoV2 {
  codeId: number;
  contractAddr: string;
  needHook: boolean;
  needOrderMatching: boolean;
  dependencies: ContractDependencyInfo[];
  numIncomingDependencies: number;
  creator: string;
  rentBalance: number;
  suspended: boolean;
  suspensionReason: string;
}

/**
 * suppose A is first registered and depends on X, then B is added and depends on X,
 * and then C is added and depends on X, then A is the elder sibling to B and B is
 * the younger sibling to A, and B is the elder sibling to C and C is the younger to B
 */
export interface ContractDependencyInfo {
  dependency: string;
  immediateElderSibling: string;
  immediateYoungerSibling: string;
}

export interface LegacyContractInfo {
  codeId: number;
  contractAddr: string;
  needHook: boolean;
  needOrderMatching: boolean;
  dependentContractAddrs: string[];
}

export interface DownsteamContracts {
  contractAddrs: string[];
}

const baseContractInfo: object = {
  codeId: 0,
  contractAddr: "",
  needHook: false,
  needOrderMatching: false,
  numIncomingDependencies: 0,
};

export const ContractInfo = {
  encode(message: ContractInfo, writer: Writer = Writer.create()): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    if (message.needHook === true) {
      writer.uint32(24).bool(message.needHook);
    }
    if (message.needOrderMatching === true) {
      writer.uint32(32).bool(message.needOrderMatching);
    }
    for (const v of message.dependencies) {
      ContractDependencyInfo.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    if (message.numIncomingDependencies !== 0) {
      writer.uint32(48).int64(message.numIncomingDependencies);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractInfo } as ContractInfo;
    message.dependencies = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        case 3:
          message.needHook = reader.bool();
          break;
        case 4:
          message.needOrderMatching = reader.bool();
          break;
        case 5:
          message.dependencies.push(
            ContractDependencyInfo.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.numIncomingDependencies = longToNumber(
            reader.int64() as Long
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractInfo {
    const message = { ...baseContractInfo } as ContractInfo;
    message.dependencies = [];
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.needHook !== undefined && object.needHook !== null) {
      message.needHook = Boolean(object.needHook);
    } else {
      message.needHook = false;
    }
    if (
      object.needOrderMatching !== undefined &&
      object.needOrderMatching !== null
    ) {
      message.needOrderMatching = Boolean(object.needOrderMatching);
    } else {
      message.needOrderMatching = false;
    }
    if (object.dependencies !== undefined && object.dependencies !== null) {
      for (const e of object.dependencies) {
        message.dependencies.push(ContractDependencyInfo.fromJSON(e));
      }
    }
    if (
      object.numIncomingDependencies !== undefined &&
      object.numIncomingDependencies !== null
    ) {
      message.numIncomingDependencies = Number(object.numIncomingDependencies);
    } else {
      message.numIncomingDependencies = 0;
    }
    return message;
  },

  toJSON(message: ContractInfo): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.needHook !== undefined && (obj.needHook = message.needHook);
    message.needOrderMatching !== undefined &&
      (obj.needOrderMatching = message.needOrderMatching);
    if (message.dependencies) {
      obj.dependencies = message.dependencies.map((e) =>
        e ? ContractDependencyInfo.toJSON(e) : undefined
      );
    } else {
      obj.dependencies = [];
    }
    message.numIncomingDependencies !== undefined &&
      (obj.numIncomingDependencies = message.numIncomingDependencies);
    return obj;
  },

  fromPartial(object: DeepPartial<ContractInfo>): ContractInfo {
    const message = { ...baseContractInfo } as ContractInfo;
    message.dependencies = [];
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.needHook !== undefined && object.needHook !== null) {
      message.needHook = object.needHook;
    } else {
      message.needHook = false;
    }
    if (
      object.needOrderMatching !== undefined &&
      object.needOrderMatching !== null
    ) {
      message.needOrderMatching = object.needOrderMatching;
    } else {
      message.needOrderMatching = false;
    }
    if (object.dependencies !== undefined && object.dependencies !== null) {
      for (const e of object.dependencies) {
        message.dependencies.push(ContractDependencyInfo.fromPartial(e));
      }
    }
    if (
      object.numIncomingDependencies !== undefined &&
      object.numIncomingDependencies !== null
    ) {
      message.numIncomingDependencies = object.numIncomingDependencies;
    } else {
      message.numIncomingDependencies = 0;
    }
    return message;
  },
};

const baseContractInfoV2: object = {
  codeId: 0,
  contractAddr: "",
  needHook: false,
  needOrderMatching: false,
  numIncomingDependencies: 0,
  creator: "",
  rentBalance: 0,
  suspended: false,
  suspensionReason: "",
};

export const ContractInfoV2 = {
  encode(message: ContractInfoV2, writer: Writer = Writer.create()): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    if (message.needHook === true) {
      writer.uint32(24).bool(message.needHook);
    }
    if (message.needOrderMatching === true) {
      writer.uint32(32).bool(message.needOrderMatching);
    }
    for (const v of message.dependencies) {
      ContractDependencyInfo.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    if (message.numIncomingDependencies !== 0) {
      writer.uint32(48).int64(message.numIncomingDependencies);
    }
    if (message.creator !== "") {
      writer.uint32(58).string(message.creator);
    }
    if (message.rentBalance !== 0) {
      writer.uint32(64).uint64(message.rentBalance);
    }
    if (message.suspended === true) {
      writer.uint32(72).bool(message.suspended);
    }
    if (message.suspensionReason !== "") {
      writer.uint32(82).string(message.suspensionReason);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractInfoV2 {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractInfoV2 } as ContractInfoV2;
    message.dependencies = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        case 3:
          message.needHook = reader.bool();
          break;
        case 4:
          message.needOrderMatching = reader.bool();
          break;
        case 5:
          message.dependencies.push(
            ContractDependencyInfo.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.numIncomingDependencies = longToNumber(
            reader.int64() as Long
          );
          break;
        case 7:
          message.creator = reader.string();
          break;
        case 8:
          message.rentBalance = longToNumber(reader.uint64() as Long);
          break;
        case 9:
          message.suspended = reader.bool();
          break;
        case 10:
          message.suspensionReason = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractInfoV2 {
    const message = { ...baseContractInfoV2 } as ContractInfoV2;
    message.dependencies = [];
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.needHook !== undefined && object.needHook !== null) {
      message.needHook = Boolean(object.needHook);
    } else {
      message.needHook = false;
    }
    if (
      object.needOrderMatching !== undefined &&
      object.needOrderMatching !== null
    ) {
      message.needOrderMatching = Boolean(object.needOrderMatching);
    } else {
      message.needOrderMatching = false;
    }
    if (object.dependencies !== undefined && object.dependencies !== null) {
      for (const e of object.dependencies) {
        message.dependencies.push(ContractDependencyInfo.fromJSON(e));
      }
    }
    if (
      object.numIncomingDependencies !== undefined &&
      object.numIncomingDependencies !== null
    ) {
      message.numIncomingDependencies = Number(object.numIncomingDependencies);
    } else {
      message.numIncomingDependencies = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.rentBalance !== undefined && object.rentBalance !== null) {
      message.rentBalance = Number(object.rentBalance);
    } else {
      message.rentBalance = 0;
    }
    if (object.suspended !== undefined && object.suspended !== null) {
      message.suspended = Boolean(object.suspended);
    } else {
      message.suspended = false;
    }
    if (
      object.suspensionReason !== undefined &&
      object.suspensionReason !== null
    ) {
      message.suspensionReason = String(object.suspensionReason);
    } else {
      message.suspensionReason = "";
    }
    return message;
  },

  toJSON(message: ContractInfoV2): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.needHook !== undefined && (obj.needHook = message.needHook);
    message.needOrderMatching !== undefined &&
      (obj.needOrderMatching = message.needOrderMatching);
    if (message.dependencies) {
      obj.dependencies = message.dependencies.map((e) =>
        e ? ContractDependencyInfo.toJSON(e) : undefined
      );
    } else {
      obj.dependencies = [];
    }
    message.numIncomingDependencies !== undefined &&
      (obj.numIncomingDependencies = message.numIncomingDependencies);
    message.creator !== undefined && (obj.creator = message.creator);
    message.rentBalance !== undefined &&
      (obj.rentBalance = message.rentBalance);
    message.suspended !== undefined && (obj.suspended = message.suspended);
    message.suspensionReason !== undefined &&
      (obj.suspensionReason = message.suspensionReason);
    return obj;
  },

  fromPartial(object: DeepPartial<ContractInfoV2>): ContractInfoV2 {
    const message = { ...baseContractInfoV2 } as ContractInfoV2;
    message.dependencies = [];
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.needHook !== undefined && object.needHook !== null) {
      message.needHook = object.needHook;
    } else {
      message.needHook = false;
    }
    if (
      object.needOrderMatching !== undefined &&
      object.needOrderMatching !== null
    ) {
      message.needOrderMatching = object.needOrderMatching;
    } else {
      message.needOrderMatching = false;
    }
    if (object.dependencies !== undefined && object.dependencies !== null) {
      for (const e of object.dependencies) {
        message.dependencies.push(ContractDependencyInfo.fromPartial(e));
      }
    }
    if (
      object.numIncomingDependencies !== undefined &&
      object.numIncomingDependencies !== null
    ) {
      message.numIncomingDependencies = object.numIncomingDependencies;
    } else {
      message.numIncomingDependencies = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.rentBalance !== undefined && object.rentBalance !== null) {
      message.rentBalance = object.rentBalance;
    } else {
      message.rentBalance = 0;
    }
    if (object.suspended !== undefined && object.suspended !== null) {
      message.suspended = object.suspended;
    } else {
      message.suspended = false;
    }
    if (
      object.suspensionReason !== undefined &&
      object.suspensionReason !== null
    ) {
      message.suspensionReason = object.suspensionReason;
    } else {
      message.suspensionReason = "";
    }
    return message;
  },
};

const baseContractDependencyInfo: object = {
  dependency: "",
  immediateElderSibling: "",
  immediateYoungerSibling: "",
};

export const ContractDependencyInfo = {
  encode(
    message: ContractDependencyInfo,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.dependency !== "") {
      writer.uint32(10).string(message.dependency);
    }
    if (message.immediateElderSibling !== "") {
      writer.uint32(18).string(message.immediateElderSibling);
    }
    if (message.immediateYoungerSibling !== "") {
      writer.uint32(26).string(message.immediateYoungerSibling);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractDependencyInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractDependencyInfo } as ContractDependencyInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.dependency = reader.string();
          break;
        case 2:
          message.immediateElderSibling = reader.string();
          break;
        case 3:
          message.immediateYoungerSibling = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractDependencyInfo {
    const message = { ...baseContractDependencyInfo } as ContractDependencyInfo;
    if (object.dependency !== undefined && object.dependency !== null) {
      message.dependency = String(object.dependency);
    } else {
      message.dependency = "";
    }
    if (
      object.immediateElderSibling !== undefined &&
      object.immediateElderSibling !== null
    ) {
      message.immediateElderSibling = String(object.immediateElderSibling);
    } else {
      message.immediateElderSibling = "";
    }
    if (
      object.immediateYoungerSibling !== undefined &&
      object.immediateYoungerSibling !== null
    ) {
      message.immediateYoungerSibling = String(object.immediateYoungerSibling);
    } else {
      message.immediateYoungerSibling = "";
    }
    return message;
  },

  toJSON(message: ContractDependencyInfo): unknown {
    const obj: any = {};
    message.dependency !== undefined && (obj.dependency = message.dependency);
    message.immediateElderSibling !== undefined &&
      (obj.immediateElderSibling = message.immediateElderSibling);
    message.immediateYoungerSibling !== undefined &&
      (obj.immediateYoungerSibling = message.immediateYoungerSibling);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ContractDependencyInfo>
  ): ContractDependencyInfo {
    const message = { ...baseContractDependencyInfo } as ContractDependencyInfo;
    if (object.dependency !== undefined && object.dependency !== null) {
      message.dependency = object.dependency;
    } else {
      message.dependency = "";
    }
    if (
      object.immediateElderSibling !== undefined &&
      object.immediateElderSibling !== null
    ) {
      message.immediateElderSibling = object.immediateElderSibling;
    } else {
      message.immediateElderSibling = "";
    }
    if (
      object.immediateYoungerSibling !== undefined &&
      object.immediateYoungerSibling !== null
    ) {
      message.immediateYoungerSibling = object.immediateYoungerSibling;
    } else {
      message.immediateYoungerSibling = "";
    }
    return message;
  },
};

const baseLegacyContractInfo: object = {
  codeId: 0,
  contractAddr: "",
  needHook: false,
  needOrderMatching: false,
  dependentContractAddrs: "",
};

export const LegacyContractInfo = {
  encode(
    message: LegacyContractInfo,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.codeId !== 0) {
      writer.uint32(8).uint64(message.codeId);
    }
    if (message.contractAddr !== "") {
      writer.uint32(18).string(message.contractAddr);
    }
    if (message.needHook === true) {
      writer.uint32(24).bool(message.needHook);
    }
    if (message.needOrderMatching === true) {
      writer.uint32(32).bool(message.needOrderMatching);
    }
    for (const v of message.dependentContractAddrs) {
      writer.uint32(42).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): LegacyContractInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseLegacyContractInfo } as LegacyContractInfo;
    message.dependentContractAddrs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.codeId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.contractAddr = reader.string();
          break;
        case 3:
          message.needHook = reader.bool();
          break;
        case 4:
          message.needOrderMatching = reader.bool();
          break;
        case 5:
          message.dependentContractAddrs.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): LegacyContractInfo {
    const message = { ...baseLegacyContractInfo } as LegacyContractInfo;
    message.dependentContractAddrs = [];
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = Number(object.codeId);
    } else {
      message.codeId = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = String(object.contractAddr);
    } else {
      message.contractAddr = "";
    }
    if (object.needHook !== undefined && object.needHook !== null) {
      message.needHook = Boolean(object.needHook);
    } else {
      message.needHook = false;
    }
    if (
      object.needOrderMatching !== undefined &&
      object.needOrderMatching !== null
    ) {
      message.needOrderMatching = Boolean(object.needOrderMatching);
    } else {
      message.needOrderMatching = false;
    }
    if (
      object.dependentContractAddrs !== undefined &&
      object.dependentContractAddrs !== null
    ) {
      for (const e of object.dependentContractAddrs) {
        message.dependentContractAddrs.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: LegacyContractInfo): unknown {
    const obj: any = {};
    message.codeId !== undefined && (obj.codeId = message.codeId);
    message.contractAddr !== undefined &&
      (obj.contractAddr = message.contractAddr);
    message.needHook !== undefined && (obj.needHook = message.needHook);
    message.needOrderMatching !== undefined &&
      (obj.needOrderMatching = message.needOrderMatching);
    if (message.dependentContractAddrs) {
      obj.dependentContractAddrs = message.dependentContractAddrs.map((e) => e);
    } else {
      obj.dependentContractAddrs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<LegacyContractInfo>): LegacyContractInfo {
    const message = { ...baseLegacyContractInfo } as LegacyContractInfo;
    message.dependentContractAddrs = [];
    if (object.codeId !== undefined && object.codeId !== null) {
      message.codeId = object.codeId;
    } else {
      message.codeId = 0;
    }
    if (object.contractAddr !== undefined && object.contractAddr !== null) {
      message.contractAddr = object.contractAddr;
    } else {
      message.contractAddr = "";
    }
    if (object.needHook !== undefined && object.needHook !== null) {
      message.needHook = object.needHook;
    } else {
      message.needHook = false;
    }
    if (
      object.needOrderMatching !== undefined &&
      object.needOrderMatching !== null
    ) {
      message.needOrderMatching = object.needOrderMatching;
    } else {
      message.needOrderMatching = false;
    }
    if (
      object.dependentContractAddrs !== undefined &&
      object.dependentContractAddrs !== null
    ) {
      for (const e of object.dependentContractAddrs) {
        message.dependentContractAddrs.push(e);
      }
    }
    return message;
  },
};

const baseDownsteamContracts: object = { contractAddrs: "" };

export const DownsteamContracts = {
  encode(
    message: DownsteamContracts,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.contractAddrs) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DownsteamContracts {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDownsteamContracts } as DownsteamContracts;
    message.contractAddrs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddrs.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DownsteamContracts {
    const message = { ...baseDownsteamContracts } as DownsteamContracts;
    message.contractAddrs = [];
    if (object.contractAddrs !== undefined && object.contractAddrs !== null) {
      for (const e of object.contractAddrs) {
        message.contractAddrs.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: DownsteamContracts): unknown {
    const obj: any = {};
    if (message.contractAddrs) {
      obj.contractAddrs = message.contractAddrs.map((e) => e);
    } else {
      obj.contractAddrs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<DownsteamContracts>): DownsteamContracts {
    const message = { ...baseDownsteamContracts } as DownsteamContracts;
    message.contractAddrs = [];
    if (object.contractAddrs !== undefined && object.contractAddrs !== null) {
      for (const e of object.contractAddrs) {
        message.contractAddrs.push(e);
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
