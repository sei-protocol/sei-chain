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
  synchrony: SynchronyParams | undefined;
  timeout: TimeoutParams | undefined;
  abci: ABCIParams | undefined;
}

/** BlockParams contains limits on the block size. */
export interface BlockParams {
  /**
   * Max block size, in bytes.
   * Note: must be greater than 0
   */
  maxBytes: number;
  /**
   * Max gas per block.
   * Note: must be greater or equal to -1
   */
  maxGas: number;
}

/** EvidenceParams determine how we handle evidence of malfeasance. */
export interface EvidenceParams {
  /**
   * Max age of evidence, in blocks.
   *
   * The basic formula for calculating this is: MaxAgeDuration / {average block
   * time}.
   */
  maxAgeNumBlocks: number;
  /**
   * Max age of evidence, in time.
   *
   * It should correspond with an app's "unbonding period" or other similar
   * mechanism for handling [Nothing-At-Stake
   * attacks](https://github.com/ethereum/wiki/wiki/Proof-of-Stake-FAQ#what-is-the-nothing-at-stake-problem-and-how-can-it-be-fixed).
   */
  maxAgeDuration: Duration | undefined;
  /**
   * This sets the maximum size of total evidence in bytes that can be committed
   * in a single block. and should fall comfortably under the max block bytes.
   * Default is 1048576 or 1MB
   */
  maxBytes: number;
}

/**
 * ValidatorParams restrict the public key types validators can use.
 * NOTE: uses ABCI pubkey naming, not Amino names.
 */
export interface ValidatorParams {
  pubKeyTypes: string[];
}

/** VersionParams contains the ABCI application version. */
export interface VersionParams {
  appVersion: number;
}

/**
 * HashedParams is a subset of ConsensusParams.
 *
 * It is hashed into the Header.ConsensusHash.
 */
export interface HashedParams {
  blockMaxBytes: number;
  blockMaxGas: number;
}

/**
 * SynchronyParams configure the bounds under which a proposed block's timestamp is considered valid.
 * These parameters are part of the proposer-based timestamps algorithm. For more information,
 * see the specification of proposer-based timestamps:
 * https://github.com/tendermint/tendermint/tree/master/spec/consensus/proposer-based-timestamp
 */
export interface SynchronyParams {
  /**
   * message_delay bounds how long a proposal message may take to reach all validators on a network
   * and still be considered valid.
   */
  messageDelay: Duration | undefined;
  /**
   * precision bounds how skewed a proposer's clock may be from any validator
   * on the network while still producing valid proposals.
   */
  precision: Duration | undefined;
}

/** TimeoutParams configure the timeouts for the steps of the Tendermint consensus algorithm. */
export interface TimeoutParams {
  /**
   * These fields configure the timeouts for the propose step of the Tendermint
   * consensus algorithm: propose is the initial timeout and propose_delta
   * determines how much the timeout grows in subsequent rounds.
   * For the first round, this propose timeout is used and for every subsequent
   * round, the timeout grows by propose_delta.
   *
   * For example:
   * With propose = 10ms, propose_delta = 5ms, the first round's propose phase
   * timeout would be 10ms, the second round's would be 15ms, the third 20ms and so on.
   *
   * If a node waiting for a proposal message does not receive one matching its
   * current height and round before this timeout, the node will issue a
   * nil prevote for the round and advance to the next step.
   */
  propose: Duration | undefined;
  proposeDelta: Duration | undefined;
  /**
   * vote along with vote_delta configure the timeout for both of the prevote and
   * precommit steps of the Tendermint consensus algorithm.
   *
   * These parameters influence the vote step timeouts in the the same way that
   * the propose and propose_delta parameters do to the proposal step.
   *
   * The vote timeout does not begin until a quorum of votes has been received. Once
   * a quorum of votes has been seen and this timeout elapses, Tendermint will
   * procced to the next step of the consensus algorithm. If Tendermint receives
   * all of the remaining votes before the end of the timeout, it will proceed
   * to the next step immediately.
   */
  vote: Duration | undefined;
  voteDelta: Duration | undefined;
  /**
   * commit configures how long Tendermint will wait after receiving a quorum of
   * precommits before beginning consensus for the next height. This can be
   * used to allow slow precommits to arrive for inclusion in the next height before progressing.
   */
  commit: Duration | undefined;
  /**
   * bypass_commit_timeout configures the node to proceed immediately to
   * the next height once the node has received all precommits for a block, forgoing
   * the remaining commit timeout.
   * Setting bypass_commit_timeout false (the default) causes Tendermint to wait
   * for the full commit timeout.
   */
  bypassCommitTimeout: boolean;
}

/** ABCIParams configure functionality specific to the Application Blockchain Interface. */
export interface ABCIParams {
  /**
   * vote_extensions_enable_height configures the first height during which
   * vote extensions will be enabled. During this specified height, and for all
   * subsequent heights, precommit messages that do not contain valid extension data
   * will be considered invalid. Prior to this height, vote extensions will not
   * be used or accepted by validators on the network.
   *
   * Once enabled, vote extensions will be created by the application in ExtendVote,
   * passed to the application for validation in VerifyVoteExtension and given
   * to the application to use when proposing a block during PrepareProposal.
   */
  voteExtensionsEnableHeight: number;
  /**
   * Indicates if CheckTx should be called on all the transactions
   * remaining in the mempool after a block is executed.
   */
  recheckTx: boolean;
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
    if (message.synchrony !== undefined) {
      SynchronyParams.encode(
        message.synchrony,
        writer.uint32(42).fork()
      ).ldelim();
    }
    if (message.timeout !== undefined) {
      TimeoutParams.encode(message.timeout, writer.uint32(50).fork()).ldelim();
    }
    if (message.abci !== undefined) {
      ABCIParams.encode(message.abci, writer.uint32(58).fork()).ldelim();
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
        case 5:
          message.synchrony = SynchronyParams.decode(reader, reader.uint32());
          break;
        case 6:
          message.timeout = TimeoutParams.decode(reader, reader.uint32());
          break;
        case 7:
          message.abci = ABCIParams.decode(reader, reader.uint32());
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
    if (object.synchrony !== undefined && object.synchrony !== null) {
      message.synchrony = SynchronyParams.fromJSON(object.synchrony);
    } else {
      message.synchrony = undefined;
    }
    if (object.timeout !== undefined && object.timeout !== null) {
      message.timeout = TimeoutParams.fromJSON(object.timeout);
    } else {
      message.timeout = undefined;
    }
    if (object.abci !== undefined && object.abci !== null) {
      message.abci = ABCIParams.fromJSON(object.abci);
    } else {
      message.abci = undefined;
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
    message.synchrony !== undefined &&
      (obj.synchrony = message.synchrony
        ? SynchronyParams.toJSON(message.synchrony)
        : undefined);
    message.timeout !== undefined &&
      (obj.timeout = message.timeout
        ? TimeoutParams.toJSON(message.timeout)
        : undefined);
    message.abci !== undefined &&
      (obj.abci = message.abci ? ABCIParams.toJSON(message.abci) : undefined);
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
    if (object.synchrony !== undefined && object.synchrony !== null) {
      message.synchrony = SynchronyParams.fromPartial(object.synchrony);
    } else {
      message.synchrony = undefined;
    }
    if (object.timeout !== undefined && object.timeout !== null) {
      message.timeout = TimeoutParams.fromPartial(object.timeout);
    } else {
      message.timeout = undefined;
    }
    if (object.abci !== undefined && object.abci !== null) {
      message.abci = ABCIParams.fromPartial(object.abci);
    } else {
      message.abci = undefined;
    }
    return message;
  },
};

const baseBlockParams: object = { maxBytes: 0, maxGas: 0 };

export const BlockParams = {
  encode(message: BlockParams, writer: Writer = Writer.create()): Writer {
    if (message.maxBytes !== 0) {
      writer.uint32(8).int64(message.maxBytes);
    }
    if (message.maxGas !== 0) {
      writer.uint32(16).int64(message.maxGas);
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
          message.maxBytes = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.maxGas = longToNumber(reader.int64() as Long);
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
    if (object.maxBytes !== undefined && object.maxBytes !== null) {
      message.maxBytes = Number(object.maxBytes);
    } else {
      message.maxBytes = 0;
    }
    if (object.maxGas !== undefined && object.maxGas !== null) {
      message.maxGas = Number(object.maxGas);
    } else {
      message.maxGas = 0;
    }
    return message;
  },

  toJSON(message: BlockParams): unknown {
    const obj: any = {};
    message.maxBytes !== undefined && (obj.maxBytes = message.maxBytes);
    message.maxGas !== undefined && (obj.maxGas = message.maxGas);
    return obj;
  },

  fromPartial(object: DeepPartial<BlockParams>): BlockParams {
    const message = { ...baseBlockParams } as BlockParams;
    if (object.maxBytes !== undefined && object.maxBytes !== null) {
      message.maxBytes = object.maxBytes;
    } else {
      message.maxBytes = 0;
    }
    if (object.maxGas !== undefined && object.maxGas !== null) {
      message.maxGas = object.maxGas;
    } else {
      message.maxGas = 0;
    }
    return message;
  },
};

const baseEvidenceParams: object = { maxAgeNumBlocks: 0, maxBytes: 0 };

export const EvidenceParams = {
  encode(message: EvidenceParams, writer: Writer = Writer.create()): Writer {
    if (message.maxAgeNumBlocks !== 0) {
      writer.uint32(8).int64(message.maxAgeNumBlocks);
    }
    if (message.maxAgeDuration !== undefined) {
      Duration.encode(
        message.maxAgeDuration,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.maxBytes !== 0) {
      writer.uint32(24).int64(message.maxBytes);
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
          message.maxAgeNumBlocks = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.maxAgeDuration = Duration.decode(reader, reader.uint32());
          break;
        case 3:
          message.maxBytes = longToNumber(reader.int64() as Long);
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
      object.maxAgeNumBlocks !== undefined &&
      object.maxAgeNumBlocks !== null
    ) {
      message.maxAgeNumBlocks = Number(object.maxAgeNumBlocks);
    } else {
      message.maxAgeNumBlocks = 0;
    }
    if (object.maxAgeDuration !== undefined && object.maxAgeDuration !== null) {
      message.maxAgeDuration = Duration.fromJSON(object.maxAgeDuration);
    } else {
      message.maxAgeDuration = undefined;
    }
    if (object.maxBytes !== undefined && object.maxBytes !== null) {
      message.maxBytes = Number(object.maxBytes);
    } else {
      message.maxBytes = 0;
    }
    return message;
  },

  toJSON(message: EvidenceParams): unknown {
    const obj: any = {};
    message.maxAgeNumBlocks !== undefined &&
      (obj.maxAgeNumBlocks = message.maxAgeNumBlocks);
    message.maxAgeDuration !== undefined &&
      (obj.maxAgeDuration = message.maxAgeDuration
        ? Duration.toJSON(message.maxAgeDuration)
        : undefined);
    message.maxBytes !== undefined && (obj.maxBytes = message.maxBytes);
    return obj;
  },

  fromPartial(object: DeepPartial<EvidenceParams>): EvidenceParams {
    const message = { ...baseEvidenceParams } as EvidenceParams;
    if (
      object.maxAgeNumBlocks !== undefined &&
      object.maxAgeNumBlocks !== null
    ) {
      message.maxAgeNumBlocks = object.maxAgeNumBlocks;
    } else {
      message.maxAgeNumBlocks = 0;
    }
    if (object.maxAgeDuration !== undefined && object.maxAgeDuration !== null) {
      message.maxAgeDuration = Duration.fromPartial(object.maxAgeDuration);
    } else {
      message.maxAgeDuration = undefined;
    }
    if (object.maxBytes !== undefined && object.maxBytes !== null) {
      message.maxBytes = object.maxBytes;
    } else {
      message.maxBytes = 0;
    }
    return message;
  },
};

const baseValidatorParams: object = { pubKeyTypes: "" };

export const ValidatorParams = {
  encode(message: ValidatorParams, writer: Writer = Writer.create()): Writer {
    for (const v of message.pubKeyTypes) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorParams } as ValidatorParams;
    message.pubKeyTypes = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pubKeyTypes.push(reader.string());
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
    message.pubKeyTypes = [];
    if (object.pubKeyTypes !== undefined && object.pubKeyTypes !== null) {
      for (const e of object.pubKeyTypes) {
        message.pubKeyTypes.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorParams): unknown {
    const obj: any = {};
    if (message.pubKeyTypes) {
      obj.pubKeyTypes = message.pubKeyTypes.map((e) => e);
    } else {
      obj.pubKeyTypes = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorParams>): ValidatorParams {
    const message = { ...baseValidatorParams } as ValidatorParams;
    message.pubKeyTypes = [];
    if (object.pubKeyTypes !== undefined && object.pubKeyTypes !== null) {
      for (const e of object.pubKeyTypes) {
        message.pubKeyTypes.push(e);
      }
    }
    return message;
  },
};

const baseVersionParams: object = { appVersion: 0 };

export const VersionParams = {
  encode(message: VersionParams, writer: Writer = Writer.create()): Writer {
    if (message.appVersion !== 0) {
      writer.uint32(8).uint64(message.appVersion);
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
          message.appVersion = longToNumber(reader.uint64() as Long);
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
    if (object.appVersion !== undefined && object.appVersion !== null) {
      message.appVersion = Number(object.appVersion);
    } else {
      message.appVersion = 0;
    }
    return message;
  },

  toJSON(message: VersionParams): unknown {
    const obj: any = {};
    message.appVersion !== undefined && (obj.appVersion = message.appVersion);
    return obj;
  },

  fromPartial(object: DeepPartial<VersionParams>): VersionParams {
    const message = { ...baseVersionParams } as VersionParams;
    if (object.appVersion !== undefined && object.appVersion !== null) {
      message.appVersion = object.appVersion;
    } else {
      message.appVersion = 0;
    }
    return message;
  },
};

const baseHashedParams: object = { blockMaxBytes: 0, blockMaxGas: 0 };

export const HashedParams = {
  encode(message: HashedParams, writer: Writer = Writer.create()): Writer {
    if (message.blockMaxBytes !== 0) {
      writer.uint32(8).int64(message.blockMaxBytes);
    }
    if (message.blockMaxGas !== 0) {
      writer.uint32(16).int64(message.blockMaxGas);
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
          message.blockMaxBytes = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.blockMaxGas = longToNumber(reader.int64() as Long);
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
    if (object.blockMaxBytes !== undefined && object.blockMaxBytes !== null) {
      message.blockMaxBytes = Number(object.blockMaxBytes);
    } else {
      message.blockMaxBytes = 0;
    }
    if (object.blockMaxGas !== undefined && object.blockMaxGas !== null) {
      message.blockMaxGas = Number(object.blockMaxGas);
    } else {
      message.blockMaxGas = 0;
    }
    return message;
  },

  toJSON(message: HashedParams): unknown {
    const obj: any = {};
    message.blockMaxBytes !== undefined &&
      (obj.blockMaxBytes = message.blockMaxBytes);
    message.blockMaxGas !== undefined &&
      (obj.blockMaxGas = message.blockMaxGas);
    return obj;
  },

  fromPartial(object: DeepPartial<HashedParams>): HashedParams {
    const message = { ...baseHashedParams } as HashedParams;
    if (object.blockMaxBytes !== undefined && object.blockMaxBytes !== null) {
      message.blockMaxBytes = object.blockMaxBytes;
    } else {
      message.blockMaxBytes = 0;
    }
    if (object.blockMaxGas !== undefined && object.blockMaxGas !== null) {
      message.blockMaxGas = object.blockMaxGas;
    } else {
      message.blockMaxGas = 0;
    }
    return message;
  },
};

const baseSynchronyParams: object = {};

export const SynchronyParams = {
  encode(message: SynchronyParams, writer: Writer = Writer.create()): Writer {
    if (message.messageDelay !== undefined) {
      Duration.encode(message.messageDelay, writer.uint32(10).fork()).ldelim();
    }
    if (message.precision !== undefined) {
      Duration.encode(message.precision, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SynchronyParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSynchronyParams } as SynchronyParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageDelay = Duration.decode(reader, reader.uint32());
          break;
        case 2:
          message.precision = Duration.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SynchronyParams {
    const message = { ...baseSynchronyParams } as SynchronyParams;
    if (object.messageDelay !== undefined && object.messageDelay !== null) {
      message.messageDelay = Duration.fromJSON(object.messageDelay);
    } else {
      message.messageDelay = undefined;
    }
    if (object.precision !== undefined && object.precision !== null) {
      message.precision = Duration.fromJSON(object.precision);
    } else {
      message.precision = undefined;
    }
    return message;
  },

  toJSON(message: SynchronyParams): unknown {
    const obj: any = {};
    message.messageDelay !== undefined &&
      (obj.messageDelay = message.messageDelay
        ? Duration.toJSON(message.messageDelay)
        : undefined);
    message.precision !== undefined &&
      (obj.precision = message.precision
        ? Duration.toJSON(message.precision)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<SynchronyParams>): SynchronyParams {
    const message = { ...baseSynchronyParams } as SynchronyParams;
    if (object.messageDelay !== undefined && object.messageDelay !== null) {
      message.messageDelay = Duration.fromPartial(object.messageDelay);
    } else {
      message.messageDelay = undefined;
    }
    if (object.precision !== undefined && object.precision !== null) {
      message.precision = Duration.fromPartial(object.precision);
    } else {
      message.precision = undefined;
    }
    return message;
  },
};

const baseTimeoutParams: object = { bypassCommitTimeout: false };

export const TimeoutParams = {
  encode(message: TimeoutParams, writer: Writer = Writer.create()): Writer {
    if (message.propose !== undefined) {
      Duration.encode(message.propose, writer.uint32(10).fork()).ldelim();
    }
    if (message.proposeDelta !== undefined) {
      Duration.encode(message.proposeDelta, writer.uint32(18).fork()).ldelim();
    }
    if (message.vote !== undefined) {
      Duration.encode(message.vote, writer.uint32(26).fork()).ldelim();
    }
    if (message.voteDelta !== undefined) {
      Duration.encode(message.voteDelta, writer.uint32(34).fork()).ldelim();
    }
    if (message.commit !== undefined) {
      Duration.encode(message.commit, writer.uint32(42).fork()).ldelim();
    }
    if (message.bypassCommitTimeout === true) {
      writer.uint32(48).bool(message.bypassCommitTimeout);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): TimeoutParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTimeoutParams } as TimeoutParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.propose = Duration.decode(reader, reader.uint32());
          break;
        case 2:
          message.proposeDelta = Duration.decode(reader, reader.uint32());
          break;
        case 3:
          message.vote = Duration.decode(reader, reader.uint32());
          break;
        case 4:
          message.voteDelta = Duration.decode(reader, reader.uint32());
          break;
        case 5:
          message.commit = Duration.decode(reader, reader.uint32());
          break;
        case 6:
          message.bypassCommitTimeout = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): TimeoutParams {
    const message = { ...baseTimeoutParams } as TimeoutParams;
    if (object.propose !== undefined && object.propose !== null) {
      message.propose = Duration.fromJSON(object.propose);
    } else {
      message.propose = undefined;
    }
    if (object.proposeDelta !== undefined && object.proposeDelta !== null) {
      message.proposeDelta = Duration.fromJSON(object.proposeDelta);
    } else {
      message.proposeDelta = undefined;
    }
    if (object.vote !== undefined && object.vote !== null) {
      message.vote = Duration.fromJSON(object.vote);
    } else {
      message.vote = undefined;
    }
    if (object.voteDelta !== undefined && object.voteDelta !== null) {
      message.voteDelta = Duration.fromJSON(object.voteDelta);
    } else {
      message.voteDelta = undefined;
    }
    if (object.commit !== undefined && object.commit !== null) {
      message.commit = Duration.fromJSON(object.commit);
    } else {
      message.commit = undefined;
    }
    if (
      object.bypassCommitTimeout !== undefined &&
      object.bypassCommitTimeout !== null
    ) {
      message.bypassCommitTimeout = Boolean(object.bypassCommitTimeout);
    } else {
      message.bypassCommitTimeout = false;
    }
    return message;
  },

  toJSON(message: TimeoutParams): unknown {
    const obj: any = {};
    message.propose !== undefined &&
      (obj.propose = message.propose
        ? Duration.toJSON(message.propose)
        : undefined);
    message.proposeDelta !== undefined &&
      (obj.proposeDelta = message.proposeDelta
        ? Duration.toJSON(message.proposeDelta)
        : undefined);
    message.vote !== undefined &&
      (obj.vote = message.vote ? Duration.toJSON(message.vote) : undefined);
    message.voteDelta !== undefined &&
      (obj.voteDelta = message.voteDelta
        ? Duration.toJSON(message.voteDelta)
        : undefined);
    message.commit !== undefined &&
      (obj.commit = message.commit
        ? Duration.toJSON(message.commit)
        : undefined);
    message.bypassCommitTimeout !== undefined &&
      (obj.bypassCommitTimeout = message.bypassCommitTimeout);
    return obj;
  },

  fromPartial(object: DeepPartial<TimeoutParams>): TimeoutParams {
    const message = { ...baseTimeoutParams } as TimeoutParams;
    if (object.propose !== undefined && object.propose !== null) {
      message.propose = Duration.fromPartial(object.propose);
    } else {
      message.propose = undefined;
    }
    if (object.proposeDelta !== undefined && object.proposeDelta !== null) {
      message.proposeDelta = Duration.fromPartial(object.proposeDelta);
    } else {
      message.proposeDelta = undefined;
    }
    if (object.vote !== undefined && object.vote !== null) {
      message.vote = Duration.fromPartial(object.vote);
    } else {
      message.vote = undefined;
    }
    if (object.voteDelta !== undefined && object.voteDelta !== null) {
      message.voteDelta = Duration.fromPartial(object.voteDelta);
    } else {
      message.voteDelta = undefined;
    }
    if (object.commit !== undefined && object.commit !== null) {
      message.commit = Duration.fromPartial(object.commit);
    } else {
      message.commit = undefined;
    }
    if (
      object.bypassCommitTimeout !== undefined &&
      object.bypassCommitTimeout !== null
    ) {
      message.bypassCommitTimeout = object.bypassCommitTimeout;
    } else {
      message.bypassCommitTimeout = false;
    }
    return message;
  },
};

const baseABCIParams: object = {
  voteExtensionsEnableHeight: 0,
  recheckTx: false,
};

export const ABCIParams = {
  encode(message: ABCIParams, writer: Writer = Writer.create()): Writer {
    if (message.voteExtensionsEnableHeight !== 0) {
      writer.uint32(8).int64(message.voteExtensionsEnableHeight);
    }
    if (message.recheckTx === true) {
      writer.uint32(16).bool(message.recheckTx);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ABCIParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseABCIParams } as ABCIParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.voteExtensionsEnableHeight = longToNumber(
            reader.int64() as Long
          );
          break;
        case 2:
          message.recheckTx = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ABCIParams {
    const message = { ...baseABCIParams } as ABCIParams;
    if (
      object.voteExtensionsEnableHeight !== undefined &&
      object.voteExtensionsEnableHeight !== null
    ) {
      message.voteExtensionsEnableHeight = Number(
        object.voteExtensionsEnableHeight
      );
    } else {
      message.voteExtensionsEnableHeight = 0;
    }
    if (object.recheckTx !== undefined && object.recheckTx !== null) {
      message.recheckTx = Boolean(object.recheckTx);
    } else {
      message.recheckTx = false;
    }
    return message;
  },

  toJSON(message: ABCIParams): unknown {
    const obj: any = {};
    message.voteExtensionsEnableHeight !== undefined &&
      (obj.voteExtensionsEnableHeight = message.voteExtensionsEnableHeight);
    message.recheckTx !== undefined && (obj.recheckTx = message.recheckTx);
    return obj;
  },

  fromPartial(object: DeepPartial<ABCIParams>): ABCIParams {
    const message = { ...baseABCIParams } as ABCIParams;
    if (
      object.voteExtensionsEnableHeight !== undefined &&
      object.voteExtensionsEnableHeight !== null
    ) {
      message.voteExtensionsEnableHeight = object.voteExtensionsEnableHeight;
    } else {
      message.voteExtensionsEnableHeight = 0;
    }
    if (object.recheckTx !== undefined && object.recheckTx !== null) {
      message.recheckTx = object.recheckTx;
    } else {
      message.recheckTx = false;
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
