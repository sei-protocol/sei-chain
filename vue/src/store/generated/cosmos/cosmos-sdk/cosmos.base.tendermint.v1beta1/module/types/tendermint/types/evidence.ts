/* eslint-disable */
import { Timestamp } from "../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Vote, LightBlock } from "../../tendermint/types/types";
import { Validator } from "../../tendermint/types/validator";

export const protobufPackage = "tendermint.types";

export interface Evidence {
  duplicate_vote_evidence: DuplicateVoteEvidence | undefined;
  light_client_attack_evidence: LightClientAttackEvidence | undefined;
}

/** DuplicateVoteEvidence contains evidence of a validator signed two conflicting votes. */
export interface DuplicateVoteEvidence {
  vote_a: Vote | undefined;
  vote_b: Vote | undefined;
  total_voting_power: number;
  validator_power: number;
  timestamp: Date | undefined;
}

/** LightClientAttackEvidence contains evidence of a set of validators attempting to mislead a light client. */
export interface LightClientAttackEvidence {
  conflicting_block: LightBlock | undefined;
  common_height: number;
  byzantine_validators: Validator[];
  total_voting_power: number;
  timestamp: Date | undefined;
}

export interface EvidenceList {
  evidence: Evidence[];
}

const baseEvidence: object = {};

export const Evidence = {
  encode(message: Evidence, writer: Writer = Writer.create()): Writer {
    if (message.duplicate_vote_evidence !== undefined) {
      DuplicateVoteEvidence.encode(
        message.duplicate_vote_evidence,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.light_client_attack_evidence !== undefined) {
      LightClientAttackEvidence.encode(
        message.light_client_attack_evidence,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Evidence {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEvidence } as Evidence;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.duplicate_vote_evidence = DuplicateVoteEvidence.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.light_client_attack_evidence = LightClientAttackEvidence.decode(
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

  fromJSON(object: any): Evidence {
    const message = { ...baseEvidence } as Evidence;
    if (
      object.duplicate_vote_evidence !== undefined &&
      object.duplicate_vote_evidence !== null
    ) {
      message.duplicate_vote_evidence = DuplicateVoteEvidence.fromJSON(
        object.duplicate_vote_evidence
      );
    } else {
      message.duplicate_vote_evidence = undefined;
    }
    if (
      object.light_client_attack_evidence !== undefined &&
      object.light_client_attack_evidence !== null
    ) {
      message.light_client_attack_evidence = LightClientAttackEvidence.fromJSON(
        object.light_client_attack_evidence
      );
    } else {
      message.light_client_attack_evidence = undefined;
    }
    return message;
  },

  toJSON(message: Evidence): unknown {
    const obj: any = {};
    message.duplicate_vote_evidence !== undefined &&
      (obj.duplicate_vote_evidence = message.duplicate_vote_evidence
        ? DuplicateVoteEvidence.toJSON(message.duplicate_vote_evidence)
        : undefined);
    message.light_client_attack_evidence !== undefined &&
      (obj.light_client_attack_evidence = message.light_client_attack_evidence
        ? LightClientAttackEvidence.toJSON(message.light_client_attack_evidence)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<Evidence>): Evidence {
    const message = { ...baseEvidence } as Evidence;
    if (
      object.duplicate_vote_evidence !== undefined &&
      object.duplicate_vote_evidence !== null
    ) {
      message.duplicate_vote_evidence = DuplicateVoteEvidence.fromPartial(
        object.duplicate_vote_evidence
      );
    } else {
      message.duplicate_vote_evidence = undefined;
    }
    if (
      object.light_client_attack_evidence !== undefined &&
      object.light_client_attack_evidence !== null
    ) {
      message.light_client_attack_evidence = LightClientAttackEvidence.fromPartial(
        object.light_client_attack_evidence
      );
    } else {
      message.light_client_attack_evidence = undefined;
    }
    return message;
  },
};

const baseDuplicateVoteEvidence: object = {
  total_voting_power: 0,
  validator_power: 0,
};

export const DuplicateVoteEvidence = {
  encode(
    message: DuplicateVoteEvidence,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.vote_a !== undefined) {
      Vote.encode(message.vote_a, writer.uint32(10).fork()).ldelim();
    }
    if (message.vote_b !== undefined) {
      Vote.encode(message.vote_b, writer.uint32(18).fork()).ldelim();
    }
    if (message.total_voting_power !== 0) {
      writer.uint32(24).int64(message.total_voting_power);
    }
    if (message.validator_power !== 0) {
      writer.uint32(32).int64(message.validator_power);
    }
    if (message.timestamp !== undefined) {
      Timestamp.encode(
        toTimestamp(message.timestamp),
        writer.uint32(42).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DuplicateVoteEvidence {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDuplicateVoteEvidence } as DuplicateVoteEvidence;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.vote_a = Vote.decode(reader, reader.uint32());
          break;
        case 2:
          message.vote_b = Vote.decode(reader, reader.uint32());
          break;
        case 3:
          message.total_voting_power = longToNumber(reader.int64() as Long);
          break;
        case 4:
          message.validator_power = longToNumber(reader.int64() as Long);
          break;
        case 5:
          message.timestamp = fromTimestamp(
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

  fromJSON(object: any): DuplicateVoteEvidence {
    const message = { ...baseDuplicateVoteEvidence } as DuplicateVoteEvidence;
    if (object.vote_a !== undefined && object.vote_a !== null) {
      message.vote_a = Vote.fromJSON(object.vote_a);
    } else {
      message.vote_a = undefined;
    }
    if (object.vote_b !== undefined && object.vote_b !== null) {
      message.vote_b = Vote.fromJSON(object.vote_b);
    } else {
      message.vote_b = undefined;
    }
    if (
      object.total_voting_power !== undefined &&
      object.total_voting_power !== null
    ) {
      message.total_voting_power = Number(object.total_voting_power);
    } else {
      message.total_voting_power = 0;
    }
    if (
      object.validator_power !== undefined &&
      object.validator_power !== null
    ) {
      message.validator_power = Number(object.validator_power);
    } else {
      message.validator_power = 0;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = fromJsonTimestamp(object.timestamp);
    } else {
      message.timestamp = undefined;
    }
    return message;
  },

  toJSON(message: DuplicateVoteEvidence): unknown {
    const obj: any = {};
    message.vote_a !== undefined &&
      (obj.vote_a = message.vote_a ? Vote.toJSON(message.vote_a) : undefined);
    message.vote_b !== undefined &&
      (obj.vote_b = message.vote_b ? Vote.toJSON(message.vote_b) : undefined);
    message.total_voting_power !== undefined &&
      (obj.total_voting_power = message.total_voting_power);
    message.validator_power !== undefined &&
      (obj.validator_power = message.validator_power);
    message.timestamp !== undefined &&
      (obj.timestamp =
        message.timestamp !== undefined
          ? message.timestamp.toISOString()
          : null);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DuplicateVoteEvidence>
  ): DuplicateVoteEvidence {
    const message = { ...baseDuplicateVoteEvidence } as DuplicateVoteEvidence;
    if (object.vote_a !== undefined && object.vote_a !== null) {
      message.vote_a = Vote.fromPartial(object.vote_a);
    } else {
      message.vote_a = undefined;
    }
    if (object.vote_b !== undefined && object.vote_b !== null) {
      message.vote_b = Vote.fromPartial(object.vote_b);
    } else {
      message.vote_b = undefined;
    }
    if (
      object.total_voting_power !== undefined &&
      object.total_voting_power !== null
    ) {
      message.total_voting_power = object.total_voting_power;
    } else {
      message.total_voting_power = 0;
    }
    if (
      object.validator_power !== undefined &&
      object.validator_power !== null
    ) {
      message.validator_power = object.validator_power;
    } else {
      message.validator_power = 0;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = undefined;
    }
    return message;
  },
};

const baseLightClientAttackEvidence: object = {
  common_height: 0,
  total_voting_power: 0,
};

export const LightClientAttackEvidence = {
  encode(
    message: LightClientAttackEvidence,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.conflicting_block !== undefined) {
      LightBlock.encode(
        message.conflicting_block,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.common_height !== 0) {
      writer.uint32(16).int64(message.common_height);
    }
    for (const v of message.byzantine_validators) {
      Validator.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    if (message.total_voting_power !== 0) {
      writer.uint32(32).int64(message.total_voting_power);
    }
    if (message.timestamp !== undefined) {
      Timestamp.encode(
        toTimestamp(message.timestamp),
        writer.uint32(42).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): LightClientAttackEvidence {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseLightClientAttackEvidence,
    } as LightClientAttackEvidence;
    message.byzantine_validators = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.conflicting_block = LightBlock.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.common_height = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.byzantine_validators.push(
            Validator.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.total_voting_power = longToNumber(reader.int64() as Long);
          break;
        case 5:
          message.timestamp = fromTimestamp(
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

  fromJSON(object: any): LightClientAttackEvidence {
    const message = {
      ...baseLightClientAttackEvidence,
    } as LightClientAttackEvidence;
    message.byzantine_validators = [];
    if (
      object.conflicting_block !== undefined &&
      object.conflicting_block !== null
    ) {
      message.conflicting_block = LightBlock.fromJSON(object.conflicting_block);
    } else {
      message.conflicting_block = undefined;
    }
    if (object.common_height !== undefined && object.common_height !== null) {
      message.common_height = Number(object.common_height);
    } else {
      message.common_height = 0;
    }
    if (
      object.byzantine_validators !== undefined &&
      object.byzantine_validators !== null
    ) {
      for (const e of object.byzantine_validators) {
        message.byzantine_validators.push(Validator.fromJSON(e));
      }
    }
    if (
      object.total_voting_power !== undefined &&
      object.total_voting_power !== null
    ) {
      message.total_voting_power = Number(object.total_voting_power);
    } else {
      message.total_voting_power = 0;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = fromJsonTimestamp(object.timestamp);
    } else {
      message.timestamp = undefined;
    }
    return message;
  },

  toJSON(message: LightClientAttackEvidence): unknown {
    const obj: any = {};
    message.conflicting_block !== undefined &&
      (obj.conflicting_block = message.conflicting_block
        ? LightBlock.toJSON(message.conflicting_block)
        : undefined);
    message.common_height !== undefined &&
      (obj.common_height = message.common_height);
    if (message.byzantine_validators) {
      obj.byzantine_validators = message.byzantine_validators.map((e) =>
        e ? Validator.toJSON(e) : undefined
      );
    } else {
      obj.byzantine_validators = [];
    }
    message.total_voting_power !== undefined &&
      (obj.total_voting_power = message.total_voting_power);
    message.timestamp !== undefined &&
      (obj.timestamp =
        message.timestamp !== undefined
          ? message.timestamp.toISOString()
          : null);
    return obj;
  },

  fromPartial(
    object: DeepPartial<LightClientAttackEvidence>
  ): LightClientAttackEvidence {
    const message = {
      ...baseLightClientAttackEvidence,
    } as LightClientAttackEvidence;
    message.byzantine_validators = [];
    if (
      object.conflicting_block !== undefined &&
      object.conflicting_block !== null
    ) {
      message.conflicting_block = LightBlock.fromPartial(
        object.conflicting_block
      );
    } else {
      message.conflicting_block = undefined;
    }
    if (object.common_height !== undefined && object.common_height !== null) {
      message.common_height = object.common_height;
    } else {
      message.common_height = 0;
    }
    if (
      object.byzantine_validators !== undefined &&
      object.byzantine_validators !== null
    ) {
      for (const e of object.byzantine_validators) {
        message.byzantine_validators.push(Validator.fromPartial(e));
      }
    }
    if (
      object.total_voting_power !== undefined &&
      object.total_voting_power !== null
    ) {
      message.total_voting_power = object.total_voting_power;
    } else {
      message.total_voting_power = 0;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = undefined;
    }
    return message;
  },
};

const baseEvidenceList: object = {};

export const EvidenceList = {
  encode(message: EvidenceList, writer: Writer = Writer.create()): Writer {
    for (const v of message.evidence) {
      Evidence.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): EvidenceList {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEvidenceList } as EvidenceList;
    message.evidence = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.evidence.push(Evidence.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): EvidenceList {
    const message = { ...baseEvidenceList } as EvidenceList;
    message.evidence = [];
    if (object.evidence !== undefined && object.evidence !== null) {
      for (const e of object.evidence) {
        message.evidence.push(Evidence.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: EvidenceList): unknown {
    const obj: any = {};
    if (message.evidence) {
      obj.evidence = message.evidence.map((e) =>
        e ? Evidence.toJSON(e) : undefined
      );
    } else {
      obj.evidence = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<EvidenceList>): EvidenceList {
    const message = { ...baseEvidenceList } as EvidenceList;
    message.evidence = [];
    if (object.evidence !== undefined && object.evidence !== null) {
      for (const e of object.evidence) {
        message.evidence.push(Evidence.fromPartial(e));
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
