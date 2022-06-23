/* eslint-disable */
import { Timestamp } from "../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Proof } from "../../tendermint/crypto/proof";
import { Consensus } from "../../tendermint/version/types";
import { ValidatorSet } from "../../tendermint/types/validator";

export const protobufPackage = "tendermint.types";

/** BlockIdFlag indicates which BlcokID the signature is for */
export enum BlockIDFlag {
  BLOCK_ID_FLAG_UNKNOWN = 0,
  BLOCK_ID_FLAG_ABSENT = 1,
  BLOCK_ID_FLAG_COMMIT = 2,
  BLOCK_ID_FLAG_NIL = 3,
  UNRECOGNIZED = -1,
}

export function blockIDFlagFromJSON(object: any): BlockIDFlag {
  switch (object) {
    case 0:
    case "BLOCK_ID_FLAG_UNKNOWN":
      return BlockIDFlag.BLOCK_ID_FLAG_UNKNOWN;
    case 1:
    case "BLOCK_ID_FLAG_ABSENT":
      return BlockIDFlag.BLOCK_ID_FLAG_ABSENT;
    case 2:
    case "BLOCK_ID_FLAG_COMMIT":
      return BlockIDFlag.BLOCK_ID_FLAG_COMMIT;
    case 3:
    case "BLOCK_ID_FLAG_NIL":
      return BlockIDFlag.BLOCK_ID_FLAG_NIL;
    case -1:
    case "UNRECOGNIZED":
    default:
      return BlockIDFlag.UNRECOGNIZED;
  }
}

export function blockIDFlagToJSON(object: BlockIDFlag): string {
  switch (object) {
    case BlockIDFlag.BLOCK_ID_FLAG_UNKNOWN:
      return "BLOCK_ID_FLAG_UNKNOWN";
    case BlockIDFlag.BLOCK_ID_FLAG_ABSENT:
      return "BLOCK_ID_FLAG_ABSENT";
    case BlockIDFlag.BLOCK_ID_FLAG_COMMIT:
      return "BLOCK_ID_FLAG_COMMIT";
    case BlockIDFlag.BLOCK_ID_FLAG_NIL:
      return "BLOCK_ID_FLAG_NIL";
    default:
      return "UNKNOWN";
  }
}

/** SignedMsgType is a type of signed message in the consensus. */
export enum SignedMsgType {
  SIGNED_MSG_TYPE_UNKNOWN = 0,
  /** SIGNED_MSG_TYPE_PREVOTE - Votes */
  SIGNED_MSG_TYPE_PREVOTE = 1,
  SIGNED_MSG_TYPE_PRECOMMIT = 2,
  /** SIGNED_MSG_TYPE_PROPOSAL - Proposals */
  SIGNED_MSG_TYPE_PROPOSAL = 32,
  UNRECOGNIZED = -1,
}

export function signedMsgTypeFromJSON(object: any): SignedMsgType {
  switch (object) {
    case 0:
    case "SIGNED_MSG_TYPE_UNKNOWN":
      return SignedMsgType.SIGNED_MSG_TYPE_UNKNOWN;
    case 1:
    case "SIGNED_MSG_TYPE_PREVOTE":
      return SignedMsgType.SIGNED_MSG_TYPE_PREVOTE;
    case 2:
    case "SIGNED_MSG_TYPE_PRECOMMIT":
      return SignedMsgType.SIGNED_MSG_TYPE_PRECOMMIT;
    case 32:
    case "SIGNED_MSG_TYPE_PROPOSAL":
      return SignedMsgType.SIGNED_MSG_TYPE_PROPOSAL;
    case -1:
    case "UNRECOGNIZED":
    default:
      return SignedMsgType.UNRECOGNIZED;
  }
}

export function signedMsgTypeToJSON(object: SignedMsgType): string {
  switch (object) {
    case SignedMsgType.SIGNED_MSG_TYPE_UNKNOWN:
      return "SIGNED_MSG_TYPE_UNKNOWN";
    case SignedMsgType.SIGNED_MSG_TYPE_PREVOTE:
      return "SIGNED_MSG_TYPE_PREVOTE";
    case SignedMsgType.SIGNED_MSG_TYPE_PRECOMMIT:
      return "SIGNED_MSG_TYPE_PRECOMMIT";
    case SignedMsgType.SIGNED_MSG_TYPE_PROPOSAL:
      return "SIGNED_MSG_TYPE_PROPOSAL";
    default:
      return "UNKNOWN";
  }
}

/** PartsetHeader */
export interface PartSetHeader {
  total: number;
  hash: Uint8Array;
}

export interface Part {
  index: number;
  bytes: Uint8Array;
  proof: Proof | undefined;
}

/** BlockID */
export interface BlockID {
  hash: Uint8Array;
  partSetHeader: PartSetHeader | undefined;
}

/** Header defines the structure of a Tendermint block header. */
export interface Header {
  /** basic block info */
  version: Consensus | undefined;
  chainId: string;
  height: number;
  time: Date | undefined;
  /** prev block info */
  lastBlockId: BlockID | undefined;
  /** hashes of block data */
  lastCommitHash: Uint8Array;
  /** transactions */
  dataHash: Uint8Array;
  /** hashes from the app output from the prev block */
  validatorsHash: Uint8Array;
  /** validators for the next block */
  nextValidatorsHash: Uint8Array;
  /** consensus params for current block */
  consensusHash: Uint8Array;
  /** state after txs from the previous block */
  appHash: Uint8Array;
  /** root hash of all results from the txs from the previous block */
  lastResultsHash: Uint8Array;
  /** consensus info */
  evidenceHash: Uint8Array;
  /** original proposer of the block */
  proposerAddress: Uint8Array;
}

/** Data contains the set of transactions included in the block */
export interface Data {
  /**
   * Txs that will be applied by state @ block.Height+1.
   * NOTE: not all txs here are valid.  We're just agreeing on the order first.
   * This means that block.AppHash does not include these txs.
   */
  txs: Uint8Array[];
}

/**
 * Vote represents a prevote, precommit, or commit vote from validators for
 * consensus.
 */
export interface Vote {
  type: SignedMsgType;
  height: number;
  round: number;
  /** zero if vote is nil. */
  blockId: BlockID | undefined;
  timestamp: Date | undefined;
  validatorAddress: Uint8Array;
  validatorIndex: number;
  signature: Uint8Array;
}

/** Commit contains the evidence that a block was committed by a set of validators. */
export interface Commit {
  height: number;
  round: number;
  blockId: BlockID | undefined;
  signatures: CommitSig[];
}

/** CommitSig is a part of the Vote included in a Commit. */
export interface CommitSig {
  blockIdFlag: BlockIDFlag;
  validatorAddress: Uint8Array;
  timestamp: Date | undefined;
  signature: Uint8Array;
}

export interface Proposal {
  type: SignedMsgType;
  height: number;
  round: number;
  polRound: number;
  blockId: BlockID | undefined;
  timestamp: Date | undefined;
  signature: Uint8Array;
}

export interface SignedHeader {
  header: Header | undefined;
  commit: Commit | undefined;
}

export interface LightBlock {
  signedHeader: SignedHeader | undefined;
  validatorSet: ValidatorSet | undefined;
}

export interface BlockMeta {
  blockId: BlockID | undefined;
  blockSize: number;
  header: Header | undefined;
  numTxs: number;
}

/** TxProof represents a Merkle proof of the presence of a transaction in the Merkle tree. */
export interface TxProof {
  rootHash: Uint8Array;
  data: Uint8Array;
  proof: Proof | undefined;
}

const basePartSetHeader: object = { total: 0 };

export const PartSetHeader = {
  encode(message: PartSetHeader, writer: Writer = Writer.create()): Writer {
    if (message.total !== 0) {
      writer.uint32(8).uint32(message.total);
    }
    if (message.hash.length !== 0) {
      writer.uint32(18).bytes(message.hash);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): PartSetHeader {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePartSetHeader } as PartSetHeader;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.total = reader.uint32();
          break;
        case 2:
          message.hash = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): PartSetHeader {
    const message = { ...basePartSetHeader } as PartSetHeader;
    if (object.total !== undefined && object.total !== null) {
      message.total = Number(object.total);
    } else {
      message.total = 0;
    }
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = bytesFromBase64(object.hash);
    }
    return message;
  },

  toJSON(message: PartSetHeader): unknown {
    const obj: any = {};
    message.total !== undefined && (obj.total = message.total);
    message.hash !== undefined &&
      (obj.hash = base64FromBytes(
        message.hash !== undefined ? message.hash : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<PartSetHeader>): PartSetHeader {
    const message = { ...basePartSetHeader } as PartSetHeader;
    if (object.total !== undefined && object.total !== null) {
      message.total = object.total;
    } else {
      message.total = 0;
    }
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = new Uint8Array();
    }
    return message;
  },
};

const basePart: object = { index: 0 };

export const Part = {
  encode(message: Part, writer: Writer = Writer.create()): Writer {
    if (message.index !== 0) {
      writer.uint32(8).uint32(message.index);
    }
    if (message.bytes.length !== 0) {
      writer.uint32(18).bytes(message.bytes);
    }
    if (message.proof !== undefined) {
      Proof.encode(message.proof, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Part {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...basePart } as Part;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.index = reader.uint32();
          break;
        case 2:
          message.bytes = reader.bytes();
          break;
        case 3:
          message.proof = Proof.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Part {
    const message = { ...basePart } as Part;
    if (object.index !== undefined && object.index !== null) {
      message.index = Number(object.index);
    } else {
      message.index = 0;
    }
    if (object.bytes !== undefined && object.bytes !== null) {
      message.bytes = bytesFromBase64(object.bytes);
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = Proof.fromJSON(object.proof);
    } else {
      message.proof = undefined;
    }
    return message;
  },

  toJSON(message: Part): unknown {
    const obj: any = {};
    message.index !== undefined && (obj.index = message.index);
    message.bytes !== undefined &&
      (obj.bytes = base64FromBytes(
        message.bytes !== undefined ? message.bytes : new Uint8Array()
      ));
    message.proof !== undefined &&
      (obj.proof = message.proof ? Proof.toJSON(message.proof) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<Part>): Part {
    const message = { ...basePart } as Part;
    if (object.index !== undefined && object.index !== null) {
      message.index = object.index;
    } else {
      message.index = 0;
    }
    if (object.bytes !== undefined && object.bytes !== null) {
      message.bytes = object.bytes;
    } else {
      message.bytes = new Uint8Array();
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = Proof.fromPartial(object.proof);
    } else {
      message.proof = undefined;
    }
    return message;
  },
};

const baseBlockID: object = {};

export const BlockID = {
  encode(message: BlockID, writer: Writer = Writer.create()): Writer {
    if (message.hash.length !== 0) {
      writer.uint32(10).bytes(message.hash);
    }
    if (message.partSetHeader !== undefined) {
      PartSetHeader.encode(
        message.partSetHeader,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): BlockID {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseBlockID } as BlockID;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.hash = reader.bytes();
          break;
        case 2:
          message.partSetHeader = PartSetHeader.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): BlockID {
    const message = { ...baseBlockID } as BlockID;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = bytesFromBase64(object.hash);
    }
    if (object.partSetHeader !== undefined && object.partSetHeader !== null) {
      message.partSetHeader = PartSetHeader.fromJSON(object.partSetHeader);
    } else {
      message.partSetHeader = undefined;
    }
    return message;
  },

  toJSON(message: BlockID): unknown {
    const obj: any = {};
    message.hash !== undefined &&
      (obj.hash = base64FromBytes(
        message.hash !== undefined ? message.hash : new Uint8Array()
      ));
    message.partSetHeader !== undefined &&
      (obj.partSetHeader = message.partSetHeader
        ? PartSetHeader.toJSON(message.partSetHeader)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<BlockID>): BlockID {
    const message = { ...baseBlockID } as BlockID;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = new Uint8Array();
    }
    if (object.partSetHeader !== undefined && object.partSetHeader !== null) {
      message.partSetHeader = PartSetHeader.fromPartial(object.partSetHeader);
    } else {
      message.partSetHeader = undefined;
    }
    return message;
  },
};

const baseHeader: object = { chainId: "", height: 0 };

export const Header = {
  encode(message: Header, writer: Writer = Writer.create()): Writer {
    if (message.version !== undefined) {
      Consensus.encode(message.version, writer.uint32(10).fork()).ldelim();
    }
    if (message.chainId !== "") {
      writer.uint32(18).string(message.chainId);
    }
    if (message.height !== 0) {
      writer.uint32(24).int64(message.height);
    }
    if (message.time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.time),
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.lastBlockId !== undefined) {
      BlockID.encode(message.lastBlockId, writer.uint32(42).fork()).ldelim();
    }
    if (message.lastCommitHash.length !== 0) {
      writer.uint32(50).bytes(message.lastCommitHash);
    }
    if (message.dataHash.length !== 0) {
      writer.uint32(58).bytes(message.dataHash);
    }
    if (message.validatorsHash.length !== 0) {
      writer.uint32(66).bytes(message.validatorsHash);
    }
    if (message.nextValidatorsHash.length !== 0) {
      writer.uint32(74).bytes(message.nextValidatorsHash);
    }
    if (message.consensusHash.length !== 0) {
      writer.uint32(82).bytes(message.consensusHash);
    }
    if (message.appHash.length !== 0) {
      writer.uint32(90).bytes(message.appHash);
    }
    if (message.lastResultsHash.length !== 0) {
      writer.uint32(98).bytes(message.lastResultsHash);
    }
    if (message.evidenceHash.length !== 0) {
      writer.uint32(106).bytes(message.evidenceHash);
    }
    if (message.proposerAddress.length !== 0) {
      writer.uint32(114).bytes(message.proposerAddress);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Header {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseHeader } as Header;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.version = Consensus.decode(reader, reader.uint32());
          break;
        case 2:
          message.chainId = reader.string();
          break;
        case 3:
          message.height = longToNumber(reader.int64() as Long);
          break;
        case 4:
          message.time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.lastBlockId = BlockID.decode(reader, reader.uint32());
          break;
        case 6:
          message.lastCommitHash = reader.bytes();
          break;
        case 7:
          message.dataHash = reader.bytes();
          break;
        case 8:
          message.validatorsHash = reader.bytes();
          break;
        case 9:
          message.nextValidatorsHash = reader.bytes();
          break;
        case 10:
          message.consensusHash = reader.bytes();
          break;
        case 11:
          message.appHash = reader.bytes();
          break;
        case 12:
          message.lastResultsHash = reader.bytes();
          break;
        case 13:
          message.evidenceHash = reader.bytes();
          break;
        case 14:
          message.proposerAddress = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Header {
    const message = { ...baseHeader } as Header;
    if (object.version !== undefined && object.version !== null) {
      message.version = Consensus.fromJSON(object.version);
    } else {
      message.version = undefined;
    }
    if (object.chainId !== undefined && object.chainId !== null) {
      message.chainId = String(object.chainId);
    } else {
      message.chainId = "";
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.time !== undefined && object.time !== null) {
      message.time = fromJsonTimestamp(object.time);
    } else {
      message.time = undefined;
    }
    if (object.lastBlockId !== undefined && object.lastBlockId !== null) {
      message.lastBlockId = BlockID.fromJSON(object.lastBlockId);
    } else {
      message.lastBlockId = undefined;
    }
    if (object.lastCommitHash !== undefined && object.lastCommitHash !== null) {
      message.lastCommitHash = bytesFromBase64(object.lastCommitHash);
    }
    if (object.dataHash !== undefined && object.dataHash !== null) {
      message.dataHash = bytesFromBase64(object.dataHash);
    }
    if (object.validatorsHash !== undefined && object.validatorsHash !== null) {
      message.validatorsHash = bytesFromBase64(object.validatorsHash);
    }
    if (
      object.nextValidatorsHash !== undefined &&
      object.nextValidatorsHash !== null
    ) {
      message.nextValidatorsHash = bytesFromBase64(object.nextValidatorsHash);
    }
    if (object.consensusHash !== undefined && object.consensusHash !== null) {
      message.consensusHash = bytesFromBase64(object.consensusHash);
    }
    if (object.appHash !== undefined && object.appHash !== null) {
      message.appHash = bytesFromBase64(object.appHash);
    }
    if (
      object.lastResultsHash !== undefined &&
      object.lastResultsHash !== null
    ) {
      message.lastResultsHash = bytesFromBase64(object.lastResultsHash);
    }
    if (object.evidenceHash !== undefined && object.evidenceHash !== null) {
      message.evidenceHash = bytesFromBase64(object.evidenceHash);
    }
    if (
      object.proposerAddress !== undefined &&
      object.proposerAddress !== null
    ) {
      message.proposerAddress = bytesFromBase64(object.proposerAddress);
    }
    return message;
  },

  toJSON(message: Header): unknown {
    const obj: any = {};
    message.version !== undefined &&
      (obj.version = message.version
        ? Consensus.toJSON(message.version)
        : undefined);
    message.chainId !== undefined && (obj.chainId = message.chainId);
    message.height !== undefined && (obj.height = message.height);
    message.time !== undefined &&
      (obj.time =
        message.time !== undefined ? message.time.toISOString() : null);
    message.lastBlockId !== undefined &&
      (obj.lastBlockId = message.lastBlockId
        ? BlockID.toJSON(message.lastBlockId)
        : undefined);
    message.lastCommitHash !== undefined &&
      (obj.lastCommitHash = base64FromBytes(
        message.lastCommitHash !== undefined
          ? message.lastCommitHash
          : new Uint8Array()
      ));
    message.dataHash !== undefined &&
      (obj.dataHash = base64FromBytes(
        message.dataHash !== undefined ? message.dataHash : new Uint8Array()
      ));
    message.validatorsHash !== undefined &&
      (obj.validatorsHash = base64FromBytes(
        message.validatorsHash !== undefined
          ? message.validatorsHash
          : new Uint8Array()
      ));
    message.nextValidatorsHash !== undefined &&
      (obj.nextValidatorsHash = base64FromBytes(
        message.nextValidatorsHash !== undefined
          ? message.nextValidatorsHash
          : new Uint8Array()
      ));
    message.consensusHash !== undefined &&
      (obj.consensusHash = base64FromBytes(
        message.consensusHash !== undefined
          ? message.consensusHash
          : new Uint8Array()
      ));
    message.appHash !== undefined &&
      (obj.appHash = base64FromBytes(
        message.appHash !== undefined ? message.appHash : new Uint8Array()
      ));
    message.lastResultsHash !== undefined &&
      (obj.lastResultsHash = base64FromBytes(
        message.lastResultsHash !== undefined
          ? message.lastResultsHash
          : new Uint8Array()
      ));
    message.evidenceHash !== undefined &&
      (obj.evidenceHash = base64FromBytes(
        message.evidenceHash !== undefined
          ? message.evidenceHash
          : new Uint8Array()
      ));
    message.proposerAddress !== undefined &&
      (obj.proposerAddress = base64FromBytes(
        message.proposerAddress !== undefined
          ? message.proposerAddress
          : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Header>): Header {
    const message = { ...baseHeader } as Header;
    if (object.version !== undefined && object.version !== null) {
      message.version = Consensus.fromPartial(object.version);
    } else {
      message.version = undefined;
    }
    if (object.chainId !== undefined && object.chainId !== null) {
      message.chainId = object.chainId;
    } else {
      message.chainId = "";
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.time !== undefined && object.time !== null) {
      message.time = object.time;
    } else {
      message.time = undefined;
    }
    if (object.lastBlockId !== undefined && object.lastBlockId !== null) {
      message.lastBlockId = BlockID.fromPartial(object.lastBlockId);
    } else {
      message.lastBlockId = undefined;
    }
    if (object.lastCommitHash !== undefined && object.lastCommitHash !== null) {
      message.lastCommitHash = object.lastCommitHash;
    } else {
      message.lastCommitHash = new Uint8Array();
    }
    if (object.dataHash !== undefined && object.dataHash !== null) {
      message.dataHash = object.dataHash;
    } else {
      message.dataHash = new Uint8Array();
    }
    if (object.validatorsHash !== undefined && object.validatorsHash !== null) {
      message.validatorsHash = object.validatorsHash;
    } else {
      message.validatorsHash = new Uint8Array();
    }
    if (
      object.nextValidatorsHash !== undefined &&
      object.nextValidatorsHash !== null
    ) {
      message.nextValidatorsHash = object.nextValidatorsHash;
    } else {
      message.nextValidatorsHash = new Uint8Array();
    }
    if (object.consensusHash !== undefined && object.consensusHash !== null) {
      message.consensusHash = object.consensusHash;
    } else {
      message.consensusHash = new Uint8Array();
    }
    if (object.appHash !== undefined && object.appHash !== null) {
      message.appHash = object.appHash;
    } else {
      message.appHash = new Uint8Array();
    }
    if (
      object.lastResultsHash !== undefined &&
      object.lastResultsHash !== null
    ) {
      message.lastResultsHash = object.lastResultsHash;
    } else {
      message.lastResultsHash = new Uint8Array();
    }
    if (object.evidenceHash !== undefined && object.evidenceHash !== null) {
      message.evidenceHash = object.evidenceHash;
    } else {
      message.evidenceHash = new Uint8Array();
    }
    if (
      object.proposerAddress !== undefined &&
      object.proposerAddress !== null
    ) {
      message.proposerAddress = object.proposerAddress;
    } else {
      message.proposerAddress = new Uint8Array();
    }
    return message;
  },
};

const baseData: object = {};

export const Data = {
  encode(message: Data, writer: Writer = Writer.create()): Writer {
    for (const v of message.txs) {
      writer.uint32(10).bytes(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Data {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseData } as Data;
    message.txs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.txs.push(reader.bytes());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Data {
    const message = { ...baseData } as Data;
    message.txs = [];
    if (object.txs !== undefined && object.txs !== null) {
      for (const e of object.txs) {
        message.txs.push(bytesFromBase64(e));
      }
    }
    return message;
  },

  toJSON(message: Data): unknown {
    const obj: any = {};
    if (message.txs) {
      obj.txs = message.txs.map((e) =>
        base64FromBytes(e !== undefined ? e : new Uint8Array())
      );
    } else {
      obj.txs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Data>): Data {
    const message = { ...baseData } as Data;
    message.txs = [];
    if (object.txs !== undefined && object.txs !== null) {
      for (const e of object.txs) {
        message.txs.push(e);
      }
    }
    return message;
  },
};

const baseVote: object = { type: 0, height: 0, round: 0, validatorIndex: 0 };

export const Vote = {
  encode(message: Vote, writer: Writer = Writer.create()): Writer {
    if (message.type !== 0) {
      writer.uint32(8).int32(message.type);
    }
    if (message.height !== 0) {
      writer.uint32(16).int64(message.height);
    }
    if (message.round !== 0) {
      writer.uint32(24).int32(message.round);
    }
    if (message.blockId !== undefined) {
      BlockID.encode(message.blockId, writer.uint32(34).fork()).ldelim();
    }
    if (message.timestamp !== undefined) {
      Timestamp.encode(
        toTimestamp(message.timestamp),
        writer.uint32(42).fork()
      ).ldelim();
    }
    if (message.validatorAddress.length !== 0) {
      writer.uint32(50).bytes(message.validatorAddress);
    }
    if (message.validatorIndex !== 0) {
      writer.uint32(56).int32(message.validatorIndex);
    }
    if (message.signature.length !== 0) {
      writer.uint32(66).bytes(message.signature);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Vote {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseVote } as Vote;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.type = reader.int32() as any;
          break;
        case 2:
          message.height = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.round = reader.int32();
          break;
        case 4:
          message.blockId = BlockID.decode(reader, reader.uint32());
          break;
        case 5:
          message.timestamp = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.validatorAddress = reader.bytes();
          break;
        case 7:
          message.validatorIndex = reader.int32();
          break;
        case 8:
          message.signature = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Vote {
    const message = { ...baseVote } as Vote;
    if (object.type !== undefined && object.type !== null) {
      message.type = signedMsgTypeFromJSON(object.type);
    } else {
      message.type = 0;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.round !== undefined && object.round !== null) {
      message.round = Number(object.round);
    } else {
      message.round = 0;
    }
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromJSON(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = fromJsonTimestamp(object.timestamp);
    } else {
      message.timestamp = undefined;
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = bytesFromBase64(object.validatorAddress);
    }
    if (object.validatorIndex !== undefined && object.validatorIndex !== null) {
      message.validatorIndex = Number(object.validatorIndex);
    } else {
      message.validatorIndex = 0;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = bytesFromBase64(object.signature);
    }
    return message;
  },

  toJSON(message: Vote): unknown {
    const obj: any = {};
    message.type !== undefined &&
      (obj.type = signedMsgTypeToJSON(message.type));
    message.height !== undefined && (obj.height = message.height);
    message.round !== undefined && (obj.round = message.round);
    message.blockId !== undefined &&
      (obj.blockId = message.blockId
        ? BlockID.toJSON(message.blockId)
        : undefined);
    message.timestamp !== undefined &&
      (obj.timestamp =
        message.timestamp !== undefined
          ? message.timestamp.toISOString()
          : null);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = base64FromBytes(
        message.validatorAddress !== undefined
          ? message.validatorAddress
          : new Uint8Array()
      ));
    message.validatorIndex !== undefined &&
      (obj.validatorIndex = message.validatorIndex);
    message.signature !== undefined &&
      (obj.signature = base64FromBytes(
        message.signature !== undefined ? message.signature : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Vote>): Vote {
    const message = { ...baseVote } as Vote;
    if (object.type !== undefined && object.type !== null) {
      message.type = object.type;
    } else {
      message.type = 0;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.round !== undefined && object.round !== null) {
      message.round = object.round;
    } else {
      message.round = 0;
    }
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromPartial(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = undefined;
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = new Uint8Array();
    }
    if (object.validatorIndex !== undefined && object.validatorIndex !== null) {
      message.validatorIndex = object.validatorIndex;
    } else {
      message.validatorIndex = 0;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = object.signature;
    } else {
      message.signature = new Uint8Array();
    }
    return message;
  },
};

const baseCommit: object = { height: 0, round: 0 };

export const Commit = {
  encode(message: Commit, writer: Writer = Writer.create()): Writer {
    if (message.height !== 0) {
      writer.uint32(8).int64(message.height);
    }
    if (message.round !== 0) {
      writer.uint32(16).int32(message.round);
    }
    if (message.blockId !== undefined) {
      BlockID.encode(message.blockId, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.signatures) {
      CommitSig.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Commit {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCommit } as Commit;
    message.signatures = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.height = longToNumber(reader.int64() as Long);
          break;
        case 2:
          message.round = reader.int32();
          break;
        case 3:
          message.blockId = BlockID.decode(reader, reader.uint32());
          break;
        case 4:
          message.signatures.push(CommitSig.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Commit {
    const message = { ...baseCommit } as Commit;
    message.signatures = [];
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.round !== undefined && object.round !== null) {
      message.round = Number(object.round);
    } else {
      message.round = 0;
    }
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromJSON(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(CommitSig.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Commit): unknown {
    const obj: any = {};
    message.height !== undefined && (obj.height = message.height);
    message.round !== undefined && (obj.round = message.round);
    message.blockId !== undefined &&
      (obj.blockId = message.blockId
        ? BlockID.toJSON(message.blockId)
        : undefined);
    if (message.signatures) {
      obj.signatures = message.signatures.map((e) =>
        e ? CommitSig.toJSON(e) : undefined
      );
    } else {
      obj.signatures = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Commit>): Commit {
    const message = { ...baseCommit } as Commit;
    message.signatures = [];
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.round !== undefined && object.round !== null) {
      message.round = object.round;
    } else {
      message.round = 0;
    }
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromPartial(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(CommitSig.fromPartial(e));
      }
    }
    return message;
  },
};

const baseCommitSig: object = { blockIdFlag: 0 };

export const CommitSig = {
  encode(message: CommitSig, writer: Writer = Writer.create()): Writer {
    if (message.blockIdFlag !== 0) {
      writer.uint32(8).int32(message.blockIdFlag);
    }
    if (message.validatorAddress.length !== 0) {
      writer.uint32(18).bytes(message.validatorAddress);
    }
    if (message.timestamp !== undefined) {
      Timestamp.encode(
        toTimestamp(message.timestamp),
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.signature.length !== 0) {
      writer.uint32(34).bytes(message.signature);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): CommitSig {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCommitSig } as CommitSig;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.blockIdFlag = reader.int32() as any;
          break;
        case 2:
          message.validatorAddress = reader.bytes();
          break;
        case 3:
          message.timestamp = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.signature = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): CommitSig {
    const message = { ...baseCommitSig } as CommitSig;
    if (object.blockIdFlag !== undefined && object.blockIdFlag !== null) {
      message.blockIdFlag = blockIDFlagFromJSON(object.blockIdFlag);
    } else {
      message.blockIdFlag = 0;
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = bytesFromBase64(object.validatorAddress);
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = fromJsonTimestamp(object.timestamp);
    } else {
      message.timestamp = undefined;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = bytesFromBase64(object.signature);
    }
    return message;
  },

  toJSON(message: CommitSig): unknown {
    const obj: any = {};
    message.blockIdFlag !== undefined &&
      (obj.blockIdFlag = blockIDFlagToJSON(message.blockIdFlag));
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = base64FromBytes(
        message.validatorAddress !== undefined
          ? message.validatorAddress
          : new Uint8Array()
      ));
    message.timestamp !== undefined &&
      (obj.timestamp =
        message.timestamp !== undefined
          ? message.timestamp.toISOString()
          : null);
    message.signature !== undefined &&
      (obj.signature = base64FromBytes(
        message.signature !== undefined ? message.signature : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<CommitSig>): CommitSig {
    const message = { ...baseCommitSig } as CommitSig;
    if (object.blockIdFlag !== undefined && object.blockIdFlag !== null) {
      message.blockIdFlag = object.blockIdFlag;
    } else {
      message.blockIdFlag = 0;
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = new Uint8Array();
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = undefined;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = object.signature;
    } else {
      message.signature = new Uint8Array();
    }
    return message;
  },
};

const baseProposal: object = { type: 0, height: 0, round: 0, polRound: 0 };

export const Proposal = {
  encode(message: Proposal, writer: Writer = Writer.create()): Writer {
    if (message.type !== 0) {
      writer.uint32(8).int32(message.type);
    }
    if (message.height !== 0) {
      writer.uint32(16).int64(message.height);
    }
    if (message.round !== 0) {
      writer.uint32(24).int32(message.round);
    }
    if (message.polRound !== 0) {
      writer.uint32(32).int32(message.polRound);
    }
    if (message.blockId !== undefined) {
      BlockID.encode(message.blockId, writer.uint32(42).fork()).ldelim();
    }
    if (message.timestamp !== undefined) {
      Timestamp.encode(
        toTimestamp(message.timestamp),
        writer.uint32(50).fork()
      ).ldelim();
    }
    if (message.signature.length !== 0) {
      writer.uint32(58).bytes(message.signature);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Proposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseProposal } as Proposal;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.type = reader.int32() as any;
          break;
        case 2:
          message.height = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.round = reader.int32();
          break;
        case 4:
          message.polRound = reader.int32();
          break;
        case 5:
          message.blockId = BlockID.decode(reader, reader.uint32());
          break;
        case 6:
          message.timestamp = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.signature = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Proposal {
    const message = { ...baseProposal } as Proposal;
    if (object.type !== undefined && object.type !== null) {
      message.type = signedMsgTypeFromJSON(object.type);
    } else {
      message.type = 0;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.round !== undefined && object.round !== null) {
      message.round = Number(object.round);
    } else {
      message.round = 0;
    }
    if (object.polRound !== undefined && object.polRound !== null) {
      message.polRound = Number(object.polRound);
    } else {
      message.polRound = 0;
    }
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromJSON(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = fromJsonTimestamp(object.timestamp);
    } else {
      message.timestamp = undefined;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = bytesFromBase64(object.signature);
    }
    return message;
  },

  toJSON(message: Proposal): unknown {
    const obj: any = {};
    message.type !== undefined &&
      (obj.type = signedMsgTypeToJSON(message.type));
    message.height !== undefined && (obj.height = message.height);
    message.round !== undefined && (obj.round = message.round);
    message.polRound !== undefined && (obj.polRound = message.polRound);
    message.blockId !== undefined &&
      (obj.blockId = message.blockId
        ? BlockID.toJSON(message.blockId)
        : undefined);
    message.timestamp !== undefined &&
      (obj.timestamp =
        message.timestamp !== undefined
          ? message.timestamp.toISOString()
          : null);
    message.signature !== undefined &&
      (obj.signature = base64FromBytes(
        message.signature !== undefined ? message.signature : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Proposal>): Proposal {
    const message = { ...baseProposal } as Proposal;
    if (object.type !== undefined && object.type !== null) {
      message.type = object.type;
    } else {
      message.type = 0;
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.round !== undefined && object.round !== null) {
      message.round = object.round;
    } else {
      message.round = 0;
    }
    if (object.polRound !== undefined && object.polRound !== null) {
      message.polRound = object.polRound;
    } else {
      message.polRound = 0;
    }
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromPartial(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.timestamp !== undefined && object.timestamp !== null) {
      message.timestamp = object.timestamp;
    } else {
      message.timestamp = undefined;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = object.signature;
    } else {
      message.signature = new Uint8Array();
    }
    return message;
  },
};

const baseSignedHeader: object = {};

export const SignedHeader = {
  encode(message: SignedHeader, writer: Writer = Writer.create()): Writer {
    if (message.header !== undefined) {
      Header.encode(message.header, writer.uint32(10).fork()).ldelim();
    }
    if (message.commit !== undefined) {
      Commit.encode(message.commit, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SignedHeader {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSignedHeader } as SignedHeader;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.header = Header.decode(reader, reader.uint32());
          break;
        case 2:
          message.commit = Commit.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SignedHeader {
    const message = { ...baseSignedHeader } as SignedHeader;
    if (object.header !== undefined && object.header !== null) {
      message.header = Header.fromJSON(object.header);
    } else {
      message.header = undefined;
    }
    if (object.commit !== undefined && object.commit !== null) {
      message.commit = Commit.fromJSON(object.commit);
    } else {
      message.commit = undefined;
    }
    return message;
  },

  toJSON(message: SignedHeader): unknown {
    const obj: any = {};
    message.header !== undefined &&
      (obj.header = message.header ? Header.toJSON(message.header) : undefined);
    message.commit !== undefined &&
      (obj.commit = message.commit ? Commit.toJSON(message.commit) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<SignedHeader>): SignedHeader {
    const message = { ...baseSignedHeader } as SignedHeader;
    if (object.header !== undefined && object.header !== null) {
      message.header = Header.fromPartial(object.header);
    } else {
      message.header = undefined;
    }
    if (object.commit !== undefined && object.commit !== null) {
      message.commit = Commit.fromPartial(object.commit);
    } else {
      message.commit = undefined;
    }
    return message;
  },
};

const baseLightBlock: object = {};

export const LightBlock = {
  encode(message: LightBlock, writer: Writer = Writer.create()): Writer {
    if (message.signedHeader !== undefined) {
      SignedHeader.encode(
        message.signedHeader,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.validatorSet !== undefined) {
      ValidatorSet.encode(
        message.validatorSet,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): LightBlock {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseLightBlock } as LightBlock;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.signedHeader = SignedHeader.decode(reader, reader.uint32());
          break;
        case 2:
          message.validatorSet = ValidatorSet.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): LightBlock {
    const message = { ...baseLightBlock } as LightBlock;
    if (object.signedHeader !== undefined && object.signedHeader !== null) {
      message.signedHeader = SignedHeader.fromJSON(object.signedHeader);
    } else {
      message.signedHeader = undefined;
    }
    if (object.validatorSet !== undefined && object.validatorSet !== null) {
      message.validatorSet = ValidatorSet.fromJSON(object.validatorSet);
    } else {
      message.validatorSet = undefined;
    }
    return message;
  },

  toJSON(message: LightBlock): unknown {
    const obj: any = {};
    message.signedHeader !== undefined &&
      (obj.signedHeader = message.signedHeader
        ? SignedHeader.toJSON(message.signedHeader)
        : undefined);
    message.validatorSet !== undefined &&
      (obj.validatorSet = message.validatorSet
        ? ValidatorSet.toJSON(message.validatorSet)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<LightBlock>): LightBlock {
    const message = { ...baseLightBlock } as LightBlock;
    if (object.signedHeader !== undefined && object.signedHeader !== null) {
      message.signedHeader = SignedHeader.fromPartial(object.signedHeader);
    } else {
      message.signedHeader = undefined;
    }
    if (object.validatorSet !== undefined && object.validatorSet !== null) {
      message.validatorSet = ValidatorSet.fromPartial(object.validatorSet);
    } else {
      message.validatorSet = undefined;
    }
    return message;
  },
};

const baseBlockMeta: object = { blockSize: 0, numTxs: 0 };

export const BlockMeta = {
  encode(message: BlockMeta, writer: Writer = Writer.create()): Writer {
    if (message.blockId !== undefined) {
      BlockID.encode(message.blockId, writer.uint32(10).fork()).ldelim();
    }
    if (message.blockSize !== 0) {
      writer.uint32(16).int64(message.blockSize);
    }
    if (message.header !== undefined) {
      Header.encode(message.header, writer.uint32(26).fork()).ldelim();
    }
    if (message.numTxs !== 0) {
      writer.uint32(32).int64(message.numTxs);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): BlockMeta {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseBlockMeta } as BlockMeta;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.blockId = BlockID.decode(reader, reader.uint32());
          break;
        case 2:
          message.blockSize = longToNumber(reader.int64() as Long);
          break;
        case 3:
          message.header = Header.decode(reader, reader.uint32());
          break;
        case 4:
          message.numTxs = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): BlockMeta {
    const message = { ...baseBlockMeta } as BlockMeta;
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromJSON(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.blockSize !== undefined && object.blockSize !== null) {
      message.blockSize = Number(object.blockSize);
    } else {
      message.blockSize = 0;
    }
    if (object.header !== undefined && object.header !== null) {
      message.header = Header.fromJSON(object.header);
    } else {
      message.header = undefined;
    }
    if (object.numTxs !== undefined && object.numTxs !== null) {
      message.numTxs = Number(object.numTxs);
    } else {
      message.numTxs = 0;
    }
    return message;
  },

  toJSON(message: BlockMeta): unknown {
    const obj: any = {};
    message.blockId !== undefined &&
      (obj.blockId = message.blockId
        ? BlockID.toJSON(message.blockId)
        : undefined);
    message.blockSize !== undefined && (obj.blockSize = message.blockSize);
    message.header !== undefined &&
      (obj.header = message.header ? Header.toJSON(message.header) : undefined);
    message.numTxs !== undefined && (obj.numTxs = message.numTxs);
    return obj;
  },

  fromPartial(object: DeepPartial<BlockMeta>): BlockMeta {
    const message = { ...baseBlockMeta } as BlockMeta;
    if (object.blockId !== undefined && object.blockId !== null) {
      message.blockId = BlockID.fromPartial(object.blockId);
    } else {
      message.blockId = undefined;
    }
    if (object.blockSize !== undefined && object.blockSize !== null) {
      message.blockSize = object.blockSize;
    } else {
      message.blockSize = 0;
    }
    if (object.header !== undefined && object.header !== null) {
      message.header = Header.fromPartial(object.header);
    } else {
      message.header = undefined;
    }
    if (object.numTxs !== undefined && object.numTxs !== null) {
      message.numTxs = object.numTxs;
    } else {
      message.numTxs = 0;
    }
    return message;
  },
};

const baseTxProof: object = {};

export const TxProof = {
  encode(message: TxProof, writer: Writer = Writer.create()): Writer {
    if (message.rootHash.length !== 0) {
      writer.uint32(10).bytes(message.rootHash);
    }
    if (message.data.length !== 0) {
      writer.uint32(18).bytes(message.data);
    }
    if (message.proof !== undefined) {
      Proof.encode(message.proof, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): TxProof {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTxProof } as TxProof;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rootHash = reader.bytes();
          break;
        case 2:
          message.data = reader.bytes();
          break;
        case 3:
          message.proof = Proof.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): TxProof {
    const message = { ...baseTxProof } as TxProof;
    if (object.rootHash !== undefined && object.rootHash !== null) {
      message.rootHash = bytesFromBase64(object.rootHash);
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = bytesFromBase64(object.data);
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = Proof.fromJSON(object.proof);
    } else {
      message.proof = undefined;
    }
    return message;
  },

  toJSON(message: TxProof): unknown {
    const obj: any = {};
    message.rootHash !== undefined &&
      (obj.rootHash = base64FromBytes(
        message.rootHash !== undefined ? message.rootHash : new Uint8Array()
      ));
    message.data !== undefined &&
      (obj.data = base64FromBytes(
        message.data !== undefined ? message.data : new Uint8Array()
      ));
    message.proof !== undefined &&
      (obj.proof = message.proof ? Proof.toJSON(message.proof) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<TxProof>): TxProof {
    const message = { ...baseTxProof } as TxProof;
    if (object.rootHash !== undefined && object.rootHash !== null) {
      message.rootHash = object.rootHash;
    } else {
      message.rootHash = new Uint8Array();
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = object.data;
    } else {
      message.data = new Uint8Array();
    }
    if (object.proof !== undefined && object.proof !== null) {
      message.proof = Proof.fromPartial(object.proof);
    } else {
      message.proof = undefined;
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
