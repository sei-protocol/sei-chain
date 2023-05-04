/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Duration } from "../../google/protobuf/duration";

export const protobufPackage = "tendermint.types";

/**
 * ConsensusParams contains consensus critical parameters that determine the
 * validity of blocks.
 */
export interface ConsensusParams {
  block: BlockParams | undefined;
  evidence: EvidenceParams | undefined;
  validator: ValidatorParams | undefined;
  version: VersionParams | undefined;
}

/** BlockParams contains limits on the block size. */
export interface BlockParams {
  /**
   * Max block size, in bytes.
   * Note: must be greater than 0
   */
  max_bytes: number;
  /**
   * Max gas per block.
   * Note: must be greater or equal to -1
   */
  max_gas: number;
  /**
   * Minimum time increment between consecutive blocks (in milliseconds) If the
   * block header timestamp is ahead of the system clock, decrease this value.
   *
   * Not exposed to the application.
   */
  time_iota_ms: number;
}

/** EvidenceParams determine how we handle evidence of malfeasance. */
export interface EvidenceParams {
  /**
   * Max age of evidence, in blocks.
   *
   * The basic formula for calculating this is: MaxAgeDuration / {average block
   * time}.
   */
  max_age_num_blocks: number;
  /**
   * Max age of evidence, in time.
   *
   * It should correspond with an app's "unbonding period" or other similar
   * mechanism for handling [Nothing-At-Stake
   * attacks](https://github.com/ethereum/wiki/wiki/Proof-of-Stake-FAQ#what-is-the-nothing-at-stake-problem-and-how-can-it-be-fixed).
   */
  max_age_duration: Duration | undefined;
  /**
   * This sets the maximum size of total evidence in bytes that can be committed in a single block.
   * and should fall comfortably under the max block bytes.
   * Default is 1048576 or 1MB
   */
  max_bytes: number;
}

/**
 * ValidatorParams restrict the public key types validators can use.
 * NOTE: uses ABCI pubkey naming, not Amino names.
 */
export interface ValidatorParams {
  pub_key_types: string[];
}

/** VersionParams contains the ABCI application version. */
export interface VersionParams {
  app_version: number;
}

/**
 * HashedParams is a subset of ConsensusParams.
 *
 * It is hashed into the Header.ConsensusHash.
 */
export interface HashedParams {
  block_max_bytes: number;
  block_max_gas: number;
}

const baseConsensusParams: object = {};

export const ConsensusParams = {
  encode(message: ConsensusParams, writer: Writer = Writer.create()): Writer {
    if (message.block !== undefined) {
      BlockParams.encode(message.block, writer.uint32(10).fork()).ldelim();
    }
    if (message.evidence !== undefined) {
      EvidenceParams.encode(
        message.evidence,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.validator !== undefined) {
      ValidatorParams.encode(
        message.validator,
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.version !== undefined) {
      VersionParams.encode(message.version, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ConsensusParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseConsensusParams } as ConsensusParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.block = BlockParams.decode(reader, reader.uint32());
          break;
        case 2:
          message.evidence = EvidenceParams.decode(reader, reader.uint32());
          break;
        case 3:
          message.validator = ValidatorParams.decode(reader, reader.uint32());
          break;
        case 4:
          message.version = VersionParams.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ConsensusParams {
    const message = { ...baseConsensusParams } as ConsensusParams;
    if (object.block !== undefined && object.block !== null) {
      message.block = BlockParams.fromJSON(object.block);
    } else {
      message.block = undefined;
    }
    if (object.evidence !== undefined && object.evidence !== null) {
      message.evidence = EvidenceParams.fromJSON(object.evidence);
    } else {
      message.evidence = undefined;
    }
    if (object.validator !== undefined && object.validator !== null) {
      message.validator = ValidatorParams.fromJSON(object.validator);
    } else {
      message.validator = undefined;
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = VersionParams.fromJSON(object.version);
    } else {
      message.version = undefined;
    }
    return message;
  },

  toJSON(message: ConsensusParams): unknown {
    const obj: any = {};
    message.block !== undefined &&
      (obj.block = message.block
        ? BlockParams.toJSON(message.block)
        : undefined);
    message.evidence !== undefined &&
      (obj.evidence = message.evidence
        ? EvidenceParams.toJSON(message.evidence)
        : undefined);
    message.validator !== undefined &&
      (obj.validator = message.validator
        ? ValidatorParams.toJSON(message.validator)
        : undefined);
    message.version !== undefined &&
      (obj.version = message.version
        ? VersionParams.toJSON(message.version)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<ConsensusParams>): ConsensusParams {
    const message = { ...baseConsensusParams } as ConsensusParams;
    if (object.block !== undefined && object.block !== null) {
      message.block = BlockParams.fromPartial(object.block);
    } else {
      message.block = undefined;
    }
    if (object.evidence !== undefined && object.evidence !== null) {
      message.evidence = EvidenceParams.fromPartial(object.evidence);
    } else {
      message.evidence = undefined;
    }
    if (object.validator !== undefined && object.validator !== null) {
      message.validator = ValidatorParams.fromPartial(object.validator);
    } else {
      message.validator = undefined;
    }
    if (object.version !== undefined && object.version !== null) {
      message.version = VersionParams.fromPartial(object.version);
    } else {
      message.version = undefined;
    }
    return message;
  },
};

const baseBlockParams: object = { max_bytes: 0, max_gas: 0, time_iota_ms: 0 };

export const BlockParams = {
  encode(message: BlockParams, writer: Writer = Writer.create()): Writer {
    if (message.max_bytes !== 0) {
      writer.uint32(8).int64(message.max_bytes);
    }
    if (message.max_gas !== 0) {
      writer.uint32(16).int64(message.max_gas);
    }
    if (message.time_iota_ms !== 0) {
      writer.uint32(24).int64(message.time_iota_ms);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): BlockParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseBlockParams } as BlockParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.max_bytes = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.max_gas = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.time_iota_ms = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): BlockParams {
    const message = { ...baseBlockParams } as BlockParams;
    if (object.max_bytes !== undefined && object.max_bytes !== null) {
      message.max_bytes = Number(object.max_bytes);
    } else {
      message.max_bytes = 0;
    }
    if (object.max_gas !== undefined && object.max_gas !== null) {
      message.max_gas = Number(object.max_gas);
    } else {
      message.max_gas = 0;
    }
    if (object.time_iota_ms !== undefined && object.time_iota_ms !== null) {
      message.time_iota_ms = Number(object.time_iota_ms);
    } else {
      message.time_iota_ms = 0;
    }
    return message;
  },

  toJSON(message: BlockParams): unknown {
    const obj: any = {};
    message.max_bytes !== undefined && (obj.max_bytes = message.max_bytes);
    message.max_gas !== undefined && (obj.max_gas = message.max_gas);
    message.time_iota_ms !== undefined &&
      (obj.time_iota_ms = message.time_iota_ms);
    return obj;
  },

  fromPartial(object: DeepPartial<BlockParams>): BlockParams {
    const message = { ...baseBlockParams } as BlockParams;
    if (object.max_bytes !== undefined && object.max_bytes !== null) {
      message.max_bytes = object.max_bytes;
    } else {
      message.max_bytes = 0;
    }
    if (object.max_gas !== undefined && object.max_gas !== null) {
      message.max_gas = object.max_gas;
    } else {
      message.max_gas = 0;
    }
    if (object.time_iota_ms !== undefined && object.time_iota_ms !== null) {
      message.time_iota_ms = object.time_iota_ms;
    } else {
      message.time_iota_ms = 0;
    }
    return message;
  },
};

const baseEvidenceParams: object = { max_age_num_blocks: 0, max_bytes: 0 };

export const EvidenceParams = {
  encode(message: EvidenceParams, writer: Writer = Writer.create()): Writer {
    if (message.max_age_num_blocks !== 0) {
      writer.uint32(8).int64(message.max_age_num_blocks);
    }
    if (message.max_age_duration !== undefined) {
      Duration.encode(
        message.max_age_duration,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.max_bytes !== 0) {
      writer.uint32(24).int64(message.max_bytes);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): EvidenceParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEvidenceParams } as EvidenceParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.max_age_num_blocks = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.max_age_duration = Duration.decode(reader, reader.uint32());
          break;
        case 3:
          message.max_bytes = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): EvidenceParams {
    const message = { ...baseEvidenceParams } as EvidenceParams;
    if (
      object.max_age_num_blocks !== undefined &&
      object.max_age_num_blocks !== null
    ) {
      message.max_age_num_blocks = Number(object.max_age_num_blocks);
    } else {
      message.max_age_num_blocks = 0;
    }
    if (
      object.max_age_duration !== undefined &&
      object.max_age_duration !== null
    ) {
      message.max_age_duration = Duration.fromJSON(object.max_age_duration);
    } else {
      message.max_age_duration = undefined;
    }
    if (object.max_bytes !== undefined && object.max_bytes !== null) {
      message.max_bytes = Number(object.max_bytes);
    } else {
      message.max_bytes = 0;
    }
    return message;
  },

  toJSON(message: EvidenceParams): unknown {
    const obj: any = {};
    message.max_age_num_blocks !== undefined &&
      (obj.max_age_num_blocks = message.max_age_num_blocks);
    message.max_age_duration !== undefined &&
      (obj.max_age_duration = message.max_age_duration
        ? Duration.toJSON(message.max_age_duration)
        : undefined);
    message.max_bytes !== undefined && (obj.max_bytes = message.max_bytes);
    return obj;
  },

  fromPartial(object: DeepPartial<EvidenceParams>): EvidenceParams {
    const message = { ...baseEvidenceParams } as EvidenceParams;
    if (
      object.max_age_num_blocks !== undefined &&
      object.max_age_num_blocks !== null
    ) {
      message.max_age_num_blocks = object.max_age_num_blocks;
    } else {
      message.max_age_num_blocks = 0;
    }
    if (
      object.max_age_duration !== undefined &&
      object.max_age_duration !== null
    ) {
      message.max_age_duration = Duration.fromPartial(object.max_age_duration);
    } else {
      message.max_age_duration = undefined;
    }
    if (object.max_bytes !== undefined && object.max_bytes !== null) {
      message.max_bytes = object.max_bytes;
    } else {
      message.max_bytes = 0;
    }
    return message;
  },
};

const baseValidatorParams: object = { pub_key_types: "" };

export const ValidatorParams = {
  encode(message: ValidatorParams, writer: Writer = Writer.create()): Writer {
    for (const v of message.pub_key_types) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorParams } as ValidatorParams;
    message.pub_key_types = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pub_key_types.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorParams {
    const message = { ...baseValidatorParams } as ValidatorParams;
    message.pub_key_types = [];
    if (object.pub_key_types !== undefined && object.pub_key_types !== null) {
      for (const e of object.pub_key_types) {
        message.pub_key_types.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorParams): unknown {
    const obj: any = {};
    if (message.pub_key_types) {
      obj.pub_key_types = message.pub_key_types.map((e) => e);
    } else {
      obj.pub_key_types = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorParams>): ValidatorParams {
    const message = { ...baseValidatorParams } as ValidatorParams;
    message.pub_key_types = [];
    if (object.pub_key_types !== undefined && object.pub_key_types !== null) {
      for (const e of object.pub_key_types) {
        message.pub_key_types.push(e);
      }
    }
    return message;
  },
};

const baseVersionParams: object = { app_version: 0 };

export const VersionParams = {
  encode(message: VersionParams, writer: Writer = Writer.create()): Writer {
    if (message.app_version !== 0) {
      writer.uint32(8).uint64(message.app_version);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): VersionParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseVersionParams } as VersionParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.app_version = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): VersionParams {
    const message = { ...baseVersionParams } as VersionParams;
    if (object.app_version !== undefined && object.app_version !== null) {
      message.app_version = Number(object.app_version);
    } else {
      message.app_version = 0;
    }
    return message;
  },

  toJSON(message: VersionParams): unknown {
    const obj: any = {};
    message.app_version !== undefined &&
      (obj.app_version = message.app_version);
    return obj;
  },

  fromPartial(object: DeepPartial<VersionParams>): VersionParams {
    const message = { ...baseVersionParams } as VersionParams;
    if (object.app_version !== undefined && object.app_version !== null) {
      message.app_version = object.app_version;
    } else {
      message.app_version = 0;
    }
    return message;
  },
};

const baseHashedParams: object = { block_max_bytes: 0, block_max_gas: 0 };

export const HashedParams = {
  encode(message: HashedParams, writer: Writer = Writer.create()): Writer {
    if (message.block_max_bytes !== 0) {
      writer.uint32(8).int64(message.block_max_bytes);
    }
    if (message.block_max_gas !== 0) {
      writer.uint32(16).int64(message.block_max_gas);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): HashedParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseHashedParams } as HashedParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.block_max_bytes = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.block_max_gas = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): HashedParams {
    const message = { ...baseHashedParams } as HashedParams;
    if (
      object.block_max_bytes !== undefined &&
      object.block_max_bytes !== null
    ) {
      message.block_max_bytes = Number(object.block_max_bytes);
    } else {
      message.block_max_bytes = 0;
    }
    if (object.block_max_gas !== undefined && object.block_max_gas !== null) {
      message.block_max_gas = Number(object.block_max_gas);
    } else {
      message.block_max_gas = 0;
    }
    return message;
  },

  toJSON(message: HashedParams): unknown {
    const obj: any = {};
    message.block_max_bytes !== undefined &&
      (obj.block_max_bytes = message.block_max_bytes);
    message.block_max_gas !== undefined &&
      (obj.block_max_gas = message.block_max_gas);
    return obj;
  },

  fromPartial(object: DeepPartial<HashedParams>): HashedParams {
    const message = { ...baseHashedParams } as HashedParams;
    if (
      object.block_max_bytes !== undefined &&
      object.block_max_bytes !== null
    ) {
      message.block_max_bytes = object.block_max_bytes;
    } else {
      message.block_max_bytes = 0;
    }
    if (object.block_max_gas !== undefined && object.block_max_gas !== null) {
      message.block_max_gas = object.block_max_gas;
    } else {
      message.block_max_gas = 0;
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
