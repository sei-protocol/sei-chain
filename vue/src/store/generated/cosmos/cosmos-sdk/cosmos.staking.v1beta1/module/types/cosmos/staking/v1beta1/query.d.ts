import { Reader, Writer } from "protobufjs/minimal";
import { PageRequest, PageResponse } from "../../../cosmos/base/query/v1beta1/pagination";
import { Validator, DelegationResponse, UnbondingDelegation, RedelegationResponse, HistoricalInfo, Pool, Params } from "../../../cosmos/staking/v1beta1/staking";
export declare const protobufPackage = "cosmos.staking.v1beta1";
/** QueryValidatorsRequest is request type for Query/Validators RPC method. */
export interface QueryValidatorsRequest {
    /** status enables to query for validators matching a given status. */
    status: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/** QueryValidatorsResponse is response type for the Query/Validators RPC method */
export interface QueryValidatorsResponse {
    /** validators contains all the queried validators. */
    validators: Validator[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/** QueryValidatorRequest is response type for the Query/Validator RPC method */
export interface QueryValidatorRequest {
    /** validator_addr defines the validator address to query for. */
    validatorAddr: string;
}
/** QueryValidatorResponse is response type for the Query/Validator RPC method */
export interface QueryValidatorResponse {
    /** validator defines the the validator info. */
    validator: Validator | undefined;
}
/**
 * QueryValidatorDelegationsRequest is request type for the
 * Query/ValidatorDelegations RPC method
 */
export interface QueryValidatorDelegationsRequest {
    /** validator_addr defines the validator address to query for. */
    validatorAddr: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryValidatorDelegationsResponse is response type for the
 * Query/ValidatorDelegations RPC method
 */
export interface QueryValidatorDelegationsResponse {
    delegationResponses: DelegationResponse[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/**
 * QueryValidatorUnbondingDelegationsRequest is required type for the
 * Query/ValidatorUnbondingDelegations RPC method
 */
export interface QueryValidatorUnbondingDelegationsRequest {
    /** validator_addr defines the validator address to query for. */
    validatorAddr: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryValidatorUnbondingDelegationsResponse is response type for the
 * Query/ValidatorUnbondingDelegations RPC method.
 */
export interface QueryValidatorUnbondingDelegationsResponse {
    unbondingResponses: UnbondingDelegation[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/** QueryDelegationRequest is request type for the Query/Delegation RPC method. */
export interface QueryDelegationRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** validator_addr defines the validator address to query for. */
    validatorAddr: string;
}
/** QueryDelegationResponse is response type for the Query/Delegation RPC method. */
export interface QueryDelegationResponse {
    /** delegation_responses defines the delegation info of a delegation. */
    delegationResponse: DelegationResponse | undefined;
}
/**
 * QueryUnbondingDelegationRequest is request type for the
 * Query/UnbondingDelegation RPC method.
 */
export interface QueryUnbondingDelegationRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** validator_addr defines the validator address to query for. */
    validatorAddr: string;
}
/**
 * QueryDelegationResponse is response type for the Query/UnbondingDelegation
 * RPC method.
 */
export interface QueryUnbondingDelegationResponse {
    /** unbond defines the unbonding information of a delegation. */
    unbond: UnbondingDelegation | undefined;
}
/**
 * QueryDelegatorDelegationsRequest is request type for the
 * Query/DelegatorDelegations RPC method.
 */
export interface QueryDelegatorDelegationsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryDelegatorDelegationsResponse is response type for the
 * Query/DelegatorDelegations RPC method.
 */
export interface QueryDelegatorDelegationsResponse {
    /** delegation_responses defines all the delegations' info of a delegator. */
    delegationResponses: DelegationResponse[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/**
 * QueryDelegatorUnbondingDelegationsRequest is request type for the
 * Query/DelegatorUnbondingDelegations RPC method.
 */
export interface QueryDelegatorUnbondingDelegationsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryUnbondingDelegatorDelegationsResponse is response type for the
 * Query/UnbondingDelegatorDelegations RPC method.
 */
export interface QueryDelegatorUnbondingDelegationsResponse {
    unbondingResponses: UnbondingDelegation[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/**
 * QueryRedelegationsRequest is request type for the Query/Redelegations RPC
 * method.
 */
export interface QueryRedelegationsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** src_validator_addr defines the validator address to redelegate from. */
    srcValidatorAddr: string;
    /** dst_validator_addr defines the validator address to redelegate to. */
    dstValidatorAddr: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryRedelegationsResponse is response type for the Query/Redelegations RPC
 * method.
 */
export interface QueryRedelegationsResponse {
    redelegationResponses: RedelegationResponse[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/**
 * QueryDelegatorValidatorsRequest is request type for the
 * Query/DelegatorValidators RPC method.
 */
export interface QueryDelegatorValidatorsRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** pagination defines an optional pagination for the request. */
    pagination: PageRequest | undefined;
}
/**
 * QueryDelegatorValidatorsResponse is response type for the
 * Query/DelegatorValidators RPC method.
 */
export interface QueryDelegatorValidatorsResponse {
    /** validators defines the the validators' info of a delegator. */
    validators: Validator[];
    /** pagination defines the pagination in the response. */
    pagination: PageResponse | undefined;
}
/**
 * QueryDelegatorValidatorRequest is request type for the
 * Query/DelegatorValidator RPC method.
 */
export interface QueryDelegatorValidatorRequest {
    /** delegator_addr defines the delegator address to query for. */
    delegatorAddr: string;
    /** validator_addr defines the validator address to query for. */
    validatorAddr: string;
}
/**
 * QueryDelegatorValidatorResponse response type for the
 * Query/DelegatorValidator RPC method.
 */
export interface QueryDelegatorValidatorResponse {
    /** validator defines the the validator info. */
    validator: Validator | undefined;
}
/**
 * QueryHistoricalInfoRequest is request type for the Query/HistoricalInfo RPC
 * method.
 */
export interface QueryHistoricalInfoRequest {
    /** height defines at which height to query the historical info. */
    height: number;
}
/**
 * QueryHistoricalInfoResponse is response type for the Query/HistoricalInfo RPC
 * method.
 */
export interface QueryHistoricalInfoResponse {
    /** hist defines the historical info at the given height. */
    hist: HistoricalInfo | undefined;
}
/** QueryPoolRequest is request type for the Query/Pool RPC method. */
export interface QueryPoolRequest {
}
/** QueryPoolResponse is response type for the Query/Pool RPC method. */
export interface QueryPoolResponse {
    /** pool defines the pool info. */
    pool: Pool | undefined;
}
/** QueryParamsRequest is request type for the Query/Params RPC method. */
export interface QueryParamsRequest {
}
/** QueryParamsResponse is response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
    /** params holds all the parameters of this module. */
    params: Params | undefined;
}
export declare const QueryValidatorsRequest: {
    encode(message: QueryValidatorsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorsRequest;
    fromJSON(object: any): QueryValidatorsRequest;
    toJSON(message: QueryValidatorsRequest): unknown;
    fromPartial(object: DeepPartial<QueryValidatorsRequest>): QueryValidatorsRequest;
};
export declare const QueryValidatorsResponse: {
    encode(message: QueryValidatorsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorsResponse;
    fromJSON(object: any): QueryValidatorsResponse;
    toJSON(message: QueryValidatorsResponse): unknown;
    fromPartial(object: DeepPartial<QueryValidatorsResponse>): QueryValidatorsResponse;
};
export declare const QueryValidatorRequest: {
    encode(message: QueryValidatorRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorRequest;
    fromJSON(object: any): QueryValidatorRequest;
    toJSON(message: QueryValidatorRequest): unknown;
    fromPartial(object: DeepPartial<QueryValidatorRequest>): QueryValidatorRequest;
};
export declare const QueryValidatorResponse: {
    encode(message: QueryValidatorResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorResponse;
    fromJSON(object: any): QueryValidatorResponse;
    toJSON(message: QueryValidatorResponse): unknown;
    fromPartial(object: DeepPartial<QueryValidatorResponse>): QueryValidatorResponse;
};
export declare const QueryValidatorDelegationsRequest: {
    encode(message: QueryValidatorDelegationsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorDelegationsRequest;
    fromJSON(object: any): QueryValidatorDelegationsRequest;
    toJSON(message: QueryValidatorDelegationsRequest): unknown;
    fromPartial(object: DeepPartial<QueryValidatorDelegationsRequest>): QueryValidatorDelegationsRequest;
};
export declare const QueryValidatorDelegationsResponse: {
    encode(message: QueryValidatorDelegationsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorDelegationsResponse;
    fromJSON(object: any): QueryValidatorDelegationsResponse;
    toJSON(message: QueryValidatorDelegationsResponse): unknown;
    fromPartial(object: DeepPartial<QueryValidatorDelegationsResponse>): QueryValidatorDelegationsResponse;
};
export declare const QueryValidatorUnbondingDelegationsRequest: {
    encode(message: QueryValidatorUnbondingDelegationsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorUnbondingDelegationsRequest;
    fromJSON(object: any): QueryValidatorUnbondingDelegationsRequest;
    toJSON(message: QueryValidatorUnbondingDelegationsRequest): unknown;
    fromPartial(object: DeepPartial<QueryValidatorUnbondingDelegationsRequest>): QueryValidatorUnbondingDelegationsRequest;
};
export declare const QueryValidatorUnbondingDelegationsResponse: {
    encode(message: QueryValidatorUnbondingDelegationsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryValidatorUnbondingDelegationsResponse;
    fromJSON(object: any): QueryValidatorUnbondingDelegationsResponse;
    toJSON(message: QueryValidatorUnbondingDelegationsResponse): unknown;
    fromPartial(object: DeepPartial<QueryValidatorUnbondingDelegationsResponse>): QueryValidatorUnbondingDelegationsResponse;
};
export declare const QueryDelegationRequest: {
    encode(message: QueryDelegationRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegationRequest;
    fromJSON(object: any): QueryDelegationRequest;
    toJSON(message: QueryDelegationRequest): unknown;
    fromPartial(object: DeepPartial<QueryDelegationRequest>): QueryDelegationRequest;
};
export declare const QueryDelegationResponse: {
    encode(message: QueryDelegationResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegationResponse;
    fromJSON(object: any): QueryDelegationResponse;
    toJSON(message: QueryDelegationResponse): unknown;
    fromPartial(object: DeepPartial<QueryDelegationResponse>): QueryDelegationResponse;
};
export declare const QueryUnbondingDelegationRequest: {
    encode(message: QueryUnbondingDelegationRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryUnbondingDelegationRequest;
    fromJSON(object: any): QueryUnbondingDelegationRequest;
    toJSON(message: QueryUnbondingDelegationRequest): unknown;
    fromPartial(object: DeepPartial<QueryUnbondingDelegationRequest>): QueryUnbondingDelegationRequest;
};
export declare const QueryUnbondingDelegationResponse: {
    encode(message: QueryUnbondingDelegationResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryUnbondingDelegationResponse;
    fromJSON(object: any): QueryUnbondingDelegationResponse;
    toJSON(message: QueryUnbondingDelegationResponse): unknown;
    fromPartial(object: DeepPartial<QueryUnbondingDelegationResponse>): QueryUnbondingDelegationResponse;
};
export declare const QueryDelegatorDelegationsRequest: {
    encode(message: QueryDelegatorDelegationsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorDelegationsRequest;
    fromJSON(object: any): QueryDelegatorDelegationsRequest;
    toJSON(message: QueryDelegatorDelegationsRequest): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorDelegationsRequest>): QueryDelegatorDelegationsRequest;
};
export declare const QueryDelegatorDelegationsResponse: {
    encode(message: QueryDelegatorDelegationsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorDelegationsResponse;
    fromJSON(object: any): QueryDelegatorDelegationsResponse;
    toJSON(message: QueryDelegatorDelegationsResponse): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorDelegationsResponse>): QueryDelegatorDelegationsResponse;
};
export declare const QueryDelegatorUnbondingDelegationsRequest: {
    encode(message: QueryDelegatorUnbondingDelegationsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorUnbondingDelegationsRequest;
    fromJSON(object: any): QueryDelegatorUnbondingDelegationsRequest;
    toJSON(message: QueryDelegatorUnbondingDelegationsRequest): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorUnbondingDelegationsRequest>): QueryDelegatorUnbondingDelegationsRequest;
};
export declare const QueryDelegatorUnbondingDelegationsResponse: {
    encode(message: QueryDelegatorUnbondingDelegationsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorUnbondingDelegationsResponse;
    fromJSON(object: any): QueryDelegatorUnbondingDelegationsResponse;
    toJSON(message: QueryDelegatorUnbondingDelegationsResponse): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorUnbondingDelegationsResponse>): QueryDelegatorUnbondingDelegationsResponse;
};
export declare const QueryRedelegationsRequest: {
    encode(message: QueryRedelegationsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryRedelegationsRequest;
    fromJSON(object: any): QueryRedelegationsRequest;
    toJSON(message: QueryRedelegationsRequest): unknown;
    fromPartial(object: DeepPartial<QueryRedelegationsRequest>): QueryRedelegationsRequest;
};
export declare const QueryRedelegationsResponse: {
    encode(message: QueryRedelegationsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryRedelegationsResponse;
    fromJSON(object: any): QueryRedelegationsResponse;
    toJSON(message: QueryRedelegationsResponse): unknown;
    fromPartial(object: DeepPartial<QueryRedelegationsResponse>): QueryRedelegationsResponse;
};
export declare const QueryDelegatorValidatorsRequest: {
    encode(message: QueryDelegatorValidatorsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorValidatorsRequest;
    fromJSON(object: any): QueryDelegatorValidatorsRequest;
    toJSON(message: QueryDelegatorValidatorsRequest): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorValidatorsRequest>): QueryDelegatorValidatorsRequest;
};
export declare const QueryDelegatorValidatorsResponse: {
    encode(message: QueryDelegatorValidatorsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorValidatorsResponse;
    fromJSON(object: any): QueryDelegatorValidatorsResponse;
    toJSON(message: QueryDelegatorValidatorsResponse): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorValidatorsResponse>): QueryDelegatorValidatorsResponse;
};
export declare const QueryDelegatorValidatorRequest: {
    encode(message: QueryDelegatorValidatorRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorValidatorRequest;
    fromJSON(object: any): QueryDelegatorValidatorRequest;
    toJSON(message: QueryDelegatorValidatorRequest): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorValidatorRequest>): QueryDelegatorValidatorRequest;
};
export declare const QueryDelegatorValidatorResponse: {
    encode(message: QueryDelegatorValidatorResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryDelegatorValidatorResponse;
    fromJSON(object: any): QueryDelegatorValidatorResponse;
    toJSON(message: QueryDelegatorValidatorResponse): unknown;
    fromPartial(object: DeepPartial<QueryDelegatorValidatorResponse>): QueryDelegatorValidatorResponse;
};
export declare const QueryHistoricalInfoRequest: {
    encode(message: QueryHistoricalInfoRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryHistoricalInfoRequest;
    fromJSON(object: any): QueryHistoricalInfoRequest;
    toJSON(message: QueryHistoricalInfoRequest): unknown;
    fromPartial(object: DeepPartial<QueryHistoricalInfoRequest>): QueryHistoricalInfoRequest;
};
export declare const QueryHistoricalInfoResponse: {
    encode(message: QueryHistoricalInfoResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryHistoricalInfoResponse;
    fromJSON(object: any): QueryHistoricalInfoResponse;
    toJSON(message: QueryHistoricalInfoResponse): unknown;
    fromPartial(object: DeepPartial<QueryHistoricalInfoResponse>): QueryHistoricalInfoResponse;
};
export declare const QueryPoolRequest: {
    encode(_: QueryPoolRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryPoolRequest;
    fromJSON(_: any): QueryPoolRequest;
    toJSON(_: QueryPoolRequest): unknown;
    fromPartial(_: DeepPartial<QueryPoolRequest>): QueryPoolRequest;
};
export declare const QueryPoolResponse: {
    encode(message: QueryPoolResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryPoolResponse;
    fromJSON(object: any): QueryPoolResponse;
    toJSON(message: QueryPoolResponse): unknown;
    fromPartial(object: DeepPartial<QueryPoolResponse>): QueryPoolResponse;
};
export declare const QueryParamsRequest: {
    encode(_: QueryParamsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryParamsRequest;
    fromJSON(_: any): QueryParamsRequest;
    toJSON(_: QueryParamsRequest): unknown;
    fromPartial(_: DeepPartial<QueryParamsRequest>): QueryParamsRequest;
};
export declare const QueryParamsResponse: {
    encode(message: QueryParamsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryParamsResponse;
    fromJSON(object: any): QueryParamsResponse;
    toJSON(message: QueryParamsResponse): unknown;
    fromPartial(object: DeepPartial<QueryParamsResponse>): QueryParamsResponse;
};
/** Query defines the gRPC querier service. */
export interface Query {
    /** Validators queries all validators that match the given status. */
    Validators(request: QueryValidatorsRequest): Promise<QueryValidatorsResponse>;
    /** Validator queries validator info for given validator address. */
    Validator(request: QueryValidatorRequest): Promise<QueryValidatorResponse>;
    /** ValidatorDelegations queries delegate info for given validator. */
    ValidatorDelegations(request: QueryValidatorDelegationsRequest): Promise<QueryValidatorDelegationsResponse>;
    /** ValidatorUnbondingDelegations queries unbonding delegations of a validator. */
    ValidatorUnbondingDelegations(request: QueryValidatorUnbondingDelegationsRequest): Promise<QueryValidatorUnbondingDelegationsResponse>;
    /** Delegation queries delegate info for given validator delegator pair. */
    Delegation(request: QueryDelegationRequest): Promise<QueryDelegationResponse>;
    /**
     * UnbondingDelegation queries unbonding info for given validator delegator
     * pair.
     */
    UnbondingDelegation(request: QueryUnbondingDelegationRequest): Promise<QueryUnbondingDelegationResponse>;
    /** DelegatorDelegations queries all delegations of a given delegator address. */
    DelegatorDelegations(request: QueryDelegatorDelegationsRequest): Promise<QueryDelegatorDelegationsResponse>;
    /**
     * DelegatorUnbondingDelegations queries all unbonding delegations of a given
     * delegator address.
     */
    DelegatorUnbondingDelegations(request: QueryDelegatorUnbondingDelegationsRequest): Promise<QueryDelegatorUnbondingDelegationsResponse>;
    /** Redelegations queries redelegations of given address. */
    Redelegations(request: QueryRedelegationsRequest): Promise<QueryRedelegationsResponse>;
    /**
     * DelegatorValidators queries all validators info for given delegator
     * address.
     */
    DelegatorValidators(request: QueryDelegatorValidatorsRequest): Promise<QueryDelegatorValidatorsResponse>;
    /**
     * DelegatorValidator queries validator info for given delegator validator
     * pair.
     */
    DelegatorValidator(request: QueryDelegatorValidatorRequest): Promise<QueryDelegatorValidatorResponse>;
    /** HistoricalInfo queries the historical info for given height. */
    HistoricalInfo(request: QueryHistoricalInfoRequest): Promise<QueryHistoricalInfoResponse>;
    /** Pool queries the pool info. */
    Pool(request: QueryPoolRequest): Promise<QueryPoolResponse>;
    /** Parameters queries the staking parameters. */
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Validators(request: QueryValidatorsRequest): Promise<QueryValidatorsResponse>;
    Validator(request: QueryValidatorRequest): Promise<QueryValidatorResponse>;
    ValidatorDelegations(request: QueryValidatorDelegationsRequest): Promise<QueryValidatorDelegationsResponse>;
    ValidatorUnbondingDelegations(request: QueryValidatorUnbondingDelegationsRequest): Promise<QueryValidatorUnbondingDelegationsResponse>;
    Delegation(request: QueryDelegationRequest): Promise<QueryDelegationResponse>;
    UnbondingDelegation(request: QueryUnbondingDelegationRequest): Promise<QueryUnbondingDelegationResponse>;
    DelegatorDelegations(request: QueryDelegatorDelegationsRequest): Promise<QueryDelegatorDelegationsResponse>;
    DelegatorUnbondingDelegations(request: QueryDelegatorUnbondingDelegationsRequest): Promise<QueryDelegatorUnbondingDelegationsResponse>;
    Redelegations(request: QueryRedelegationsRequest): Promise<QueryRedelegationsResponse>;
    DelegatorValidators(request: QueryDelegatorValidatorsRequest): Promise<QueryDelegatorValidatorsResponse>;
    DelegatorValidator(request: QueryDelegatorValidatorRequest): Promise<QueryDelegatorValidatorResponse>;
    HistoricalInfo(request: QueryHistoricalInfoRequest): Promise<QueryHistoricalInfoResponse>;
    Pool(request: QueryPoolRequest): Promise<QueryPoolResponse>;
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
