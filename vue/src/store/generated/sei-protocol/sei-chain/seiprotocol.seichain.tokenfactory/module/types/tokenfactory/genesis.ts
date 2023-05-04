/* eslint-disable */
import { Params } from "../tokenfactory/params";
import { DenomAuthorityMetadata } from "../tokenfactory/authorityMetadata";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "seiprotocol.seichain.tokenfactory";

/** GenesisState defines the tokenfactory module's genesis state. */
export interface GenesisState {
  /** params defines the paramaters of the module. */
  params: Params | undefined;
  factory_denoms: GenesisDenom[];
}

/**
 * GenesisDenom defines a tokenfactory denom that is defined within genesis
 * state. The structure contains DenomAuthorityMetadata which defines the
 * denom's admin.
 */
export interface GenesisDenom {
  denom: string;
  authority_metadata: DenomAuthorityMetadata | undefined;
}

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.factory_denoms) {
      GenesisDenom.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.factory_denoms = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.factory_denoms.push(
            GenesisDenom.decode(reader, reader.uint32())
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
    message.factory_denoms = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.factory_denoms !== undefined && object.factory_denoms !== null) {
      for (const e of object.factory_denoms) {
        message.factory_denoms.push(GenesisDenom.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.factory_denoms) {
      obj.factory_denoms = message.factory_denoms.map((e) =>
        e ? GenesisDenom.toJSON(e) : undefined
      );
    } else {
      obj.factory_denoms = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.factory_denoms = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.factory_denoms !== undefined && object.factory_denoms !== null) {
      for (const e of object.factory_denoms) {
        message.factory_denoms.push(GenesisDenom.fromPartial(e));
      }
    }
    return message;
  },
};

const baseGenesisDenom: object = { denom: "" };

export const GenesisDenom = {
  encode(message: GenesisDenom, writer: Writer = Writer.create()): Writer {
    if (message.denom !== "") {
      writer.uint32(10).string(message.denom);
    }
    if (message.authority_metadata !== undefined) {
      DenomAuthorityMetadata.encode(
        message.authority_metadata,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisDenom {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisDenom } as GenesisDenom;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.denom = reader.string();
          break;
        case 2:
          message.authority_metadata = DenomAuthorityMetadata.decode(
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

  fromJSON(object: any): GenesisDenom {
    const message = { ...baseGenesisDenom } as GenesisDenom;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = String(object.denom);
    } else {
      message.denom = "";
    }
    if (
      object.authority_metadata !== undefined &&
      object.authority_metadata !== null
    ) {
      message.authority_metadata = DenomAuthorityMetadata.fromJSON(
        object.authority_metadata
      );
    } else {
      message.authority_metadata = undefined;
    }
    return message;
  },

  toJSON(message: GenesisDenom): unknown {
    const obj: any = {};
    message.denom !== undefined && (obj.denom = message.denom);
    message.authority_metadata !== undefined &&
      (obj.authority_metadata = message.authority_metadata
        ? DenomAuthorityMetadata.toJSON(message.authority_metadata)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisDenom>): GenesisDenom {
    const message = { ...baseGenesisDenom } as GenesisDenom;
    if (object.denom !== undefined && object.denom !== null) {
      message.denom = object.denom;
    } else {
      message.denom = "";
    }
    if (
      object.authority_metadata !== undefined &&
      object.authority_metadata !== null
    ) {
      message.authority_metadata = DenomAuthorityMetadata.fromPartial(
        object.authority_metadata
      );
    } else {
      message.authority_metadata = undefined;
    }
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
