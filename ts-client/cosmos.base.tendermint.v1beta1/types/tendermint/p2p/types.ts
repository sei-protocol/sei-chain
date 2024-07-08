/* eslint-disable */
import { Timestamp } from "../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "tendermint.p2p";

export interface ProtocolVersion {
  p2p: number;
  block: number;
  app: number;
}

export interface NodeInfo {
  protocolVersion: ProtocolVersion | undefined;
  nodeId: string;
  listenAddr: string;
  network: string;
  version: string;
  channels: Uint8Array;
  moniker: string;
  other: NodeInfoOther | undefined;
}

export interface NodeInfoOther {
  txIndex: string;
  rpcAddress: string;
}

export interface PeerInfo {
  id: string;
  addressInfo: PeerAddressInfo[];
  lastConnected: Date | undefined;
}

export interface PeerAddressInfo {
  address: string;
  lastDialSuccess: Date | undefined;
  lastDialFailure: Date | undefined;
  dialFailures: number;
}

const baseProtocolVersion: object = { p2p: 0, block: 0, app: 0 };

export const ProtocolVersion = {
  encode(message: ProtocolVersion, writer: Writer = Writer.create()): Writer {
    if (message.p2p !== 0) {
      writer.uint32(8).uint64(message.p2p);
    }
    if (message.block !== 0) {
      writer.uint32(16).uint64(message.block);
    }
    if (message.app !== 0) {
      writer.uint32(24).uint64(message.app);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ProtocolVersion {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseProtocolVersion } as ProtocolVersion;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.p2p = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.block = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.app = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ProtocolVersion {
    const message = { ...baseProtocolVersion } as ProtocolVersion;
    if (object.p2p !== undefined && object.p2p !== null) {
      message.p2p = Number(object.p2p);
    } else {
      message.p2p = 0;
    }
    if (object.block !== undefined && object.block !== null) {
      message.block = Number(object.block);
    } else {
      message.block = 0;
    }
    if (object.app !== undefined && object.app !== null) {
      message.app = Number(object.app);
    } else {
      message.app = 0;
    }
    return message;
  },

  toJSON(message: ProtocolVersion): unknown {
    const obj: any = {};
    message.p2p !== undefined && (obj.p2p = message.p2p);
    message.block !== undefined && (obj.block = message.block);
    message.app !== undefined && (obj.app = message.app);
    return obj;
  },

  fromPartial(object: DeepPartial<ProtocolVersion>): ProtocolVersion {
    const message = { ...baseProtocolVersion } as ProtocolVersion;
    if (object.p2p !== undefined && object.p2p !== null) {
      message.p2p = object.p2p;
    } else {
      message.p2p = 0;
    }
    if (object.block !== undefined && object.block !== null) {
      message.block = object.block;
    } else {
      message.block = 0;
    }
    if (object.app !== undefined && object.app !== null) {
      message.app = object.app;
    } else {
      message.app = 0;
    }
    return message;
  },
};

const baseNodeInfo: object = {
  nodeId: "",
  listenAddr: "",
  network: "",
  version: "",
  moniker: "",
};

export const NodeInfo = {
  encode(message: NodeInfo, writer: Writer = Writer.create()): Writer {
    if (message.protocolVersion !== undefined) {
      ProtocolVersion.encode(
        message.protocolVersion,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.nodeId !== "") {
      writer.uint32(18).string(message.nodeId);
    }
    if (message.listenAddr !== "") {
      writer.uint32(26).string(message.listenAddr);
    }
    if (message.network !== "") {
      writer.uint32(34).string(message.network);
    }
    if (message.version !== "") {
      writer.uint32(42).string(message.version);
    }
    if (message.channels.length !== 0) {
      writer.uint32(50).bytes(message.channels);
    }
    if (message.moniker !== "") {
      writer.uint32(58).string(message.moniker);
    }
    if (message.other !== undefined) {
      NodeInfoOther.encode(message.other, writer.uint32(66).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): NodeInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseNodeInfo } as NodeInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.protocolVersion = ProtocolVersion.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.nodeId = reader.string();
          break;
        case 3:
          message.listenAddr = reader.string();
          break;
        case 4:
          message.network = reader.string();
          break;
        case 5:
          message.version = reader.string();
          break;
        case 6:
          message.channels = reader.bytes();
          break;
        case 7:
          message.moniker = reader.string();
          break;
        case 8:
          message.other = NodeInfoOther.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): NodeInfo {
    const message = { ...baseNodeInfo } as NodeInfo;
    if (
      object.protocolVersion !== undefined &&
      object.protocolVersion !== null
    ) {
      message.protocolVersion = ProtocolVersion.fromJSON(
        object.protocolVersion
      );
    } else {
      message.protocolVersion = undefined;
    }
    if (object.nodeId !== undefined && object.nodeId !== null) {
      message.nodeId = String(object.nodeId);
    } else {
      message.nodeId = "";
    }
    if (object.listenAddr !== undefined && object.listenAddr !== null) {
      message.listenAddr = String(object.listenAddr);
    } else {
      message.listenAddr = "";
    }
    if (object.network !== undefined && object.network !== null) {
      message.network = String(object.network);
    } else {
      message.network = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = String(object.version);
    } else {
      message.version = "";
    }
    if (object.channels !== undefined && object.channels !== null) {
      message.channels = bytesFromBase64(object.channels);
    }
    if (object.moniker !== undefined && object.moniker !== null) {
      message.moniker = String(object.moniker);
    } else {
      message.moniker = "";
    }
    if (object.other !== undefined && object.other !== null) {
      message.other = NodeInfoOther.fromJSON(object.other);
    } else {
      message.other = undefined;
    }
    return message;
  },

  toJSON(message: NodeInfo): unknown {
    const obj: any = {};
    message.protocolVersion !== undefined &&
      (obj.protocolVersion = message.protocolVersion
        ? ProtocolVersion.toJSON(message.protocolVersion)
        : undefined);
    message.nodeId !== undefined && (obj.nodeId = message.nodeId);
    message.listenAddr !== undefined && (obj.listenAddr = message.listenAddr);
    message.network !== undefined && (obj.network = message.network);
    message.version !== undefined && (obj.version = message.version);
    message.channels !== undefined &&
      (obj.channels = base64FromBytes(
        message.channels !== undefined ? message.channels : new Uint8Array()
      ));
    message.moniker !== undefined && (obj.moniker = message.moniker);
    message.other !== undefined &&
      (obj.other = message.other
        ? NodeInfoOther.toJSON(message.other)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<NodeInfo>): NodeInfo {
    const message = { ...baseNodeInfo } as NodeInfo;
    if (
      object.protocolVersion !== undefined &&
      object.protocolVersion !== null
    ) {
      message.protocolVersion = ProtocolVersion.fromPartial(
        object.protocolVersion
      );
    } else {
      message.protocolVersion = undefined;
    }
    if (object.nodeId !== undefined && object.nodeId !== null) {
      message.nodeId = object.nodeId;
    } else {
      message.nodeId = "";
    }
    if (object.listenAddr !== undefined && object.listenAddr !== null) {
      message.listenAddr = object.listenAddr;
    } else {
      message.listenAddr = "";
    }
    if (object.network !== undefined && object.network !== null) {
      message.network = object.network;
    } else {
      message.network = "";
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = object.version;
    } else {
      message.version = "";
    }
    if (object.channels !== undefined && object.channels !== null) {
      message.channels = object.channels;
    } else {
      message.channels = new Uint8Array();
    }
    if (object.moniker !== undefined && object.moniker !== null) {
      message.moniker = object.moniker;
    } else {
      message.moniker = "";
    }
    if (object.other !== undefined && object.other !== null) {
      message.other = NodeInfoOther.fromPartial(object.other);
    } else {
      message.other = undefined;
    }
    return message;
  },
};

const baseNodeInfoOther: object = { txIndex: "", rpcAddress: "" };

export const NodeInfoOther = {
  encode(message: NodeInfoOther, writer: Writer = Writer.create()): Writer {
    if (message.txIndex !== "") {
      writer.uint32(10).string(message.txIndex);
    }
    if (message.rpcAddress !== "") {
      writer.uint32(18).string(message.rpcAddress);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): NodeInfoOther {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseNodeInfoOther } as NodeInfoOther;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.txIndex = reader.string();
          break;
        case 2:
          message.rpcAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): NodeInfoOther {
    const message = { ...baseNodeInfoOther } as NodeInfoOther;
    if (object.txIndex !== undefined && object.txIndex !== null) {
      message.txIndex = String(object.txIndex);
    } else {
      message.txIndex = "";
    }
    if (object.rpcAddress !== undefined && object.rpcAddress !== null) {
      message.rpcAddress = String(object.rpcAddress);
    } else {
      message.rpcAddress = "";
    }
    return message;
  },

  toJSON(message: NodeInfoOther): unknown {
    const obj: any = {};
    message.txIndex !== undefined && (obj.txIndex = message.txIndex);
    message.rpcAddress !== undefined && (obj.rpcAddress = message.rpcAddress);
    return obj;
  },

  fromPartial(object: DeepPartial<NodeInfoOther>): NodeInfoOther {
    const message = { ...baseNodeInfoOther } as NodeInfoOther;
    if (object.txIndex !== undefined && object.txIndex !== null) {
      message.txIndex = object.txIndex;
    } else {
      message.txIndex = "";
    }
    if (object.rpcAddress !== undefined && object.rpcAddress !== null) {
      message.rpcAddress = object.rpcAddress;
    } else {
      message.rpcAddress = "";
    }
    return message;
  },
};

const basePeerInfo: object = { id: "" };

export const PeerInfo = {
  encode(message: PeerInfo, writer: Writer = Writer.create()): Writer {
    if (message.id !== "") {
      writer.uint32(10).string(message.id);
    }
    for (const v of message.addressInfo) {
      PeerAddressInfo.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.lastConnected !== undefined) {
      Timestamp.encode(
        toTimestamp(message.lastConnected),
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PeerInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePeerInfo } as PeerInfo;
    message.addressInfo = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.id = reader.string();
          break;
        case 2:
          message.addressInfo.push(
            PeerAddressInfo.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.lastConnected = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PeerInfo {
    const message = { ...basePeerInfo } as PeerInfo;
    message.addressInfo = [];
    if (object.id !== undefined && object.id !== null) {
      message.id = String(object.id);
    } else {
      message.id = "";
    }
    if (object.addressInfo !== undefined && object.addressInfo !== null) {
      for (const e of object.addressInfo) {
        message.addressInfo.push(PeerAddressInfo.fromJSON(e));
      }
    }
    if (object.lastConnected !== undefined && object.lastConnected !== null) {
      message.lastConnected = fromJsonTimestamp(object.lastConnected);
    } else {
      message.lastConnected = undefined;
    }
    return message;
  },

  toJSON(message: PeerInfo): unknown {
    const obj: any = {};
    message.id !== undefined && (obj.id = message.id);
    if (message.addressInfo) {
      obj.addressInfo = message.addressInfo.map((e) =>
        e ? PeerAddressInfo.toJSON(e) : undefined
      );
    } else {
      obj.addressInfo = [];
    }
    message.lastConnected !== undefined &&
      (obj.lastConnected =
        message.lastConnected !== undefined
          ? message.lastConnected.toISOString()
          : null);
    return obj;
  },

  fromPartial(object: DeepPartial<PeerInfo>): PeerInfo {
    const message = { ...basePeerInfo } as PeerInfo;
    message.addressInfo = [];
    if (object.id !== undefined && object.id !== null) {
      message.id = object.id;
    } else {
      message.id = "";
    }
    if (object.addressInfo !== undefined && object.addressInfo !== null) {
      for (const e of object.addressInfo) {
        message.addressInfo.push(PeerAddressInfo.fromPartial(e));
      }
    }
    if (object.lastConnected !== undefined && object.lastConnected !== null) {
      message.lastConnected = object.lastConnected;
    } else {
      message.lastConnected = undefined;
    }
    return message;
  },
};

const basePeerAddressInfo: object = { address: "", dialFailures: 0 };

export const PeerAddressInfo = {
  encode(message: PeerAddressInfo, writer: Writer = Writer.create()): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.lastDialSuccess !== undefined) {
      Timestamp.encode(
        toTimestamp(message.lastDialSuccess),
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.lastDialFailure !== undefined) {
      Timestamp.encode(
        toTimestamp(message.lastDialFailure),
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.dialFailures !== 0) {
      writer.uint32(32).uint32(message.dialFailures);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PeerAddressInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePeerAddressInfo } as PeerAddressInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.lastDialSuccess = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.lastDialFailure = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.dialFailures = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PeerAddressInfo {
    const message = { ...basePeerAddressInfo } as PeerAddressInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (
      object.lastDialSuccess !== undefined &&
      object.lastDialSuccess !== null
    ) {
      message.lastDialSuccess = fromJsonTimestamp(object.lastDialSuccess);
    } else {
      message.lastDialSuccess = undefined;
    }
    if (
      object.lastDialFailure !== undefined &&
      object.lastDialFailure !== null
    ) {
      message.lastDialFailure = fromJsonTimestamp(object.lastDialFailure);
    } else {
      message.lastDialFailure = undefined;
    }
    if (object.dialFailures !== undefined && object.dialFailures !== null) {
      message.dialFailures = Number(object.dialFailures);
    } else {
      message.dialFailures = 0;
    }
    return message;
  },

  toJSON(message: PeerAddressInfo): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.lastDialSuccess !== undefined &&
      (obj.lastDialSuccess =
        message.lastDialSuccess !== undefined
          ? message.lastDialSuccess.toISOString()
          : null);
    message.lastDialFailure !== undefined &&
      (obj.lastDialFailure =
        message.lastDialFailure !== undefined
          ? message.lastDialFailure.toISOString()
          : null);
    message.dialFailures !== undefined &&
      (obj.dialFailures = message.dialFailures);
    return obj;
  },

  fromPartial(object: DeepPartial<PeerAddressInfo>): PeerAddressInfo {
    const message = { ...basePeerAddressInfo } as PeerAddressInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (
      object.lastDialSuccess !== undefined &&
      object.lastDialSuccess !== null
    ) {
      message.lastDialSuccess = object.lastDialSuccess;
    } else {
      message.lastDialSuccess = undefined;
    }
    if (
      object.lastDialFailure !== undefined &&
      object.lastDialFailure !== null
    ) {
      message.lastDialFailure = object.lastDialFailure;
    } else {
      message.lastDialFailure = undefined;
    }
    if (object.dialFailures !== undefined && object.dialFailures !== null) {
      message.dialFailures = object.dialFailures;
    } else {
      message.dialFailures = 0;
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

function toTimestamp(date: Date): Timestamp {
  const seconds = date.getTime() / 1_000;
  const nanos = (date.getTime() % 1_000) * 1_000_000;
  return { seconds, nanos };
}

function fromTimestamp(t: Timestamp): Date {
  let millis = t.seconds * 1_000;
  millis += t.nanos / 1_000_000;
  return new Date(millis);
}

function fromJsonTimestamp(o: any): Date {
  if (o instanceof Date) {
    return o;
  } else if (typeof o === "string") {
    return new Date(o);
  } else {
    return fromTimestamp(Timestamp.fromJSON(o));
  }
}

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
