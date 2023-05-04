/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import {
  Params,
  ValidatorSigningInfo,
} from "../../../cosmos/slashing/v1beta1/slashing";

export const protobufPackage = "cosmos.slashing.v1beta1";

/** GenesisState defines the slashing module's genesis state. */
export interface GenesisState {
  /** params defines all the paramaters of related to deposit. */
  params: Params | undefined;
  /**
   * signing_infos represents a map between validator addresses and their
   * signing infos.
   */
  signing_infos: SigningInfo[];
  /**
   * missed_blocks represents a map between validator addresses and their
   * missed blocks.
   */
  missed_blocks: ValidatorMissedBlocks[];
}

/** SigningInfo stores validator signing info of corresponding address. */
export interface SigningInfo {
  /** address is the validator address. */
  address: string;
  /** validator_signing_info represents the signing info of this validator. */
  validator_signing_info: ValidatorSigningInfo | undefined;
}

/**
 * ValidatorMissedBlocks contains array of missed blocks of corresponding
 * address.
 */
export interface ValidatorMissedBlocks {
  /** address is the validator address. */
  address: string;
  /** missed_blocks is an array of missed blocks by the validator. */
  missed_blocks: MissedBlock[];
}

/** MissedBlock contains height and missed status as boolean. */
export interface MissedBlock {
  /** index is the height at which the block was missed. */
  index: number;
  /** missed is the missed status. */
  missed: boolean;
}

const baseGenesisState: object = {};

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.signing_infos) {
      SigningInfo.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.missed_blocks) {
      ValidatorMissedBlocks.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.signing_infos = [];
    message.missed_blocks = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.signing_infos.push(
            SigningInfo.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.missed_blocks.push(
            ValidatorMissedBlocks.decode(reader, reader.uint32())
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
    message.signing_infos = [];
    message.missed_blocks = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.signing_infos !== undefined && object.signing_infos !== null) {
      for (const e of object.signing_infos) {
        message.signing_infos.push(SigningInfo.fromJSON(e));
      }
    }
    if (object.missed_blocks !== undefined && object.missed_blocks !== null) {
      for (const e of object.missed_blocks) {
        message.missed_blocks.push(ValidatorMissedBlocks.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.signing_infos) {
      obj.signing_infos = message.signing_infos.map((e) =>
        e ? SigningInfo.toJSON(e) : undefined
      );
    } else {
      obj.signing_infos = [];
    }
    if (message.missed_blocks) {
      obj.missed_blocks = message.missed_blocks.map((e) =>
        e ? ValidatorMissedBlocks.toJSON(e) : undefined
      );
    } else {
      obj.missed_blocks = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.signing_infos = [];
    message.missed_blocks = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.signing_infos !== undefined && object.signing_infos !== null) {
      for (const e of object.signing_infos) {
        message.signing_infos.push(SigningInfo.fromPartial(e));
      }
    }
    if (object.missed_blocks !== undefined && object.missed_blocks !== null) {
      for (const e of object.missed_blocks) {
        message.missed_blocks.push(ValidatorMissedBlocks.fromPartial(e));
      }
    }
    return message;
  },
};

const baseSigningInfo: object = { address: "" };

export const SigningInfo = {
  encode(message: SigningInfo, writer: Writer = Writer.create()): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    if (message.validator_signing_info !== undefined) {
      ValidatorSigningInfo.encode(
        message.validator_signing_info,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SigningInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSigningInfo } as SigningInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.validator_signing_info = ValidatorSigningInfo.decode(
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

  fromJSON(object: any): SigningInfo {
    const message = { ...baseSigningInfo } as SigningInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (
      object.validator_signing_info !== undefined &&
      object.validator_signing_info !== null
    ) {
      message.validator_signing_info = ValidatorSigningInfo.fromJSON(
        object.validator_signing_info
      );
    } else {
      message.validator_signing_info = undefined;
    }
    return message;
  },

  toJSON(message: SigningInfo): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.validator_signing_info !== undefined &&
      (obj.validator_signing_info = message.validator_signing_info
        ? ValidatorSigningInfo.toJSON(message.validator_signing_info)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<SigningInfo>): SigningInfo {
    const message = { ...baseSigningInfo } as SigningInfo;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (
      object.validator_signing_info !== undefined &&
      object.validator_signing_info !== null
    ) {
      message.validator_signing_info = ValidatorSigningInfo.fromPartial(
        object.validator_signing_info
      );
    } else {
      message.validator_signing_info = undefined;
    }
    return message;
  },
};

const baseValidatorMissedBlocks: object = { address: "" };

export const ValidatorMissedBlocks = {
  encode(
    message: ValidatorMissedBlocks,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.address !== "") {
      writer.uint32(10).string(message.address);
    }
    for (const v of message.missed_blocks) {
      MissedBlock.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorMissedBlocks {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorMissedBlocks } as ValidatorMissedBlocks;
    message.missed_blocks = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.missed_blocks.push(
            MissedBlock.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorMissedBlocks {
    const message = { ...baseValidatorMissedBlocks } as ValidatorMissedBlocks;
    message.missed_blocks = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.missed_blocks !== undefined && object.missed_blocks !== null) {
      for (const e of object.missed_blocks) {
        message.missed_blocks.push(MissedBlock.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorMissedBlocks): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    if (message.missed_blocks) {
      obj.missed_blocks = message.missed_blocks.map((e) =>
        e ? MissedBlock.toJSON(e) : undefined
      );
    } else {
      obj.missed_blocks = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorMissedBlocks>
  ): ValidatorMissedBlocks {
    const message = { ...baseValidatorMissedBlocks } as ValidatorMissedBlocks;
    message.missed_blocks = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.missed_blocks !== undefined && object.missed_blocks !== null) {
      for (const e of object.missed_blocks) {
        message.missed_blocks.push(MissedBlock.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMissedBlock: object = { index: 0, missed: false };

export const MissedBlock = {
  encode(message: MissedBlock, writer: Writer = Writer.create()): Writer {
    if (message.index !== 0) {
      writer.uint32(8).int64(message.index);
    }
    if (message.missed === true) {
      writer.uint32(16).bool(message.missed);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MissedBlock {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMissedBlock } as MissedBlock;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.index = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.missed = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MissedBlock {
    const message = { ...baseMissedBlock } as MissedBlock;
    if (object.index !== undefined && object.index !== null) {
      message.index = Number(object.index);
    } else {
      message.index = 0;
    }
    if (object.missed !== undefined && object.missed !== null) {
      message.missed = Boolean(object.missed);
    } else {
      message.missed = false;
    }
    return message;
  },

  toJSON(message: MissedBlock): unknown {
    const obj: any = {};
    message.index !== undefined && (obj.index = message.index);
    message.missed !== undefined && (obj.missed = message.missed);
    return obj;
  },

  fromPartial(object: DeepPartial<MissedBlock>): MissedBlock {
    const message = { ...baseMissedBlock } as MissedBlock;
    if (object.index !== undefined && object.index !== null) {
      message.index = object.index;
    } else {
      message.index = 0;
    }
    if (object.missed !== undefined && object.missed !== null) {
      message.missed = object.missed;
    } else {
      message.missed = false;
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
