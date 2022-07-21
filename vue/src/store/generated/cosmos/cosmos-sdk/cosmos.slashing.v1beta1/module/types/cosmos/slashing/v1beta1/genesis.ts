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
  signingInfos: SigningInfo[];
  /**
   * missed_blocks represents a map between validator addresses and their
   * missed blocks.
   */
  missedBlocks: ValidatorMissedBlocks[];
}

/** SigningInfo stores validator signing info of corresponding address. */
export interface SigningInfo {
  /** address is the validator address. */
  address: string;
  /** validator_signing_info represents the signing info of this validator. */
  validatorSigningInfo: ValidatorSigningInfo | undefined;
}

/**
 * ValidatorMissedBlocks contains array of missed blocks of corresponding
 * address.
 */
export interface ValidatorMissedBlocks {
  /** address is the validator address. */
  address: string;
  /** missed_blocks is an array of missed blocks by the validator. */
  missedBlocks: MissedBlock[];
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
    for (const v of message.signingInfos) {
      SigningInfo.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.missedBlocks) {
      ValidatorMissedBlocks.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.signingInfos = [];
    message.missedBlocks = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.signingInfos.push(
            SigningInfo.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.missedBlocks.push(
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
    message.signingInfos = [];
    message.missedBlocks = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.signingInfos !== undefined && object.signingInfos !== null) {
      for (const e of object.signingInfos) {
        message.signingInfos.push(SigningInfo.fromJSON(e));
      }
    }
    if (object.missedBlocks !== undefined && object.missedBlocks !== null) {
      for (const e of object.missedBlocks) {
        message.missedBlocks.push(ValidatorMissedBlocks.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    if (message.signingInfos) {
      obj.signingInfos = message.signingInfos.map((e) =>
        e ? SigningInfo.toJSON(e) : undefined
      );
    } else {
      obj.signingInfos = [];
    }
    if (message.missedBlocks) {
      obj.missedBlocks = message.missedBlocks.map((e) =>
        e ? ValidatorMissedBlocks.toJSON(e) : undefined
      );
    } else {
      obj.missedBlocks = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.signingInfos = [];
    message.missedBlocks = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.signingInfos !== undefined && object.signingInfos !== null) {
      for (const e of object.signingInfos) {
        message.signingInfos.push(SigningInfo.fromPartial(e));
      }
    }
    if (object.missedBlocks !== undefined && object.missedBlocks !== null) {
      for (const e of object.missedBlocks) {
        message.missedBlocks.push(ValidatorMissedBlocks.fromPartial(e));
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
    if (message.validatorSigningInfo !== undefined) {
      ValidatorSigningInfo.encode(
        message.validatorSigningInfo,
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
          message.validatorSigningInfo = ValidatorSigningInfo.decode(
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
      object.validatorSigningInfo !== undefined &&
      object.validatorSigningInfo !== null
    ) {
      message.validatorSigningInfo = ValidatorSigningInfo.fromJSON(
        object.validatorSigningInfo
      );
    } else {
      message.validatorSigningInfo = undefined;
    }
    return message;
  },

  toJSON(message: SigningInfo): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    message.validatorSigningInfo !== undefined &&
      (obj.validatorSigningInfo = message.validatorSigningInfo
        ? ValidatorSigningInfo.toJSON(message.validatorSigningInfo)
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
      object.validatorSigningInfo !== undefined &&
      object.validatorSigningInfo !== null
    ) {
      message.validatorSigningInfo = ValidatorSigningInfo.fromPartial(
        object.validatorSigningInfo
      );
    } else {
      message.validatorSigningInfo = undefined;
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
    for (const v of message.missedBlocks) {
      MissedBlock.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorMissedBlocks {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorMissedBlocks } as ValidatorMissedBlocks;
    message.missedBlocks = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.string();
          break;
        case 2:
          message.missedBlocks.push(
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
    message.missedBlocks = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    if (object.missedBlocks !== undefined && object.missedBlocks !== null) {
      for (const e of object.missedBlocks) {
        message.missedBlocks.push(MissedBlock.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorMissedBlocks): unknown {
    const obj: any = {};
    message.address !== undefined && (obj.address = message.address);
    if (message.missedBlocks) {
      obj.missedBlocks = message.missedBlocks.map((e) =>
        e ? MissedBlock.toJSON(e) : undefined
      );
    } else {
      obj.missedBlocks = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorMissedBlocks>
  ): ValidatorMissedBlocks {
    const message = { ...baseValidatorMissedBlocks } as ValidatorMissedBlocks;
    message.missedBlocks = [];
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    if (object.missedBlocks !== undefined && object.missedBlocks !== null) {
      for (const e of object.missedBlocks) {
        message.missedBlocks.push(MissedBlock.fromPartial(e));
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
