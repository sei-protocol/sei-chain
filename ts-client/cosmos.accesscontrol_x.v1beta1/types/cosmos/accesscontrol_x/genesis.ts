/* eslint-disable */
import {
  MessageDependencyMapping,
  WasmDependencyMapping,
} from "../../cosmos/accesscontrol/accesscontrol";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.accesscontrol_x.v1beta1";

export interface GenesisState {
  params: Params | undefined;
  /** mapping between every message type and its predetermined resource read/write sequence */
  messageDependencyMapping: MessageDependencyMapping[];
  wasmDependencyMappings: WasmDependencyMapping[];
}

export interface Params {}

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.messageDependencyMapping) {
      MessageDependencyMapping.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.wasmDependencyMappings) {
      WasmDependencyMapping.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.messageDependencyMapping = [];
    message.wasmDependencyMappings = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.messageDependencyMapping.push(
            MessageDependencyMapping.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.wasmDependencyMappings.push(
            WasmDependencyMapping.decode(reader, reader.uint32())
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
    message.messageDependencyMapping = [];
    message.wasmDependencyMappings = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      for (const e of object.messageDependencyMapping) {
        message.messageDependencyMapping.push(
          MessageDependencyMapping.fromJSON(e)
        );
      }
    }
    if (
      object.wasmDependencyMappings !== undefined &&
      object.wasmDependencyMappings !== null
    ) {
      for (const e of object.wasmDependencyMappings) {
        message.wasmDependencyMappings.push(WasmDependencyMapping.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.messageDependencyMapping) {
      obj.messageDependencyMapping = message.messageDependencyMapping.map((e) =>
        e ? MessageDependencyMapping.toJSON(e) : undefined
      );
    } else {
      obj.messageDependencyMapping = [];
    }
    if (message.wasmDependencyMappings) {
      obj.wasmDependencyMappings = message.wasmDependencyMappings.map((e) =>
        e ? WasmDependencyMapping.toJSON(e) : undefined
      );
    } else {
      obj.wasmDependencyMappings = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.messageDependencyMapping = [];
    message.wasmDependencyMappings = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (
      object.messageDependencyMapping !== undefined &&
      object.messageDependencyMapping !== null
    ) {
      for (const e of object.messageDependencyMapping) {
        message.messageDependencyMapping.push(
          MessageDependencyMapping.fromPartial(e)
        );
      }
    }
    if (
      object.wasmDependencyMappings !== undefined &&
      object.wasmDependencyMappings !== null
    ) {
      for (const e of object.wasmDependencyMappings) {
        message.wasmDependencyMappings.push(
          WasmDependencyMapping.fromPartial(e)
        );
      }
    }
    return message;
  },
};

const baseParams: object = {};

export const Params = {
  encode(_: Params, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
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

  fromJSON(_: any): Params {
    const message = { ...baseParams } as Params;
    return message;
  },

  toJSON(_: Params): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    return message;
  },
};

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
