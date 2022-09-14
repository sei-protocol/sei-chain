/* eslint-disable */
import { Timestamp } from "../../../google/protobuf/timestamp";
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Any } from "../../../google/protobuf/any";
import { Duration } from "../../../google/protobuf/duration";

export const protobufPackage = "cosmos.gov.v1beta1";

/** VoteOption enumerates the valid vote options for a given governance proposal. */
export enum VoteOption {
  /** VOTE_OPTION_UNSPECIFIED - VOTE_OPTION_UNSPECIFIED defines a no-op vote option. */
  VOTE_OPTION_UNSPECIFIED = 0,
  /** VOTE_OPTION_YES - VOTE_OPTION_YES defines a yes vote option. */
  VOTE_OPTION_YES = 1,
  /** VOTE_OPTION_ABSTAIN - VOTE_OPTION_ABSTAIN defines an abstain vote option. */
  VOTE_OPTION_ABSTAIN = 2,
  /** VOTE_OPTION_NO - VOTE_OPTION_NO defines a no vote option. */
  VOTE_OPTION_NO = 3,
  /** VOTE_OPTION_NO_WITH_VETO - VOTE_OPTION_NO_WITH_VETO defines a no with veto vote option. */
  VOTE_OPTION_NO_WITH_VETO = 4,
  UNRECOGNIZED = -1,
}

export function voteOptionFromJSON(object: any): VoteOption {
  switch (object) {
    case 0:
    case "VOTE_OPTION_UNSPECIFIED":
      return VoteOption.VOTE_OPTION_UNSPECIFIED;
    case 1:
    case "VOTE_OPTION_YES":
      return VoteOption.VOTE_OPTION_YES;
    case 2:
    case "VOTE_OPTION_ABSTAIN":
      return VoteOption.VOTE_OPTION_ABSTAIN;
    case 3:
    case "VOTE_OPTION_NO":
      return VoteOption.VOTE_OPTION_NO;
    case 4:
    case "VOTE_OPTION_NO_WITH_VETO":
      return VoteOption.VOTE_OPTION_NO_WITH_VETO;
    case -1:
    case "UNRECOGNIZED":
    default:
      return VoteOption.UNRECOGNIZED;
  }
}

export function voteOptionToJSON(object: VoteOption): string {
  switch (object) {
    case VoteOption.VOTE_OPTION_UNSPECIFIED:
      return "VOTE_OPTION_UNSPECIFIED";
    case VoteOption.VOTE_OPTION_YES:
      return "VOTE_OPTION_YES";
    case VoteOption.VOTE_OPTION_ABSTAIN:
      return "VOTE_OPTION_ABSTAIN";
    case VoteOption.VOTE_OPTION_NO:
      return "VOTE_OPTION_NO";
    case VoteOption.VOTE_OPTION_NO_WITH_VETO:
      return "VOTE_OPTION_NO_WITH_VETO";
    default:
      return "UNKNOWN";
  }
}

/** ProposalStatus enumerates the valid statuses of a proposal. */
export enum ProposalStatus {
  /** PROPOSAL_STATUS_UNSPECIFIED - PROPOSAL_STATUS_UNSPECIFIED defines the default propopsal status. */
  PROPOSAL_STATUS_UNSPECIFIED = 0,
  /**
   * PROPOSAL_STATUS_DEPOSIT_PERIOD - PROPOSAL_STATUS_DEPOSIT_PERIOD defines a proposal status during the deposit
   * period.
   */
  PROPOSAL_STATUS_DEPOSIT_PERIOD = 1,
  /**
   * PROPOSAL_STATUS_VOTING_PERIOD - PROPOSAL_STATUS_VOTING_PERIOD defines a proposal status during the voting
   * period.
   */
  PROPOSAL_STATUS_VOTING_PERIOD = 2,
  /**
   * PROPOSAL_STATUS_PASSED - PROPOSAL_STATUS_PASSED defines a proposal status of a proposal that has
   * passed.
   */
  PROPOSAL_STATUS_PASSED = 3,
  /**
   * PROPOSAL_STATUS_REJECTED - PROPOSAL_STATUS_REJECTED defines a proposal status of a proposal that has
   * been rejected.
   */
  PROPOSAL_STATUS_REJECTED = 4,
  /**
   * PROPOSAL_STATUS_FAILED - PROPOSAL_STATUS_FAILED defines a proposal status of a proposal that has
   * failed.
   */
  PROPOSAL_STATUS_FAILED = 5,
  UNRECOGNIZED = -1,
}

export function proposalStatusFromJSON(object: any): ProposalStatus {
  switch (object) {
    case 0:
    case "PROPOSAL_STATUS_UNSPECIFIED":
      return ProposalStatus.PROPOSAL_STATUS_UNSPECIFIED;
    case 1:
    case "PROPOSAL_STATUS_DEPOSIT_PERIOD":
      return ProposalStatus.PROPOSAL_STATUS_DEPOSIT_PERIOD;
    case 2:
    case "PROPOSAL_STATUS_VOTING_PERIOD":
      return ProposalStatus.PROPOSAL_STATUS_VOTING_PERIOD;
    case 3:
    case "PROPOSAL_STATUS_PASSED":
      return ProposalStatus.PROPOSAL_STATUS_PASSED;
    case 4:
    case "PROPOSAL_STATUS_REJECTED":
      return ProposalStatus.PROPOSAL_STATUS_REJECTED;
    case 5:
    case "PROPOSAL_STATUS_FAILED":
      return ProposalStatus.PROPOSAL_STATUS_FAILED;
    case -1:
    case "UNRECOGNIZED":
    default:
      return ProposalStatus.UNRECOGNIZED;
  }
}

export function proposalStatusToJSON(object: ProposalStatus): string {
  switch (object) {
    case ProposalStatus.PROPOSAL_STATUS_UNSPECIFIED:
      return "PROPOSAL_STATUS_UNSPECIFIED";
    case ProposalStatus.PROPOSAL_STATUS_DEPOSIT_PERIOD:
      return "PROPOSAL_STATUS_DEPOSIT_PERIOD";
    case ProposalStatus.PROPOSAL_STATUS_VOTING_PERIOD:
      return "PROPOSAL_STATUS_VOTING_PERIOD";
    case ProposalStatus.PROPOSAL_STATUS_PASSED:
      return "PROPOSAL_STATUS_PASSED";
    case ProposalStatus.PROPOSAL_STATUS_REJECTED:
      return "PROPOSAL_STATUS_REJECTED";
    case ProposalStatus.PROPOSAL_STATUS_FAILED:
      return "PROPOSAL_STATUS_FAILED";
    default:
      return "UNKNOWN";
  }
}

/**
 * WeightedVoteOption defines a unit of vote for vote split.
 *
 * Since: cosmos-sdk 0.43
 */
export interface WeightedVoteOption {
  option: VoteOption;
  weight: string;
}

/**
 * TextProposal defines a standard text proposal whose changes need to be
 * manually updated in case of approval.
 */
export interface TextProposal {
  title: string;
  description: string;
}

/**
 * Deposit defines an amount deposited by an account address to an active
 * proposal.
 */
export interface Deposit {
  proposal_id: number;
  depositor: string;
  amount: Coin[];
}

/** Proposal defines the core field members of a governance proposal. */
export interface Proposal {
  proposal_id: number;
  content: Any | undefined;
  status: ProposalStatus;
  final_tally_result: TallyResult | undefined;
  submit_time: Date | undefined;
  deposit_end_time: Date | undefined;
  total_deposit: Coin[];
  voting_start_time: Date | undefined;
  voting_end_time: Date | undefined;
}

/** TallyResult defines a standard tally for a governance proposal. */
export interface TallyResult {
  yes: string;
  abstain: string;
  no: string;
  no_with_veto: string;
}

/**
 * Vote defines a vote on a governance proposal.
 * A Vote consists of a proposal ID, the voter, and the vote option.
 */
export interface Vote {
  proposal_id: number;
  voter: string;
  /**
   * Deprecated: Prefer to use `options` instead. This field is set in queries
   * if and only if `len(options) == 1` and that option has weight 1. In all
   * other cases, this field will default to VOTE_OPTION_UNSPECIFIED.
   *
   * @deprecated
   */
  option: VoteOption;
  /** Since: cosmos-sdk 0.43 */
  options: WeightedVoteOption[];
}

/** DepositParams defines the params for deposits on governance proposals. */
export interface DepositParams {
  /** Minimum deposit for a proposal to enter voting period. */
  min_deposit: Coin[];
  /**
   * Maximum period for Atom holders to deposit on a proposal. Initial value: 2
   *  months.
   */
  max_deposit_period: Duration | undefined;
}

/** VotingParams defines the params for voting on governance proposals. */
export interface VotingParams {
  /** Length of the voting period. */
  voting_period: Duration | undefined;
}

/** TallyParams defines the params for tallying votes on governance proposals. */
export interface TallyParams {
  /**
   * Minimum percentage of total stake needed to vote for a result to be
   *  considered valid.
   */
  quorum: Uint8Array;
  /** Minimum proportion of Yes votes for proposal to pass. Default value: 0.5. */
  threshold: Uint8Array;
  /**
   * Minimum value of Veto votes to Total votes ratio for proposal to be
   *  vetoed. Default value: 1/3.
   */
  veto_threshold: Uint8Array;
}

const baseWeightedVoteOption: object = { option: 0, weight: "" };

export const WeightedVoteOption = {
  encode(
    message: WeightedVoteOption,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.option !== 0) {
      writer.uint32(8).int32(message.option);
    }
    if (message.weight !== "") {
      writer.uint32(18).string(message.weight);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): WeightedVoteOption {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWeightedVoteOption } as WeightedVoteOption;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.option = reader.int32() as any;
          break;
        case 2:
          message.weight = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WeightedVoteOption {
    const message = { ...baseWeightedVoteOption } as WeightedVoteOption;
    if (object.option !== undefined && object.option !== null) {
      message.option = voteOptionFromJSON(object.option);
    } else {
      message.option = 0;
    }
    if (object.weight !== undefined && object.weight !== null) {
      message.weight = String(object.weight);
    } else {
      message.weight = "";
    }
    return message;
  },

  toJSON(message: WeightedVoteOption): unknown {
    const obj: any = {};
    message.option !== undefined &&
      (obj.option = voteOptionToJSON(message.option));
    message.weight !== undefined && (obj.weight = message.weight);
    return obj;
  },

  fromPartial(object: DeepPartial<WeightedVoteOption>): WeightedVoteOption {
    const message = { ...baseWeightedVoteOption } as WeightedVoteOption;
    if (object.option !== undefined && object.option !== null) {
      message.option = object.option;
    } else {
      message.option = 0;
    }
    if (object.weight !== undefined && object.weight !== null) {
      message.weight = object.weight;
    } else {
      message.weight = "";
    }
    return message;
  },
};

const baseTextProposal: object = { title: "", description: "" };

export const TextProposal = {
  encode(message: TextProposal, writer: Writer = Writer.create()): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): TextProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTextProposal } as TextProposal;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.title = reader.string();
          break;
        case 2:
          message.description = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): TextProposal {
    const message = { ...baseTextProposal } as TextProposal;
    if (object.title !== undefined && object.title !== null) {
      message.title = String(object.title);
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = String(object.description);
    } else {
      message.description = "";
    }
    return message;
  },

  toJSON(message: TextProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    return obj;
  },

  fromPartial(object: DeepPartial<TextProposal>): TextProposal {
    const message = { ...baseTextProposal } as TextProposal;
    if (object.title !== undefined && object.title !== null) {
      message.title = object.title;
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = object.description;
    } else {
      message.description = "";
    }
    return message;
  },
};

const baseDeposit: object = { proposal_id: 0, depositor: "" };

export const Deposit = {
  encode(message: Deposit, writer: Writer = Writer.create()): Writer {
    if (message.proposal_id !== 0) {
      writer.uint32(8).uint64(message.proposal_id);
    }
    if (message.depositor !== "") {
      writer.uint32(18).string(message.depositor);
    }
    for (const v of message.amount) {
      Coin.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Deposit {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDeposit } as Deposit;
    message.amount = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposal_id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.depositor = reader.string();
          break;
        case 3:
          message.amount.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Deposit {
    const message = { ...baseDeposit } as Deposit;
    message.amount = [];
    if (object.proposal_id !== undefined && object.proposal_id !== null) {
      message.proposal_id = Number(object.proposal_id);
    } else {
      message.proposal_id = 0;
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = String(object.depositor);
    } else {
      message.depositor = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Deposit): unknown {
    const obj: any = {};
    message.proposal_id !== undefined &&
      (obj.proposal_id = message.proposal_id);
    message.depositor !== undefined && (obj.depositor = message.depositor);
    if (message.amount) {
      obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.amount = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Deposit>): Deposit {
    const message = { ...baseDeposit } as Deposit;
    message.amount = [];
    if (object.proposal_id !== undefined && object.proposal_id !== null) {
      message.proposal_id = object.proposal_id;
    } else {
      message.proposal_id = 0;
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = object.depositor;
    } else {
      message.depositor = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseProposal: object = { proposal_id: 0, status: 0 };

export const Proposal = {
  encode(message: Proposal, writer: Writer = Writer.create()): Writer {
    if (message.proposal_id !== 0) {
      writer.uint32(8).uint64(message.proposal_id);
    }
    if (message.content !== undefined) {
      Any.encode(message.content, writer.uint32(18).fork()).ldelim();
    }
    if (message.status !== 0) {
      writer.uint32(24).int32(message.status);
    }
    if (message.final_tally_result !== undefined) {
      TallyResult.encode(
        message.final_tally_result,
        writer.uint32(34).fork()
      ).ldelim();
    }
    if (message.submit_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.submit_time),
        writer.uint32(42).fork()
      ).ldelim();
    }
    if (message.deposit_end_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.deposit_end_time),
        writer.uint32(50).fork()
      ).ldelim();
    }
    for (const v of message.total_deposit) {
      Coin.encode(v!, writer.uint32(58).fork()).ldelim();
    }
    if (message.voting_start_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.voting_start_time),
        writer.uint32(66).fork()
      ).ldelim();
    }
    if (message.voting_end_time !== undefined) {
      Timestamp.encode(
        toTimestamp(message.voting_end_time),
        writer.uint32(74).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Proposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseProposal } as Proposal;
    message.total_deposit = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposal_id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.content = Any.decode(reader, reader.uint32());
          break;
        case 3:
          message.status = reader.int32() as any;
          break;
        case 4:
          message.final_tally_result = TallyResult.decode(
            reader,
            reader.uint32()
          );
          break;
        case 5:
          message.submit_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.deposit_end_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.total_deposit.push(Coin.decode(reader, reader.uint32()));
          break;
        case 8:
          message.voting_start_time = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        case 9:
          message.voting_end_time = fromTimestamp(
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

  fromJSON(object: any): Proposal {
    const message = { ...baseProposal } as Proposal;
    message.total_deposit = [];
    if (object.proposal_id !== undefined && object.proposal_id !== null) {
      message.proposal_id = Number(object.proposal_id);
    } else {
      message.proposal_id = 0;
    }
    if (object.content !== undefined && object.content !== null) {
      message.content = Any.fromJSON(object.content);
    } else {
      message.content = undefined;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = proposalStatusFromJSON(object.status);
    } else {
      message.status = 0;
    }
    if (
      object.final_tally_result !== undefined &&
      object.final_tally_result !== null
    ) {
      message.final_tally_result = TallyResult.fromJSON(
        object.final_tally_result
      );
    } else {
      message.final_tally_result = undefined;
    }
    if (object.submit_time !== undefined && object.submit_time !== null) {
      message.submit_time = fromJsonTimestamp(object.submit_time);
    } else {
      message.submit_time = undefined;
    }
    if (
      object.deposit_end_time !== undefined &&
      object.deposit_end_time !== null
    ) {
      message.deposit_end_time = fromJsonTimestamp(object.deposit_end_time);
    } else {
      message.deposit_end_time = undefined;
    }
    if (object.total_deposit !== undefined && object.total_deposit !== null) {
      for (const e of object.total_deposit) {
        message.total_deposit.push(Coin.fromJSON(e));
      }
    }
    if (
      object.voting_start_time !== undefined &&
      object.voting_start_time !== null
    ) {
      message.voting_start_time = fromJsonTimestamp(object.voting_start_time);
    } else {
      message.voting_start_time = undefined;
    }
    if (
      object.voting_end_time !== undefined &&
      object.voting_end_time !== null
    ) {
      message.voting_end_time = fromJsonTimestamp(object.voting_end_time);
    } else {
      message.voting_end_time = undefined;
    }
    return message;
  },

  toJSON(message: Proposal): unknown {
    const obj: any = {};
    message.proposal_id !== undefined &&
      (obj.proposal_id = message.proposal_id);
    message.content !== undefined &&
      (obj.content = message.content ? Any.toJSON(message.content) : undefined);
    message.status !== undefined &&
      (obj.status = proposalStatusToJSON(message.status));
    message.final_tally_result !== undefined &&
      (obj.final_tally_result = message.final_tally_result
        ? TallyResult.toJSON(message.final_tally_result)
        : undefined);
    message.submit_time !== undefined &&
      (obj.submit_time =
        message.submit_time !== undefined
          ? message.submit_time.toISOString()
          : null);
    message.deposit_end_time !== undefined &&
      (obj.deposit_end_time =
        message.deposit_end_time !== undefined
          ? message.deposit_end_time.toISOString()
          : null);
    if (message.total_deposit) {
      obj.total_deposit = message.total_deposit.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.total_deposit = [];
    }
    message.voting_start_time !== undefined &&
      (obj.voting_start_time =
        message.voting_start_time !== undefined
          ? message.voting_start_time.toISOString()
          : null);
    message.voting_end_time !== undefined &&
      (obj.voting_end_time =
        message.voting_end_time !== undefined
          ? message.voting_end_time.toISOString()
          : null);
    return obj;
  },

  fromPartial(object: DeepPartial<Proposal>): Proposal {
    const message = { ...baseProposal } as Proposal;
    message.total_deposit = [];
    if (object.proposal_id !== undefined && object.proposal_id !== null) {
      message.proposal_id = object.proposal_id;
    } else {
      message.proposal_id = 0;
    }
    if (object.content !== undefined && object.content !== null) {
      message.content = Any.fromPartial(object.content);
    } else {
      message.content = undefined;
    }
    if (object.status !== undefined && object.status !== null) {
      message.status = object.status;
    } else {
      message.status = 0;
    }
    if (
      object.final_tally_result !== undefined &&
      object.final_tally_result !== null
    ) {
      message.final_tally_result = TallyResult.fromPartial(
        object.final_tally_result
      );
    } else {
      message.final_tally_result = undefined;
    }
    if (object.submit_time !== undefined && object.submit_time !== null) {
      message.submit_time = object.submit_time;
    } else {
      message.submit_time = undefined;
    }
    if (
      object.deposit_end_time !== undefined &&
      object.deposit_end_time !== null
    ) {
      message.deposit_end_time = object.deposit_end_time;
    } else {
      message.deposit_end_time = undefined;
    }
    if (object.total_deposit !== undefined && object.total_deposit !== null) {
      for (const e of object.total_deposit) {
        message.total_deposit.push(Coin.fromPartial(e));
      }
    }
    if (
      object.voting_start_time !== undefined &&
      object.voting_start_time !== null
    ) {
      message.voting_start_time = object.voting_start_time;
    } else {
      message.voting_start_time = undefined;
    }
    if (
      object.voting_end_time !== undefined &&
      object.voting_end_time !== null
    ) {
      message.voting_end_time = object.voting_end_time;
    } else {
      message.voting_end_time = undefined;
    }
    return message;
  },
};

const baseTallyResult: object = {
  yes: "",
  abstain: "",
  no: "",
  no_with_veto: "",
};

export const TallyResult = {
  encode(message: TallyResult, writer: Writer = Writer.create()): Writer {
    if (message.yes !== "") {
      writer.uint32(10).string(message.yes);
    }
    if (message.abstain !== "") {
      writer.uint32(18).string(message.abstain);
    }
    if (message.no !== "") {
      writer.uint32(26).string(message.no);
    }
    if (message.no_with_veto !== "") {
      writer.uint32(34).string(message.no_with_veto);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): TallyResult {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTallyResult } as TallyResult;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.yes = reader.string();
          break;
        case 2:
          message.abstain = reader.string();
          break;
        case 3:
          message.no = reader.string();
          break;
        case 4:
          message.no_with_veto = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): TallyResult {
    const message = { ...baseTallyResult } as TallyResult;
    if (object.yes !== undefined && object.yes !== null) {
      message.yes = String(object.yes);
    } else {
      message.yes = "";
    }
    if (object.abstain !== undefined && object.abstain !== null) {
      message.abstain = String(object.abstain);
    } else {
      message.abstain = "";
    }
    if (object.no !== undefined && object.no !== null) {
      message.no = String(object.no);
    } else {
      message.no = "";
    }
    if (object.no_with_veto !== undefined && object.no_with_veto !== null) {
      message.no_with_veto = String(object.no_with_veto);
    } else {
      message.no_with_veto = "";
    }
    return message;
  },

  toJSON(message: TallyResult): unknown {
    const obj: any = {};
    message.yes !== undefined && (obj.yes = message.yes);
    message.abstain !== undefined && (obj.abstain = message.abstain);
    message.no !== undefined && (obj.no = message.no);
    message.no_with_veto !== undefined &&
      (obj.no_with_veto = message.no_with_veto);
    return obj;
  },

  fromPartial(object: DeepPartial<TallyResult>): TallyResult {
    const message = { ...baseTallyResult } as TallyResult;
    if (object.yes !== undefined && object.yes !== null) {
      message.yes = object.yes;
    } else {
      message.yes = "";
    }
    if (object.abstain !== undefined && object.abstain !== null) {
      message.abstain = object.abstain;
    } else {
      message.abstain = "";
    }
    if (object.no !== undefined && object.no !== null) {
      message.no = object.no;
    } else {
      message.no = "";
    }
    if (object.no_with_veto !== undefined && object.no_with_veto !== null) {
      message.no_with_veto = object.no_with_veto;
    } else {
      message.no_with_veto = "";
    }
    return message;
  },
};

const baseVote: object = { proposal_id: 0, voter: "", option: 0 };

export const Vote = {
  encode(message: Vote, writer: Writer = Writer.create()): Writer {
    if (message.proposal_id !== 0) {
      writer.uint32(8).uint64(message.proposal_id);
    }
    if (message.voter !== "") {
      writer.uint32(18).string(message.voter);
    }
    if (message.option !== 0) {
      writer.uint32(24).int32(message.option);
    }
    for (const v of message.options) {
      WeightedVoteOption.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Vote {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseVote } as Vote;
    message.options = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposal_id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.voter = reader.string();
          break;
        case 3:
          message.option = reader.int32() as any;
          break;
        case 4:
          message.options.push(
            WeightedVoteOption.decode(reader, reader.uint32())
          );
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
    message.options = [];
    if (object.proposal_id !== undefined && object.proposal_id !== null) {
      message.proposal_id = Number(object.proposal_id);
    } else {
      message.proposal_id = 0;
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = String(object.voter);
    } else {
      message.voter = "";
    }
    if (object.option !== undefined && object.option !== null) {
      message.option = voteOptionFromJSON(object.option);
    } else {
      message.option = 0;
    }
    if (object.options !== undefined && object.options !== null) {
      for (const e of object.options) {
        message.options.push(WeightedVoteOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: Vote): unknown {
    const obj: any = {};
    message.proposal_id !== undefined &&
      (obj.proposal_id = message.proposal_id);
    message.voter !== undefined && (obj.voter = message.voter);
    message.option !== undefined &&
      (obj.option = voteOptionToJSON(message.option));
    if (message.options) {
      obj.options = message.options.map((e) =>
        e ? WeightedVoteOption.toJSON(e) : undefined
      );
    } else {
      obj.options = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<Vote>): Vote {
    const message = { ...baseVote } as Vote;
    message.options = [];
    if (object.proposal_id !== undefined && object.proposal_id !== null) {
      message.proposal_id = object.proposal_id;
    } else {
      message.proposal_id = 0;
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = object.voter;
    } else {
      message.voter = "";
    }
    if (object.option !== undefined && object.option !== null) {
      message.option = object.option;
    } else {
      message.option = 0;
    }
    if (object.options !== undefined && object.options !== null) {
      for (const e of object.options) {
        message.options.push(WeightedVoteOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseDepositParams: object = {};

export const DepositParams = {
  encode(message: DepositParams, writer: Writer = Writer.create()): Writer {
    for (const v of message.min_deposit) {
      Coin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.max_deposit_period !== undefined) {
      Duration.encode(
        message.max_deposit_period,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DepositParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDepositParams } as DepositParams;
    message.min_deposit = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.min_deposit.push(Coin.decode(reader, reader.uint32()));
          break;
        case 2:
          message.max_deposit_period = Duration.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DepositParams {
    const message = { ...baseDepositParams } as DepositParams;
    message.min_deposit = [];
    if (object.min_deposit !== undefined && object.min_deposit !== null) {
      for (const e of object.min_deposit) {
        message.min_deposit.push(Coin.fromJSON(e));
      }
    }
    if (
      object.max_deposit_period !== undefined &&
      object.max_deposit_period !== null
    ) {
      message.max_deposit_period = Duration.fromJSON(object.max_deposit_period);
    } else {
      message.max_deposit_period = undefined;
    }
    return message;
  },

  toJSON(message: DepositParams): unknown {
    const obj: any = {};
    if (message.min_deposit) {
      obj.min_deposit = message.min_deposit.map((e) =>
        e ? Coin.toJSON(e) : undefined
      );
    } else {
      obj.min_deposit = [];
    }
    message.max_deposit_period !== undefined &&
      (obj.max_deposit_period = message.max_deposit_period
        ? Duration.toJSON(message.max_deposit_period)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<DepositParams>): DepositParams {
    const message = { ...baseDepositParams } as DepositParams;
    message.min_deposit = [];
    if (object.min_deposit !== undefined && object.min_deposit !== null) {
      for (const e of object.min_deposit) {
        message.min_deposit.push(Coin.fromPartial(e));
      }
    }
    if (
      object.max_deposit_period !== undefined &&
      object.max_deposit_period !== null
    ) {
      message.max_deposit_period = Duration.fromPartial(
        object.max_deposit_period
      );
    } else {
      message.max_deposit_period = undefined;
    }
    return message;
  },
};

const baseVotingParams: object = {};

export const VotingParams = {
  encode(message: VotingParams, writer: Writer = Writer.create()): Writer {
    if (message.voting_period !== undefined) {
      Duration.encode(message.voting_period, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): VotingParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseVotingParams } as VotingParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.voting_period = Duration.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): VotingParams {
    const message = { ...baseVotingParams } as VotingParams;
    if (object.voting_period !== undefined && object.voting_period !== null) {
      message.voting_period = Duration.fromJSON(object.voting_period);
    } else {
      message.voting_period = undefined;
    }
    return message;
  },

  toJSON(message: VotingParams): unknown {
    const obj: any = {};
    message.voting_period !== undefined &&
      (obj.voting_period = message.voting_period
        ? Duration.toJSON(message.voting_period)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<VotingParams>): VotingParams {
    const message = { ...baseVotingParams } as VotingParams;
    if (object.voting_period !== undefined && object.voting_period !== null) {
      message.voting_period = Duration.fromPartial(object.voting_period);
    } else {
      message.voting_period = undefined;
    }
    return message;
  },
};

const baseTallyParams: object = {};

export const TallyParams = {
  encode(message: TallyParams, writer: Writer = Writer.create()): Writer {
    if (message.quorum.length !== 0) {
      writer.uint32(10).bytes(message.quorum);
    }
    if (message.threshold.length !== 0) {
      writer.uint32(18).bytes(message.threshold);
    }
    if (message.veto_threshold.length !== 0) {
      writer.uint32(26).bytes(message.veto_threshold);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): TallyParams {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseTallyParams } as TallyParams;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.quorum = reader.bytes();
          break;
        case 2:
          message.threshold = reader.bytes();
          break;
        case 3:
          message.veto_threshold = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): TallyParams {
    const message = { ...baseTallyParams } as TallyParams;
    if (object.quorum !== undefined && object.quorum !== null) {
      message.quorum = bytesFromBase64(object.quorum);
    }
    if (object.threshold !== undefined && object.threshold !== null) {
      message.threshold = bytesFromBase64(object.threshold);
    }
    if (object.veto_threshold !== undefined && object.veto_threshold !== null) {
      message.veto_threshold = bytesFromBase64(object.veto_threshold);
    }
    return message;
  },

  toJSON(message: TallyParams): unknown {
    const obj: any = {};
    message.quorum !== undefined &&
      (obj.quorum = base64FromBytes(
        message.quorum !== undefined ? message.quorum : new Uint8Array()
      ));
    message.threshold !== undefined &&
      (obj.threshold = base64FromBytes(
        message.threshold !== undefined ? message.threshold : new Uint8Array()
      ));
    message.veto_threshold !== undefined &&
      (obj.veto_threshold = base64FromBytes(
        message.veto_threshold !== undefined
          ? message.veto_threshold
          : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<TallyParams>): TallyParams {
    const message = { ...baseTallyParams } as TallyParams;
    if (object.quorum !== undefined && object.quorum !== null) {
      message.quorum = object.quorum;
    } else {
      message.quorum = new Uint8Array();
    }
    if (object.threshold !== undefined && object.threshold !== null) {
      message.threshold = object.threshold;
    } else {
      message.threshold = new Uint8Array();
    }
    if (object.veto_threshold !== undefined && object.veto_threshold !== null) {
      message.veto_threshold = object.veto_threshold;
    } else {
      message.veto_threshold = new Uint8Array();
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
