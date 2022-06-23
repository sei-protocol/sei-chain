/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "tendermint.version";

/**
 * App includes the protocol and software version for the application.
 * This information is included in ResponseInfo. The App.Protocol can be
 * updated in ResponseEndBlock.
 */
export interface App {
  protocol: number;
  software: string;
}

/**
 * Consensus captures the consensus rules for processing a block in the blockchain,
 * including all blockchain data structures and the rules of the application's
 * state transition machine.
 */
export interface Consensus {
  block: number;
  app: number;
}

const baseApp: object = { protocol: 0, software: "" };

export const App = {
  encode(message: App, writer: Writer = Writer.create()): Writer {
    if (message.protocol !== 0) {
      writer.uint32(8).uint64(message.protocol);
    }
    if (message.software !== "") {
      writer.uint32(18).string(message.software);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): App {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseApp } as App;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.protocol = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.software = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): App {
    const message = { ...baseApp } as App;
    if (object.protocol !== undefined && object.protocol !== null) {
      message.protocol = Number(object.protocol);
    } else {
      message.protocol = 0;
    }
    if (object.software !== undefined && object.software !== null) {
      message.software = String(object.software);
    } else {
      message.software = "";
    }
    return message;
  },

  toJSON(message: App): unknown {
    const obj: any = {};
    message.protocol !== undefined && (obj.protocol = message.protocol);
    message.software !== undefined && (obj.software = message.software);
    return obj;
  },

  fromPartial(object: DeepPartial<App>): App {
    const message = { ...baseApp } as App;
    if (object.protocol !== undefined && object.protocol !== null) {
      message.protocol = object.protocol;
    } else {
      message.protocol = 0;
    }
    if (object.software !== undefined && object.software !== null) {
      message.software = object.software;
    } else {
      message.software = "";
    }
    return message;
  },
};

const baseConsensus: object = { block: 0, app: 0 };

export const Consensus = {
  encode(message: Consensus, writer: Writer = Writer.create()): Writer {
    if (message.block !== 0) {
      writer.uint32(8).uint64(message.block);
    }
    if (message.app !== 0) {
      writer.uint32(16).uint64(message.app);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Consensus {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseConsensus } as Consensus;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.block = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.app = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Consensus {
    const message = { ...baseConsensus } as Consensus;
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

  toJSON(message: Consensus): unknown {
    const obj: any = {};
    message.block !== undefined && (obj.block = message.block);
    message.app !== undefined && (obj.app = message.app);
    return obj;
  },

  fromPartial(object: DeepPartial<Consensus>): Consensus {
    const message = { ...baseConsensus } as Consensus;
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
