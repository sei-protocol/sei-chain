/* eslint-disable */
import { Reader, util, configure, Writer } from "protobufjs/minimal";
import * as Long from "long";
import {
  Params,
  ValidatorOutstandingRewards,
  ValidatorAccumulatedCommission,
  ValidatorSlashEvent,
  DelegationDelegatorReward,
} from "../../../cosmos/distribution/v1beta1/distribution";
import {
  PageRequest,
  PageResponse,
} from "../../../cosmos/base/query/v1beta1/pagination";
import { DecCoin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.distribution.v1beta1";

/** QueryParamsRequest is the request type for the Query/Params RPC method. */
export interface QueryParamsRequest {}

/** QueryParamsResponse is the response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
  /** params defines the parameters of the module. */
  params: Params | undefined;
}

/**
 * QueryValidatorOutstandingRewardsRequest is the request type for the
 * Query/ValidatorOutstandingRewards RPC method.
 */
export interface QueryValidatorOutstandingRewardsRequest {
  /** validator_address defines the validator address to query for. */
  validator_address: string;
}

/**
 * QueryValidatorOutstandingRewardsResponse is the response type for the
 * Query/ValidatorOutstandingRewards RPC method.
 */
export interface QueryValidatorOutstandingRewardsResponse {
  rewards: ValidatorOutstandingRewards | undefined;
}

/**
 * QueryValidatorCommissionRequest is the request type for the
 * Query/ValidatorCommission RPC method
 */
export interface QueryValidatorCommissionRequest {
  /** validator_address defines the validator address to query for. */
  validator_address: string;
}

/**
 * QueryValidatorCommissionResponse is the response type for the
 * Query/ValidatorCommission RPC method
 */
export interface QueryValidatorCommissionResponse {
  /** commission defines the commision the validator received. */
  commission: ValidatorAccumulatedCommission | undefined;
}

/**
 * QueryValidatorSlashesRequest is the request type for the
 * Query/ValidatorSlashes RPC method
 */
export interface QueryValidatorSlashesRequest {
  /** validator_address defines the validator address to query for. */
  validator_address: string;
  /** starting_height defines the optional starting height to query the slashes. */
  starting_height: number;
  /** starting_height defines the optional ending height to query the slashes. */
  ending_height: number;
  /** pagination defines an optional pagination for the request. */
  pagination: PageRequest | undefined;
}

/**
 * QueryValidatorSlashesResponse is the response type for the
 * Query/ValidatorSlashes RPC method.
 */
export interface QueryValidatorSlashesResponse {
  /** slashes defines the slashes the validator received. */
  slashes: ValidatorSlashEvent[];
  /** pagination defines the pagination in the response. */
  pagination: PageResponse | undefined;
}

/**
 * QueryDelegationRewardsRequest is the request type for the
 * Query/DelegationRewards RPC method.
 */
export interface QueryDelegationRewardsRequest {
  /** delegator_address defines the delegator address to query for. */
  delegator_address: string;
  /** validator_address defines the validator address to query for. */
  validator_address: string;
}

/**
 * QueryDelegationRewardsResponse is the response type for the
 * Query/DelegationRewards RPC method.
 */
export interface QueryDelegationRewardsResponse {
  /** rewards defines the rewards accrued by a delegation. */
  rewards: DecCoin[];
}

/**
 * QueryDelegationTotalRewardsRequest is the request type for the
 * Query/DelegationTotalRewards RPC method.
 */
export interface QueryDelegationTotalRewardsRequest {
  /** delegator_address defines the delegator address to query for. */
  delegator_address: string;
}

/**
 * QueryDelegationTotalRewardsResponse is the response type for the
 * Query/DelegationTotalRewards RPC method.
 */
export interface QueryDelegationTotalRewardsResponse {
  /** rewards defines all the rewards accrued by a delegator. */
  rewards: DelegationDelegatorReward[];
  /** total defines the sum of all the rewards. */
  total: DecCoin[];
}

/**
 * QueryDelegatorValidatorsRequest is the request type for the
 * Query/DelegatorValidators RPC method.
 */
export interface QueryDelegatorValidatorsRequest {
  /** delegator_address defines the delegator address to query for. */
  delegator_address: string;
}

/**
 * QueryDelegatorValidatorsResponse is the response type for the
 * Query/DelegatorValidators RPC method.
 */
export interface QueryDelegatorValidatorsResponse {
  /** validators defines the validators a delegator is delegating for. */
  validators: string[];
}

/**
 * QueryDelegatorWithdrawAddressRequest is the request type for the
 * Query/DelegatorWithdrawAddress RPC method.
 */
export interface QueryDelegatorWithdrawAddressRequest {
  /** delegator_address defines the delegator address to query for. */
  delegator_address: string;
}

/**
 * QueryDelegatorWithdrawAddressResponse is the response type for the
 * Query/DelegatorWithdrawAddress RPC method.
 */
export interface QueryDelegatorWithdrawAddressResponse {
  /** withdraw_address defines the delegator address to query for. */
  withdraw_address: string;
}

/**
 * QueryCommunityPoolRequest is the request type for the Query/CommunityPool RPC
 * method.
 */
export interface QueryCommunityPoolRequest {}

/**
 * QueryCommunityPoolResponse is the response type for the Query/CommunityPool
 * RPC method.
 */
export interface QueryCommunityPoolResponse {
  /** pool defines community pool's coins. */
  pool: DecCoin[];
}

const baseQueryParamsRequest: object = {};

export const QueryParamsRequest = {
  encode(_: QueryParamsRequest, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): QueryParamsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
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

  fromJSON(_: any): QueryParamsRequest {
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    return message;
  },

  toJSON(_: QueryParamsRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<QueryParamsRequest>): QueryParamsRequest {
    const message = { ...baseQueryParamsRequest } as QueryParamsRequest;
    return message;
  },
};

const baseQueryParamsResponse: object = {};

export const QueryParamsResponse = {
  encode(
    message: QueryParamsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
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
          message.params = Params.decode(reader, reader.uint32());
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
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    return message;
  },

  toJSON(message: QueryParamsResponse): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<QueryParamsResponse>): QueryParamsResponse {
    const message = { ...baseQueryParamsResponse } as QueryParamsResponse;
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    return message;
  },
};

const baseQueryValidatorOutstandingRewardsRequest: object = {
  validator_address: "",
};

export const QueryValidatorOutstandingRewardsRequest = {
  encode(
    message: QueryValidatorOutstandingRewardsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validator_address !== "") {
      writer.uint32(10).string(message.validator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryValidatorOutstandingRewardsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryValidatorOutstandingRewardsRequest,
    } as QueryValidatorOutstandingRewardsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryValidatorOutstandingRewardsRequest {
    const message = {
      ...baseQueryValidatorOutstandingRewardsRequest,
    } as QueryValidatorOutstandingRewardsRequest;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: QueryValidatorOutstandingRewardsRequest): unknown {
    const obj: any = {};
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryValidatorOutstandingRewardsRequest>
  ): QueryValidatorOutstandingRewardsRequest {
    const message = {
      ...baseQueryValidatorOutstandingRewardsRequest,
    } as QueryValidatorOutstandingRewardsRequest;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const baseQueryValidatorOutstandingRewardsResponse: object = {};

export const QueryValidatorOutstandingRewardsResponse = {
  encode(
    message: QueryValidatorOutstandingRewardsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.rewards !== undefined) {
      ValidatorOutstandingRewards.encode(
        message.rewards,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryValidatorOutstandingRewardsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryValidatorOutstandingRewardsResponse,
    } as QueryValidatorOutstandingRewardsResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rewards = ValidatorOutstandingRewards.decode(
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

  fromJSON(object: any): QueryValidatorOutstandingRewardsResponse {
    const message = {
      ...baseQueryValidatorOutstandingRewardsResponse,
    } as QueryValidatorOutstandingRewardsResponse;
    if (object.rewards !== undefined && object.rewards !== null) {
      message.rewards = ValidatorOutstandingRewards.fromJSON(object.rewards);
    } else {
      message.rewards = undefined;
    }
    return message;
  },

  toJSON(message: QueryValidatorOutstandingRewardsResponse): unknown {
    const obj: any = {};
    message.rewards !== undefined &&
      (obj.rewards = message.rewards
        ? ValidatorOutstandingRewards.toJSON(message.rewards)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryValidatorOutstandingRewardsResponse>
  ): QueryValidatorOutstandingRewardsResponse {
    const message = {
      ...baseQueryValidatorOutstandingRewardsResponse,
    } as QueryValidatorOutstandingRewardsResponse;
    if (object.rewards !== undefined && object.rewards !== null) {
      message.rewards = ValidatorOutstandingRewards.fromPartial(object.rewards);
    } else {
      message.rewards = undefined;
    }
    return message;
  },
};

const baseQueryValidatorCommissionRequest: object = { validator_address: "" };

export const QueryValidatorCommissionRequest = {
  encode(
    message: QueryValidatorCommissionRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validator_address !== "") {
      writer.uint32(10).string(message.validator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryValidatorCommissionRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryValidatorCommissionRequest,
    } as QueryValidatorCommissionRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryValidatorCommissionRequest {
    const message = {
      ...baseQueryValidatorCommissionRequest,
    } as QueryValidatorCommissionRequest;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: QueryValidatorCommissionRequest): unknown {
    const obj: any = {};
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryValidatorCommissionRequest>
  ): QueryValidatorCommissionRequest {
    const message = {
      ...baseQueryValidatorCommissionRequest,
    } as QueryValidatorCommissionRequest;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const baseQueryValidatorCommissionResponse: object = {};

export const QueryValidatorCommissionResponse = {
  encode(
    message: QueryValidatorCommissionResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.commission !== undefined) {
      ValidatorAccumulatedCommission.encode(
        message.commission,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryValidatorCommissionResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryValidatorCommissionResponse,
    } as QueryValidatorCommissionResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.commission = ValidatorAccumulatedCommission.decode(
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

  fromJSON(object: any): QueryValidatorCommissionResponse {
    const message = {
      ...baseQueryValidatorCommissionResponse,
    } as QueryValidatorCommissionResponse;
    if (object.commission !== undefined && object.commission !== null) {
      message.commission = ValidatorAccumulatedCommission.fromJSON(
        object.commission
      );
    } else {
      message.commission = undefined;
    }
    return message;
  },

  toJSON(message: QueryValidatorCommissionResponse): unknown {
    const obj: any = {};
    message.commission !== undefined &&
      (obj.commission = message.commission
        ? ValidatorAccumulatedCommission.toJSON(message.commission)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryValidatorCommissionResponse>
  ): QueryValidatorCommissionResponse {
    const message = {
      ...baseQueryValidatorCommissionResponse,
    } as QueryValidatorCommissionResponse;
    if (object.commission !== undefined && object.commission !== null) {
      message.commission = ValidatorAccumulatedCommission.fromPartial(
        object.commission
      );
    } else {
      message.commission = undefined;
    }
    return message;
  },
};

const baseQueryValidatorSlashesRequest: object = {
  validator_address: "",
  starting_height: 0,
  ending_height: 0,
};

export const QueryValidatorSlashesRequest = {
  encode(
    message: QueryValidatorSlashesRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validator_address !== "") {
      writer.uint32(10).string(message.validator_address);
    }
    if (message.starting_height !== 0) {
      writer.uint32(16).uint64(message.starting_height);
    }
    if (message.ending_height !== 0) {
      writer.uint32(24).uint64(message.ending_height);
    }
    if (message.pagination !== undefined) {
      PageRequest.encode(message.pagination, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryValidatorSlashesRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryValidatorSlashesRequest,
    } as QueryValidatorSlashesRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_address = reader.string();
          break;
        case 2:
          message.starting_height = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.ending_height = longToNumber(reader.uint64() as Long);
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

  fromJSON(object: any): QueryValidatorSlashesRequest {
    const message = {
      ...baseQueryValidatorSlashesRequest,
    } as QueryValidatorSlashesRequest;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    if (
      object.starting_height !== undefined &&
      object.starting_height !== null
    ) {
      message.starting_height = Number(object.starting_height);
    } else {
      message.starting_height = 0;
    }
    if (object.ending_height !== undefined && object.ending_height !== null) {
      message.ending_height = Number(object.ending_height);
    } else {
      message.ending_height = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryValidatorSlashesRequest): unknown {
    const obj: any = {};
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    message.starting_height !== undefined &&
      (obj.starting_height = message.starting_height);
    message.ending_height !== undefined &&
      (obj.ending_height = message.ending_height);
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageRequest.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryValidatorSlashesRequest>
  ): QueryValidatorSlashesRequest {
    const message = {
      ...baseQueryValidatorSlashesRequest,
    } as QueryValidatorSlashesRequest;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    if (
      object.starting_height !== undefined &&
      object.starting_height !== null
    ) {
      message.starting_height = object.starting_height;
    } else {
      message.starting_height = 0;
    }
    if (object.ending_height !== undefined && object.ending_height !== null) {
      message.ending_height = object.ending_height;
    } else {
      message.ending_height = 0;
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageRequest.fromPartial(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },
};

const baseQueryValidatorSlashesResponse: object = {};

export const QueryValidatorSlashesResponse = {
  encode(
    message: QueryValidatorSlashesResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.slashes) {
      ValidatorSlashEvent.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.pagination !== undefined) {
      PageResponse.encode(
        message.pagination,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryValidatorSlashesResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryValidatorSlashesResponse,
    } as QueryValidatorSlashesResponse;
    message.slashes = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.slashes.push(
            ValidatorSlashEvent.decode(reader, reader.uint32())
          );
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

  fromJSON(object: any): QueryValidatorSlashesResponse {
    const message = {
      ...baseQueryValidatorSlashesResponse,
    } as QueryValidatorSlashesResponse;
    message.slashes = [];
    if (object.slashes !== undefined && object.slashes !== null) {
      for (const e of object.slashes) {
        message.slashes.push(ValidatorSlashEvent.fromJSON(e));
      }
    }
    if (object.pagination !== undefined && object.pagination !== null) {
      message.pagination = PageResponse.fromJSON(object.pagination);
    } else {
      message.pagination = undefined;
    }
    return message;
  },

  toJSON(message: QueryValidatorSlashesResponse): unknown {
    const obj: any = {};
    if (message.slashes) {
      obj.slashes = message.slashes.map((e) =>
        e ? ValidatorSlashEvent.toJSON(e) : undefined
      );
    } else {
      obj.slashes = [];
    }
    message.pagination !== undefined &&
      (obj.pagination = message.pagination
        ? PageResponse.toJSON(message.pagination)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryValidatorSlashesResponse>
  ): QueryValidatorSlashesResponse {
    const message = {
      ...baseQueryValidatorSlashesResponse,
    } as QueryValidatorSlashesResponse;
    message.slashes = [];
    if (object.slashes !== undefined && object.slashes !== null) {
      for (const e of object.slashes) {
        message.slashes.push(ValidatorSlashEvent.fromPartial(e));
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

const baseQueryDelegationRewardsRequest: object = {
  delegator_address: "",
  validator_address: "",
};

export const QueryDelegationRewardsRequest = {
  encode(
    message: QueryDelegationRewardsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_address !== "") {
      writer.uint32(18).string(message.validator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegationRewardsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegationRewardsRequest,
    } as QueryDelegationRewardsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegationRewardsRequest {
    const message = {
      ...baseQueryDelegationRewardsRequest,
    } as QueryDelegationRewardsRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: QueryDelegationRewardsRequest): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegationRewardsRequest>
  ): QueryDelegationRewardsRequest {
    const message = {
      ...baseQueryDelegationRewardsRequest,
    } as QueryDelegationRewardsRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const baseQueryDelegationRewardsResponse: object = {};

export const QueryDelegationRewardsResponse = {
  encode(
    message: QueryDelegationRewardsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.rewards) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegationRewardsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegationRewardsResponse,
    } as QueryDelegationRewardsResponse;
    message.rewards = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rewards.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegationRewardsResponse {
    const message = {
      ...baseQueryDelegationRewardsResponse,
    } as QueryDelegationRewardsResponse;
    message.rewards = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryDelegationRewardsResponse): unknown {
    const obj: any = {};
    if (message.rewards) {
      obj.rewards = message.rewards.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.rewards = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegationRewardsResponse>
  ): QueryDelegationRewardsResponse {
    const message = {
      ...baseQueryDelegationRewardsResponse,
    } as QueryDelegationRewardsResponse;
    message.rewards = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryDelegationTotalRewardsRequest: object = {
  delegator_address: "",
};

export const QueryDelegationTotalRewardsRequest = {
  encode(
    message: QueryDelegationTotalRewardsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegationTotalRewardsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegationTotalRewardsRequest,
    } as QueryDelegationTotalRewardsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegationTotalRewardsRequest {
    const message = {
      ...baseQueryDelegationTotalRewardsRequest,
    } as QueryDelegationTotalRewardsRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    return message;
  },

  toJSON(message: QueryDelegationTotalRewardsRequest): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegationTotalRewardsRequest>
  ): QueryDelegationTotalRewardsRequest {
    const message = {
      ...baseQueryDelegationTotalRewardsRequest,
    } as QueryDelegationTotalRewardsRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    return message;
  },
};

const baseQueryDelegationTotalRewardsResponse: object = {};

export const QueryDelegationTotalRewardsResponse = {
  encode(
    message: QueryDelegationTotalRewardsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.rewards) {
      DelegationDelegatorReward.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.total) {
      DecCoin.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegationTotalRewardsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegationTotalRewardsResponse,
    } as QueryDelegationTotalRewardsResponse;
    message.rewards = [];
    message.total = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rewards.push(
            DelegationDelegatorReward.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.total.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegationTotalRewardsResponse {
    const message = {
      ...baseQueryDelegationTotalRewardsResponse,
    } as QueryDelegationTotalRewardsResponse;
    message.rewards = [];
    message.total = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DelegationDelegatorReward.fromJSON(e));
      }
    }
    if (object.total !== undefined && object.total !== null) {
      for (const e of object.total) {
        message.total.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryDelegationTotalRewardsResponse): unknown {
    const obj: any = {};
    if (message.rewards) {
      obj.rewards = message.rewards.map((e) =>
        e ? DelegationDelegatorReward.toJSON(e) : undefined
      );
    } else {
      obj.rewards = [];
    }
    if (message.total) {
      obj.total = message.total.map((e) => (e ? DecCoin.toJSON(e) : undefined));
    } else {
      obj.total = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegationTotalRewardsResponse>
  ): QueryDelegationTotalRewardsResponse {
    const message = {
      ...baseQueryDelegationTotalRewardsResponse,
    } as QueryDelegationTotalRewardsResponse;
    message.rewards = [];
    message.total = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DelegationDelegatorReward.fromPartial(e));
      }
    }
    if (object.total !== undefined && object.total !== null) {
      for (const e of object.total) {
        message.total.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseQueryDelegatorValidatorsRequest: object = { delegator_address: "" };

export const QueryDelegatorValidatorsRequest = {
  encode(
    message: QueryDelegatorValidatorsRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegatorValidatorsRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegatorValidatorsRequest,
    } as QueryDelegatorValidatorsRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegatorValidatorsRequest {
    const message = {
      ...baseQueryDelegatorValidatorsRequest,
    } as QueryDelegatorValidatorsRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    return message;
  },

  toJSON(message: QueryDelegatorValidatorsRequest): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegatorValidatorsRequest>
  ): QueryDelegatorValidatorsRequest {
    const message = {
      ...baseQueryDelegatorValidatorsRequest,
    } as QueryDelegatorValidatorsRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    return message;
  },
};

const baseQueryDelegatorValidatorsResponse: object = { validators: "" };

export const QueryDelegatorValidatorsResponse = {
  encode(
    message: QueryDelegatorValidatorsResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.validators) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegatorValidatorsResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegatorValidatorsResponse,
    } as QueryDelegatorValidatorsResponse;
    message.validators = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validators.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegatorValidatorsResponse {
    const message = {
      ...baseQueryDelegatorValidatorsResponse,
    } as QueryDelegatorValidatorsResponse;
    message.validators = [];
    if (object.validators !== undefined && object.validators !== null) {
      for (const e of object.validators) {
        message.validators.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: QueryDelegatorValidatorsResponse): unknown {
    const obj: any = {};
    if (message.validators) {
      obj.validators = message.validators.map((e) => e);
    } else {
      obj.validators = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegatorValidatorsResponse>
  ): QueryDelegatorValidatorsResponse {
    const message = {
      ...baseQueryDelegatorValidatorsResponse,
    } as QueryDelegatorValidatorsResponse;
    message.validators = [];
    if (object.validators !== undefined && object.validators !== null) {
      for (const e of object.validators) {
        message.validators.push(e);
      }
    }
    return message;
  },
};

const baseQueryDelegatorWithdrawAddressRequest: object = {
  delegator_address: "",
};

export const QueryDelegatorWithdrawAddressRequest = {
  encode(
    message: QueryDelegatorWithdrawAddressRequest,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegatorWithdrawAddressRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegatorWithdrawAddressRequest,
    } as QueryDelegatorWithdrawAddressRequest;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegatorWithdrawAddressRequest {
    const message = {
      ...baseQueryDelegatorWithdrawAddressRequest,
    } as QueryDelegatorWithdrawAddressRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    return message;
  },

  toJSON(message: QueryDelegatorWithdrawAddressRequest): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegatorWithdrawAddressRequest>
  ): QueryDelegatorWithdrawAddressRequest {
    const message = {
      ...baseQueryDelegatorWithdrawAddressRequest,
    } as QueryDelegatorWithdrawAddressRequest;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    return message;
  },
};

const baseQueryDelegatorWithdrawAddressResponse: object = {
  withdraw_address: "",
};

export const QueryDelegatorWithdrawAddressResponse = {
  encode(
    message: QueryDelegatorWithdrawAddressResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.withdraw_address !== "") {
      writer.uint32(10).string(message.withdraw_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryDelegatorWithdrawAddressResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryDelegatorWithdrawAddressResponse,
    } as QueryDelegatorWithdrawAddressResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.withdraw_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryDelegatorWithdrawAddressResponse {
    const message = {
      ...baseQueryDelegatorWithdrawAddressResponse,
    } as QueryDelegatorWithdrawAddressResponse;
    if (
      object.withdraw_address !== undefined &&
      object.withdraw_address !== null
    ) {
      message.withdraw_address = String(object.withdraw_address);
    } else {
      message.withdraw_address = "";
    }
    return message;
  },

  toJSON(message: QueryDelegatorWithdrawAddressResponse): unknown {
    const obj: any = {};
    message.withdraw_address !== undefined &&
      (obj.withdraw_address = message.withdraw_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryDelegatorWithdrawAddressResponse>
  ): QueryDelegatorWithdrawAddressResponse {
    const message = {
      ...baseQueryDelegatorWithdrawAddressResponse,
    } as QueryDelegatorWithdrawAddressResponse;
    if (
      object.withdraw_address !== undefined &&
      object.withdraw_address !== null
    ) {
      message.withdraw_address = object.withdraw_address;
    } else {
      message.withdraw_address = "";
    }
    return message;
  },
};

const baseQueryCommunityPoolRequest: object = {};

export const QueryCommunityPoolRequest = {
  encode(
    _: QueryCommunityPoolRequest,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryCommunityPoolRequest {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryCommunityPoolRequest,
    } as QueryCommunityPoolRequest;
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

  fromJSON(_: any): QueryCommunityPoolRequest {
    const message = {
      ...baseQueryCommunityPoolRequest,
    } as QueryCommunityPoolRequest;
    return message;
  },

  toJSON(_: QueryCommunityPoolRequest): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<QueryCommunityPoolRequest>
  ): QueryCommunityPoolRequest {
    const message = {
      ...baseQueryCommunityPoolRequest,
    } as QueryCommunityPoolRequest;
    return message;
  },
};

const baseQueryCommunityPoolResponse: object = {};

export const QueryCommunityPoolResponse = {
  encode(
    message: QueryCommunityPoolResponse,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.pool) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): QueryCommunityPoolResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseQueryCommunityPoolResponse,
    } as QueryCommunityPoolResponse;
    message.pool = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pool.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): QueryCommunityPoolResponse {
    const message = {
      ...baseQueryCommunityPoolResponse,
    } as QueryCommunityPoolResponse;
    message.pool = [];
    if (object.pool !== undefined && object.pool !== null) {
      for (const e of object.pool) {
        message.pool.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: QueryCommunityPoolResponse): unknown {
    const obj: any = {};
    if (message.pool) {
      obj.pool = message.pool.map((e) => (e ? DecCoin.toJSON(e) : undefined));
    } else {
      obj.pool = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<QueryCommunityPoolResponse>
  ): QueryCommunityPoolResponse {
    const message = {
      ...baseQueryCommunityPoolResponse,
    } as QueryCommunityPoolResponse;
    message.pool = [];
    if (object.pool !== undefined && object.pool !== null) {
      for (const e of object.pool) {
        message.pool.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

/** Query defines the gRPC querier service for distribution module. */
export interface Query {
  /** Params queries params of the distribution module. */
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
  /** ValidatorOutstandingRewards queries rewards of a validator address. */
  ValidatorOutstandingRewards(
    request: QueryValidatorOutstandingRewardsRequest
  ): Promise<QueryValidatorOutstandingRewardsResponse>;
  /** ValidatorCommission queries accumulated commission for a validator. */
  ValidatorCommission(
    request: QueryValidatorCommissionRequest
  ): Promise<QueryValidatorCommissionResponse>;
  /** ValidatorSlashes queries slash events of a validator. */
  ValidatorSlashes(
    request: QueryValidatorSlashesRequest
  ): Promise<QueryValidatorSlashesResponse>;
  /** DelegationRewards queries the total rewards accrued by a delegation. */
  DelegationRewards(
    request: QueryDelegationRewardsRequest
  ): Promise<QueryDelegationRewardsResponse>;
  /**
   * DelegationTotalRewards queries the total rewards accrued by a each
   * validator.
   */
  DelegationTotalRewards(
    request: QueryDelegationTotalRewardsRequest
  ): Promise<QueryDelegationTotalRewardsResponse>;
  /** DelegatorValidators queries the validators of a delegator. */
  DelegatorValidators(
    request: QueryDelegatorValidatorsRequest
  ): Promise<QueryDelegatorValidatorsResponse>;
  /** DelegatorWithdrawAddress queries withdraw address of a delegator. */
  DelegatorWithdrawAddress(
    request: QueryDelegatorWithdrawAddressRequest
  ): Promise<QueryDelegatorWithdrawAddressResponse>;
  /** CommunityPool queries the community pool coins. */
  CommunityPool(
    request: QueryCommunityPoolRequest
  ): Promise<QueryCommunityPoolResponse>;
}

export class QueryClientImpl implements Query {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  Params(request: QueryParamsRequest): Promise<QueryParamsResponse> {
    const data = QueryParamsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "Params",
      data
    );
    return promise.then((data) => QueryParamsResponse.decode(new Reader(data)));
  }

  ValidatorOutstandingRewards(
    request: QueryValidatorOutstandingRewardsRequest
  ): Promise<QueryValidatorOutstandingRewardsResponse> {
    const data = QueryValidatorOutstandingRewardsRequest.encode(
      request
    ).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "ValidatorOutstandingRewards",
      data
    );
    return promise.then((data) =>
      QueryValidatorOutstandingRewardsResponse.decode(new Reader(data))
    );
  }

  ValidatorCommission(
    request: QueryValidatorCommissionRequest
  ): Promise<QueryValidatorCommissionResponse> {
    const data = QueryValidatorCommissionRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "ValidatorCommission",
      data
    );
    return promise.then((data) =>
      QueryValidatorCommissionResponse.decode(new Reader(data))
    );
  }

  ValidatorSlashes(
    request: QueryValidatorSlashesRequest
  ): Promise<QueryValidatorSlashesResponse> {
    const data = QueryValidatorSlashesRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "ValidatorSlashes",
      data
    );
    return promise.then((data) =>
      QueryValidatorSlashesResponse.decode(new Reader(data))
    );
  }

  DelegationRewards(
    request: QueryDelegationRewardsRequest
  ): Promise<QueryDelegationRewardsResponse> {
    const data = QueryDelegationRewardsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "DelegationRewards",
      data
    );
    return promise.then((data) =>
      QueryDelegationRewardsResponse.decode(new Reader(data))
    );
  }

  DelegationTotalRewards(
    request: QueryDelegationTotalRewardsRequest
  ): Promise<QueryDelegationTotalRewardsResponse> {
    const data = QueryDelegationTotalRewardsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "DelegationTotalRewards",
      data
    );
    return promise.then((data) =>
      QueryDelegationTotalRewardsResponse.decode(new Reader(data))
    );
  }

  DelegatorValidators(
    request: QueryDelegatorValidatorsRequest
  ): Promise<QueryDelegatorValidatorsResponse> {
    const data = QueryDelegatorValidatorsRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "DelegatorValidators",
      data
    );
    return promise.then((data) =>
      QueryDelegatorValidatorsResponse.decode(new Reader(data))
    );
  }

  DelegatorWithdrawAddress(
    request: QueryDelegatorWithdrawAddressRequest
  ): Promise<QueryDelegatorWithdrawAddressResponse> {
    const data = QueryDelegatorWithdrawAddressRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "DelegatorWithdrawAddress",
      data
    );
    return promise.then((data) =>
      QueryDelegatorWithdrawAddressResponse.decode(new Reader(data))
    );
  }

  CommunityPool(
    request: QueryCommunityPoolRequest
  ): Promise<QueryCommunityPoolResponse> {
    const data = QueryCommunityPoolRequest.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Query",
      "CommunityPool",
      data
    );
    return promise.then((data) =>
      QueryCommunityPoolResponse.decode(new Reader(data))
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
