/* eslint-disable */
import {
  ProposalStatus,
  Proposal,
  Vote,
  VotingParams,
  DepositParams,
  TallyParams,
  Deposit,
  TallyResult,
  proposalStatusFromJSON,
  proposalStatusToJSON,
} from "../../../cosmos/gov/v1beta1/gov";
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  PageRequest,
  PageResponse,
} from "../../../cosmos/base/query/v1beta1/pagination";

export const protobufPackage = "cosmos.gov.v1beta1";

/** QueryProposalRequest is the request type for the Query/Proposal RPC method. */
export interface QueryProposalRequest {
  /** proposal_id defines the unique id of the proposal. */
  proposalId: number;
}

/** QueryProposalResponse is the response type for the Query/Proposal RPC method. */
export interface QueryProposalResponse {
  proposal: Proposal | undefined;
}

/** QueryProposalsRequest is the request type for the Query/Proposals RPC method. */
export interface QueryProposalsRequest {
  /** proposal_status defines the status of the proposals. */
  proposalStatus: ProposalStatus;
  /** voter defines the voter address for the proposals. */
  voter: string;
  /** depositor defines the deposit addresses from the proposals. */
  depositor: string;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryProposalsResponse is the response type for the Query/Proposals RPC
 * method.
 */
export interface QueryProposalsResponse {
  proposals: Proposal[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/** QueryVoteRequest is the request type for the Query/Vote RPC method. */
export interface QueryVoteRequest {
  /** proposal_id defines the unique id of the proposal. */
  proposalId: number;
  /** voter defines the oter address for the proposals. */
  voter: string;
}

/** QueryVoteResponse is the response type for the Query/Vote RPC method. */
export interface QueryVoteResponse {
  /** vote defined the queried vote. */
  vote: Vote | undefined;
}

/** QueryVotesRequest is the request type for the Query/Votes RPC method. */
export interface QueryVotesRequest {
  /** proposal_id defines the unique id of the proposal. */
  proposalId: number;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/** QueryVotesResponse is the response type for the Query/Votes RPC method. */
export interface QueryVotesResponse {
  /** votes defined the queried votes. */
  votes: Vote[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {
  /**
   * params_type defines which parameters to query for, can be one of "voting",
   * "tallying" or "deposit".
   */
  paramsType: string;
}

/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** voting_params defines the parameters related to voting. */
  votingParams: VotingParams | undefined;
  /** deposit_params defines the parameters related to deposit. */
  depositParams: DepositParams | undefined;
  /** tally_params defines the parameters related to tally. */
  tallyParams: TallyParams | undefined;
}

/** QueryDepositRequest is the request type for the Query/Deposit RPC method. */
export interface QueryDepositRequest {
  /** proposal_id defines the unique id of the proposal. */
  proposalId: number;
  /** depositor defines the deposit addresses from the proposals. */
  depositor: string;
}

/** QueryDepositResponse is the response type for the Query/Deposit RPC method. */
export interface QueryDepositResponse {
  /** deposit defines the requested deposit. */
  deposit: Deposit | undefined;
}

/** QueryDepositsRequest is the request type for the Query/Deposits RPC method. */
export interface QueryDepositsRequest {
  /** proposal_id defines the unique id of the proposal. */
  proposalId: number;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/** QueryDepositsResponse is the response type for the Query/Deposits RPC method. */
export interface QueryDepositsResponse {
  deposits: Deposit[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/** QueryTallyResultRequest is the request type for the Query/Tally RPC method. */
export interface QueryTallyResultRequest {
  /** proposal_id defines the unique id of the proposal. */
  proposalId: number;
}

/** QueryTallyResultResponse is the response type for the Query/Tally RPC method. */
export interface QueryTallyResultResponse {
  /** tally defines the requested tally. */
  tally: TallyResult | undefined;
}

const baseQueryProposalRequest: object = { proposalId: 0 };

export const QueryProposalRequest = {
  encode(
    message: QueryProposalRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.proposalId !== 0) {
      writer.uint32(8).uint64(message.proposalId);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryProposalRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryProposalRequest } as QueryProposalRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryProposalRequest {
    const message = { ...baseQueryProposalRequest } as QueryProposalRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = Number(object.proposalId);
    } else {
      message.proposalId = 0;
    }
    return message;
  },

  toJSON(message: QueryProposalRequest): unknown {
    const obj: any = {};
    message.proposalId !== undefined && (obj.proposalId = message.proposalId);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryProposalRequest>): QueryProposalRequest {
    const message = { ...baseQueryProposalRequest } as QueryProposalRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = object.proposalId;
    } else {
      message.proposalId = 0;
    }
    return message;
  },
};

const baseQueryProposalResponse: object = {};

export const QueryProposalResponse = {
  encode(
    message: QueryProposalResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.proposal !== undefined) {
      Proposal.encode(message.proposal, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryProposalResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryProposalResponse } as QueryProposalResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposal = Proposal.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryProposalResponse {
    const message = { ...baseQueryProposalResponse } as QueryProposalResponse;
    if (object.proposal !== undefined && object.proposal !== null) {
      message.proposal = Proposal.fromJSON(object.proposal);
    } else {
      message.proposal = undefined;
    }
    return message;
  },

  toJSON(message: QueryProposalResponse): unknown {
    const obj: any = {};
    message.proposal !== undefined &&
      (obj.proposal = message.proposal
        ? Proposal.toJSON(message.proposal)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryProposalResponse>
  ): QueryProposalResponse {
    const message = { ...baseQueryProposalResponse } as QueryProposalResponse;
    if (object.proposal !== undefined && object.proposal !== null) {
      message.proposal = Proposal.fromPartial(object.proposal);
    } else {
      message.proposal = undefined;
    }
    return message;
  },
};

const baseQueryProposalsRequest: object = {
  proposalStatus: 0,
  voter: "",
  depositor: "",
};

export const QueryProposalsRequest = {
  encode(
    message: QueryProposalsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.proposalStatus !== 0) {
      writer.uint32(8).int32(message.proposalStatus);
    }
    if (message.voter !== "") {
      writer.uint32(18).string(message.voter);
    }
    if (message.depositor !== "") {
      writer.uint32(26).string(message.depositor);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryProposalsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryProposalsRequest } as QueryProposalsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalStatus = reader.int32() as any;
          break;
        case 2:
          message.voter = reader.string();
          break;
        case 3:
          message.depositor = reader.string();
          break;
        case 4:
          message.pagination = PageRequest.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryProposalsRequest {
    const message = { ...baseQueryProposalsRequest } as QueryProposalsRequest;
    if (object.proposalStatus !== undefined && object.proposalStatus !== null) {
      message.proposalStatus = proposalStatusFromJSON(object.proposalStatus);
    } else {
      message.proposalStatus = 0;
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = String(object.voter);
    } else {
      message.voter = "";
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = String(object.depositor);
    } else {
      message.depositor = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryProposalsRequest): unknown {
    const obj: any = {};
    message.proposalStatus !== undefined &&
      (obj.proposalStatus = proposalStatusToJSON(message.proposalStatus));
    message.voter !== undefined && (obj.voter = message.voter);
    message.depositor !== undefined && (obj.depositor = message.depositor);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryProposalsRequest>
  ): QueryProposalsRequest {
    const message = { ...baseQueryProposalsRequest } as QueryProposalsRequest;
    if (object.proposalStatus !== undefined && object.proposalStatus !== null) {
      message.proposalStatus = object.proposalStatus;
    } else {
      message.proposalStatus = 0;
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = object.voter;
    } else {
      message.voter = "";
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = object.depositor;
    } else {
      message.depositor = "";
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryProposalsResponse: object = {};

export const QueryProposalsResponse = {
  encode(
    message: QueryProposalsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.proposals) {
      Proposal.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryProposalsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryProposalsResponse } as QueryProposalsResponse;
    message.proposals = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposals.push(Proposal.decode(reader, reader.uint32()));
          break;
        case 2:
          message.pagination = PageResponse.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryProposalsResponse {
    const message = { ...baseQueryProposalsResponse } as QueryProposalsResponse;
    message.proposals = [];
    if (object.proposals !== undefined && object.proposals !== null) {
      for (const e of object.proposals) {
        message.proposals.push(Proposal.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryProposalsResponse): unknown {
    const obj: any = {};
    if (message.proposals) {
      obj.proposals = message.proposals.map((e) =>
        e ? Proposal.toJSON(e) : undefined
      );
    } else {
      obj.proposals = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryProposalsResponse>
  ): QueryProposalsResponse {
    const message = { ...baseQueryProposalsResponse } as QueryProposalsResponse;
    message.proposals = [];
    if (object.proposals !== undefined && object.proposals !== null) {
      for (const e of object.proposals) {
        message.proposals.push(Proposal.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryVoteRequest: object = { proposalId: 0, voter: "" };

export const QueryVoteRequest = {
  encode(message: QueryVoteRequest, writer: Writer = Writer.create()): Writer {
    if (message.proposalId !== 0) {
      writer.uint32(8).uint64(message.proposalId);
    }
    if (message.voter !== "") {
      writer.uint32(18).string(message.voter);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryVoteRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryVoteRequest } as QueryVoteRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.voter = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryVoteRequest {
    const message = { ...baseQueryVoteRequest } as QueryVoteRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = Number(object.proposalId);
    } else {
      message.proposalId = 0;
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = String(object.voter);
    } else {
      message.voter = "";
    }
    return message;
  },

  toJSON(message: QueryVoteRequest): unknown {
    const obj: any = {};
    message.proposalId !== undefined && (obj.proposalId = message.proposalId);
    message.voter !== undefined && (obj.voter = message.voter);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryVoteRequest>): QueryVoteRequest {
    const message = { ...baseQueryVoteRequest } as QueryVoteRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = object.proposalId;
    } else {
      message.proposalId = 0;
    }
    if (object.voter !== undefined && object.voter !== null) {
      message.voter = object.voter;
    } else {
      message.voter = "";
    }
    return message;
  },
};

const baseQueryVoteResponse: object = {};

export const QueryVoteResponse = {
  encode(message: QueryVoteResponse, writer: Writer = Writer.create()): Writer {
    if (message.vote !== undefined) {
      Vote.encode(message.vote, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryVoteResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryVoteResponse } as QueryVoteResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.vote = Vote.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryVoteResponse {
    const message = { ...baseQueryVoteResponse } as QueryVoteResponse;
    if (object.vote !== undefined && object.vote !== null) {
      message.vote = Vote.fromJSON(object.vote);
    } else {
      message.vote = undefined;
    }
    return message;
  },

  toJSON(message: QueryVoteResponse): unknown {
    const obj: any = {};
    message.vote !== undefined &&
      (obj.vote = message.vote ? Vote.toJSON(message.vote) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryVoteResponse>): QueryVoteResponse {
    const message = { ...baseQueryVoteResponse } as QueryVoteResponse;
    if (object.vote !== undefined && object.vote !== null) {
      message.vote = Vote.fromPartial(object.vote);
    } else {
      message.vote = undefined;
    }
    return message;
  },
};

const baseQueryVotesRequest: object = { proposalId: 0 };

export const QueryVotesRequest = {
  encode(message: QueryVotesRequest, writer: Writer = Writer.create()): Writer {
    if (message.proposalId !== 0) {
      writer.uint32(8).uint64(message.proposalId);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryVotesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryVotesRequest } as QueryVotesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.pagination = PageRequest.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryVotesRequest {
    const message = { ...baseQueryVotesRequest } as QueryVotesRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = Number(object.proposalId);
    } else {
      message.proposalId = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryVotesRequest): unknown {
    const obj: any = {};
    message.proposalId !== undefined && (obj.proposalId = message.proposalId);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryVotesRequest>): QueryVotesRequest {
    const message = { ...baseQueryVotesRequest } as QueryVotesRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = object.proposalId;
    } else {
      message.proposalId = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryVotesResponse: object = {};

export const QueryVotesResponse = {
  encode(
    message: QueryVotesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.votes) {
      Vote.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryVotesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryVotesResponse } as QueryVotesResponse;
    message.votes = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.votes.push(Vote.decode(reader, reader.uint32()));
          break;
        case 2:
          message.pagination = PageResponse.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryVotesResponse {
    const message = { ...baseQueryVotesResponse } as QueryVotesResponse;
    message.votes = [];
    if (object.votes !== undefined && object.votes !== null) {
      for (const e of object.votes) {
        message.votes.push(Vote.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryVotesResponse): unknown {
    const obj: any = {};
    if (message.votes) {
      obj.votes = message.votes.map((e) => (e ? Vote.toJSON(e) : undefined));
    } else {
      obj.votes = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryVotesResponse>): QueryVotesResponse {
    const message = { ...baseQueryVotesResponse } as QueryVotesResponse;
    message.votes = [];
    if (object.votes !== undefined && object.votes !== null) {
      for (const e of object.votes) {
        message.votes.push(Vote.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryParamsRequest: object = { paramsType: "" };

export const QueryParamsRequest = {
  encode(
    message: QueryParamsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.paramsType !== "") {
      writer.uint32(10).string(message.paramsType);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryParamsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.paramsType = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryParamsRequest {
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    if (object.paramsType !== undefined && object.paramsType !== null) {
      message.paramsType = String(object.paramsType);
    } else {
      message.paramsType = "";
    }
    return message;
  },

  toJSON(message: QueryParamsRequest): unknown {
    const obj: any = {};
    message.paramsType !== undefined && (obj.paramsType = message.paramsType);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryParamsRequest>): QueryParamsRequest {
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    if (object.paramsType !== undefined && object.paramsType !== null) {
      message.paramsType = object.paramsType;
    } else {
      message.paramsType = "";
    }
    return message;
  },
};

const baseQueryParamsResponse: object = {};

export const QueryParamsResponse = {
  encode(
    message: QueryParamsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.votingParams !== undefined) {
      VotingParams.encode(
        message.votingParams,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.depositParams !== undefined) {
      DepositParams.encode(
        message.depositParams,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.tallyParams !== undefined) {
      TallyParams.encode(
        message.tallyParams,
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryParamsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.votingParams = VotingParams.decode(reader, reader.uint32());
          break;
        case 2:
          message.depositParams = DepositParams.decode(reader, reader.uint32());
          break;
        case 3:
          message.tallyParams = TallyParams.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryParamsResponse {
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    if (object.votingParams !== undefined && object.votingParams !== null) {
      message.votingParams = VotingParams.fromJSON(object.votingParams);
    } else {
      message.votingParams = undefined;
    }
    if (object.depositParams !== undefined && object.depositParams !== null) {
      message.depositParams = DepositParams.fromJSON(object.depositParams);
    } else {
      message.depositParams = undefined;
    }
    if (object.tallyParams !== undefined && object.tallyParams !== null) {
      message.tallyParams = TallyParams.fromJSON(object.tallyParams);
    } else {
      message.tallyParams = undefined;
    }
    return message;
  },

  toJSON(message: QueryParamsResponse): unknown {
    const obj: any = {};
    message.votingParams !== undefined &&
      (obj.votingParams = message.votingParams
        ? VotingParams.toJSON(message.votingParams)
        : undefined);
    message.depositParams !== undefined &&
      (obj.depositParams = message.depositParams
        ? DepositParams.toJSON(message.depositParams)
        : undefined);
    message.tallyParams !== undefined &&
      (obj.tallyParams = message.tallyParams
        ? TallyParams.toJSON(message.tallyParams)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryParamsResponse>): QueryParamsResponse {
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    if (object.votingParams !== undefined && object.votingParams !== null) {
      message.votingParams = VotingParams.fromPartial(object.votingParams);
    } else {
      message.votingParams = undefined;
    }
    if (object.depositParams !== undefined && object.depositParams !== null) {
      message.depositParams = DepositParams.fromPartial(object.depositParams);
    } else {
      message.depositParams = undefined;
    }
    if (object.tallyParams !== undefined && object.tallyParams !== null) {
      message.tallyParams = TallyParams.fromPartial(object.tallyParams);
    } else {
      message.tallyParams = undefined;
    }
    return message;
  },
};

const baseQueryDepositRequest: object = { proposalId: 0, depositor: "" };

export const QueryDepositRequest = {
  encode(
    message: QueryDepositRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.proposalId !== 0) {
      writer.uint32(8).uint64(message.proposalId);
    }
    if (message.depositor !== "") {
      writer.uint32(18).string(message.depositor);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDepositRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDepositRequest } as QueryDepositRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.depositor = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDepositRequest {
    const message = { ...baseQueryDepositRequest } as QueryDepositRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = Number(object.proposalId);
    } else {
      message.proposalId = 0;
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = String(object.depositor);
    } else {
      message.depositor = "";
    }
    return message;
  },

  toJSON(message: QueryDepositRequest): unknown {
    const obj: any = {};
    message.proposalId !== undefined && (obj.proposalId = message.proposalId);
    message.depositor !== undefined && (obj.depositor = message.depositor);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryDepositRequest>): QueryDepositRequest {
    const message = { ...baseQueryDepositRequest } as QueryDepositRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = object.proposalId;
    } else {
      message.proposalId = 0;
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = object.depositor;
    } else {
      message.depositor = "";
    }
    return message;
  },
};

const baseQueryDepositResponse: object = {};

export const QueryDepositResponse = {
  encode(
    message: QueryDepositResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.deposit !== undefined) {
      Deposit.encode(message.deposit, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDepositResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDepositResponse } as QueryDepositResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.deposit = Deposit.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDepositResponse {
    const message = { ...baseQueryDepositResponse } as QueryDepositResponse;
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = Deposit.fromJSON(object.deposit);
    } else {
      message.deposit = undefined;
    }
    return message;
  },

  toJSON(message: QueryDepositResponse): unknown {
    const obj: any = {};
    message.deposit !== undefined &&
      (obj.deposit = message.deposit
        ? Deposit.toJSON(message.deposit)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryDepositResponse>): QueryDepositResponse {
    const message = { ...baseQueryDepositResponse } as QueryDepositResponse;
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = Deposit.fromPartial(object.deposit);
    } else {
      message.deposit = undefined;
    }
    return message;
  },
};

const baseQueryDepositsRequest: object = { proposalId: 0 };

export const QueryDepositsRequest = {
  encode(
    message: QueryDepositsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.proposalId !== 0) {
      writer.uint32(8).uint64(message.proposalId);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDepositsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDepositsRequest } as QueryDepositsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalId = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.pagination = PageRequest.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDepositsRequest {
    const message = { ...baseQueryDepositsRequest } as QueryDepositsRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = Number(object.proposalId);
    } else {
      message.proposalId = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryDepositsRequest): unknown {
    const obj: any = {};
    message.proposalId !== undefined && (obj.proposalId = message.proposalId);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryDepositsRequest>): QueryDepositsRequest {
    const message = { ...baseQueryDepositsRequest } as QueryDepositsRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = object.proposalId;
    } else {
      message.proposalId = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryDepositsResponse: object = {};

export const QueryDepositsResponse = {
  encode(
    message: QueryDepositsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.deposits) {
      Deposit.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryDepositsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryDepositsResponse } as QueryDepositsResponse;
    message.deposits = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.deposits.push(Deposit.decode(reader, reader.uint32()));
          break;
        case 2:
          message.pagination = PageResponse.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDepositsResponse {
    const message = { ...baseQueryDepositsResponse } as QueryDepositsResponse;
    message.deposits = [];
    if (object.deposits !== undefined && object.deposits !== null) {
      for (const e of object.deposits) {
        message.deposits.push(Deposit.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryDepositsResponse): unknown {
    const obj: any = {};
    if (message.deposits) {
      obj.deposits = message.deposits.map((e) =>
        e ? Deposit.toJSON(e) : undefined
      );
    } else {
      obj.deposits = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDepositsResponse>
  ): QueryDepositsResponse {
    const message = { ...baseQueryDepositsResponse } as QueryDepositsResponse;
    message.deposits = [];
    if (object.deposits !== undefined && object.deposits !== null) {
      for (const e of object.deposits) {
        message.deposits.push(Deposit.fromPartial(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryTallyResultRequest: object = { proposalId: 0 };

export const QueryTallyResultRequest = {
  encode(
    message: QueryTallyResultRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.proposalId !== 0) {
      writer.uint32(8).uint64(message.proposalId);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryTallyResultRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryTallyResultRequest,
    } as QueryTallyResultRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proposalId = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryTallyResultRequest {
    const message = {
      ...baseQueryTallyResultRequest,
    } as QueryTallyResultRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = Number(object.proposalId);
    } else {
      message.proposalId = 0;
    }
    return message;
  },

  toJSON(message: QueryTallyResultRequest): unknown {
    const obj: any = {};
    message.proposalId !== undefined && (obj.proposalId = message.proposalId);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryTallyResultRequest>
  ): QueryTallyResultRequest {
    const message = {
      ...baseQueryTallyResultRequest,
    } as QueryTallyResultRequest;
    if (object.proposalId !== undefined && object.proposalId !== null) {
      message.proposalId = object.proposalId;
    } else {
      message.proposalId = 0;
    }
    return message;
  },
};

const baseQueryTallyResultResponse: object = {};

export const QueryTallyResultResponse = {
  encode(
    message: QueryTallyResultResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.tally !== undefined) {
      TallyResult.encode(message.tally, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryTallyResultResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryTallyResultResponse,
    } as QueryTallyResultResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.tally = TallyResult.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryTallyResultResponse {
    const message = {
      ...baseQueryTallyResultResponse,
    } as QueryTallyResultResponse;
    if (object.tally !== undefined && object.tally !== null) {
      message.tally = TallyResult.fromJSON(object.tally);
    } else {
      message.tally = undefined;
    }
    return message;
  },

  toJSON(message: QueryTallyResultResponse): unknown {
    const obj: any = {};
    message.tally !== undefined &&
      (obj.tally = message.tally
        ? TallyResult.toJSON(message.tally)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryTallyResultResponse>
  ): QueryTallyResultResponse {
    const message = {
      ...baseQueryTallyResultResponse,
    } as QueryTallyResultResponse;
    if (object.tally !== undefined && object.tally !== null) {
      message.tally = TallyResult.fromPartial(object.tally);
    } else {
      message.tally = undefined;
    }
    return message;
  },
};

/** Query defines the gRPC querier service for gov module */
export interface Query {
  /** Proposal queries proposal details based on ProposalID. */
  Proposal(request: QueryProposalRequest): Promise<QueryProposalResponse>;
  /** Proposals queries all proposals based on given status. */
  Proposals(request: QueryProposalsRequest): Promise<QueryProposalsResponse>;
  /** Vote queries voted information based on proposalID, voterAddr. */
  Vote(request: QueryVoteRequest): Promise<QueryVoteResponse>;
  /** Votes queries votes of a given proposal. */
  Votes(request: QueryVotesRequest): Promise<QueryVotesResponse>;
  /** Params queries all parameters of the gov module. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** Deposit queries single deposit information based proposalID, depositAddr. */
  Deposit(request: QueryDepositRequest): Promise<QueryDepositResponse>;
  /** Deposits queries all deposits of a single proposal. */
  Deposits(request: QueryDepositsRequest): Promise<QueryDepositsResponse>;
  /** TallyResult queries the tally of a proposal vote. */
  TallyResult(
    request: QueryTallyResultRequest
  ): Promise<QueryTallyResultResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Proposal(request: QueryProposalRequest): Promise<QueryProposalResponse> {
    const data = QueryProposalRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.gov.v1beta1.Query",
      "Proposal",
      data
    );
    return promise.then((data) =>
      QueryProposalResponse.decode(new Reader(data))
    );
  }

  Proposals(request: QueryProposalsRequest): Promise<QueryProposalsResponse> {
    const data = QueryProposalsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.gov.v1beta1.Query",
      "Proposals",
      data
    );
    return promise.then((data) =>
      QueryProposalsResponse.decode(new Reader(data))
    );
  }

  Vote(request: QueryVoteRequest): Promise<QueryVoteResponse> {
    const data = QueryVoteRequest.encode(request).finish();
    const promise = this.rpc.request("cosmos.gov.v1beta1.Query", "Vote", data);
    return promise.then((data) => QueryVoteResponse.decode(new Reader(data)));
  }

  Votes(request: QueryVotesRequest): Promise<QueryVotesResponse> {
    const data = QueryVotesRequest.encode(request).finish();
    const promise = this.rpc.request("cosmos.gov.v1beta1.Query", "Votes", data);
    return promise.then((data) => QueryVotesResponse.decode(new Reader(data)));
  }

  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.gov.v1beta1.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  Deposit(request: QueryDepositRequest): Promise<QueryDepositResponse> {
    const data = QueryDepositRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.gov.v1beta1.Query",
      "Deposit",
      data
    );
    return promise.then((data) =>
      QueryDepositResponse.decode(new Reader(data))
    );
  }

  Deposits(request: QueryDepositsRequest): Promise<QueryDepositsResponse> {
    const data = QueryDepositsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.gov.v1beta1.Query",
      "Deposits",
      data
    );
    return promise.then((data) =>
      QueryDepositsResponse.decode(new Reader(data))
    );
  }

  TallyResult(
    request: QueryTallyResultRequest
  ): Promise<QueryTallyResultResponse> {
    const data = QueryTallyResultRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.gov.v1beta1.Query",
      "TallyResult",
      data
    );
    return promise.then((data) =>
      QueryTallyResultResponse.decode(new Reader(data))
    );
  }
}

interface Rpc {
  request(
    service: string,
    method: string,
    data: Uint8Array
  ): Promise<Uint8Array>;
}

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
