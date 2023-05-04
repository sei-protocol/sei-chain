import { Reader, Writer } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.oracle";
/**
 * MsgAggregateExchangeRatePrevote represents a message to submit
 * aggregate exchange rate prevote.
 */
export interface MsgAggregateExchangeRatePrevote {
    hash: string;
    feeder: string;
    validator: string;
}
/** MsgAggregateExchangeRatePrevoteResponse defines the Msg/AggregateExchangeRatePrevote response type. */
export interface MsgAggregateExchangeRatePrevoteResponse {
}
/**
 * MsgAggregateExchangeRateVote represents a message to submit
 * aggregate exchange rate vote.
 */
export interface MsgAggregateExchangeRateVote {
    salt: string;
    exchangeRates: string;
    feeder: string;
    validator: string;
}
/** MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type. */
export interface MsgAggregateExchangeRateVoteResponse {
}
/**
 * MsgAggregateExchangeRateVote represents a message to submit
 * aggregate exchange rate vote.
 */
export interface MsgAggregateExchangeRateCombinedVote {
    voteSalt: string;
    voteExchangeRates: string;
    prevoteHash: string;
    feeder: string;
    validator: string;
}
/** MsgAggregateExchangeRateVoteResponse defines the Msg/AggregateExchangeRateVote response type. */
export interface MsgAggregateExchangeRateCombinedVoteResponse {
}
/**
 * MsgDelegateFeedConsent represents a message to
 * delegate oracle voting rights to another address.
 */
export interface MsgDelegateFeedConsent {
    operator: string;
    delegate: string;
}
/** MsgDelegateFeedConsentResponse defines the Msg/DelegateFeedConsent response type. */
export interface MsgDelegateFeedConsentResponse {
}
export declare const MsgAggregateExchangeRatePrevote: {
    encode(message: MsgAggregateExchangeRatePrevote, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgAggregateExchangeRatePrevote;
    fromJSON(object: any): MsgAggregateExchangeRatePrevote;
    toJSON(message: MsgAggregateExchangeRatePrevote): unknown;
    fromPartial(object: DeepPartial<MsgAggregateExchangeRatePrevote>): MsgAggregateExchangeRatePrevote;
};
export declare const MsgAggregateExchangeRatePrevoteResponse: {
    encode(_: MsgAggregateExchangeRatePrevoteResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgAggregateExchangeRatePrevoteResponse;
    fromJSON(_: any): MsgAggregateExchangeRatePrevoteResponse;
    toJSON(_: MsgAggregateExchangeRatePrevoteResponse): unknown;
    fromPartial(_: DeepPartial<MsgAggregateExchangeRatePrevoteResponse>): MsgAggregateExchangeRatePrevoteResponse;
};
export declare const MsgAggregateExchangeRateVote: {
    encode(message: MsgAggregateExchangeRateVote, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgAggregateExchangeRateVote;
    fromJSON(object: any): MsgAggregateExchangeRateVote;
    toJSON(message: MsgAggregateExchangeRateVote): unknown;
    fromPartial(object: DeepPartial<MsgAggregateExchangeRateVote>): MsgAggregateExchangeRateVote;
};
export declare const MsgAggregateExchangeRateVoteResponse: {
    encode(_: MsgAggregateExchangeRateVoteResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgAggregateExchangeRateVoteResponse;
    fromJSON(_: any): MsgAggregateExchangeRateVoteResponse;
    toJSON(_: MsgAggregateExchangeRateVoteResponse): unknown;
    fromPartial(_: DeepPartial<MsgAggregateExchangeRateVoteResponse>): MsgAggregateExchangeRateVoteResponse;
};
export declare const MsgAggregateExchangeRateCombinedVote: {
    encode(message: MsgAggregateExchangeRateCombinedVote, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgAggregateExchangeRateCombinedVote;
    fromJSON(object: any): MsgAggregateExchangeRateCombinedVote;
    toJSON(message: MsgAggregateExchangeRateCombinedVote): unknown;
    fromPartial(object: DeepPartial<MsgAggregateExchangeRateCombinedVote>): MsgAggregateExchangeRateCombinedVote;
};
export declare const MsgAggregateExchangeRateCombinedVoteResponse: {
    encode(_: MsgAggregateExchangeRateCombinedVoteResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgAggregateExchangeRateCombinedVoteResponse;
    fromJSON(_: any): MsgAggregateExchangeRateCombinedVoteResponse;
    toJSON(_: MsgAggregateExchangeRateCombinedVoteResponse): unknown;
    fromPartial(_: DeepPartial<MsgAggregateExchangeRateCombinedVoteResponse>): MsgAggregateExchangeRateCombinedVoteResponse;
};
export declare const MsgDelegateFeedConsent: {
    encode(message: MsgDelegateFeedConsent, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgDelegateFeedConsent;
    fromJSON(object: any): MsgDelegateFeedConsent;
    toJSON(message: MsgDelegateFeedConsent): unknown;
    fromPartial(object: DeepPartial<MsgDelegateFeedConsent>): MsgDelegateFeedConsent;
};
export declare const MsgDelegateFeedConsentResponse: {
    encode(_: MsgDelegateFeedConsentResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): MsgDelegateFeedConsentResponse;
    fromJSON(_: any): MsgDelegateFeedConsentResponse;
    toJSON(_: MsgDelegateFeedConsentResponse): unknown;
    fromPartial(_: DeepPartial<MsgDelegateFeedConsentResponse>): MsgDelegateFeedConsentResponse;
};
/** Msg defines the oracle Msg service. */
export interface Msg {
    /**
     * AggregateExchangeRatePrevote defines a method for submitting
     * aggregate exchange rate prevote
     */
    AggregateExchangeRatePrevote(request: MsgAggregateExchangeRatePrevote): Promise<MsgAggregateExchangeRatePrevoteResponse>;
    /**
     * AggregateExchangeRateVote defines a method for submitting
     * aggregate exchange rate vote
     */
    AggregateExchangeRateVote(request: MsgAggregateExchangeRateVote): Promise<MsgAggregateExchangeRateVoteResponse>;
    /** Aggregate vote and prevote combines the functionality of prevote and vote into one RPC */
    AggregateExchangeRateCombinedVote(request: MsgAggregateExchangeRateCombinedVote): Promise<MsgAggregateExchangeRateCombinedVoteResponse>;
    /** DelegateFeedConsent defines a method for setting the feeder delegation */
    DelegateFeedConsent(request: MsgDelegateFeedConsent): Promise<MsgDelegateFeedConsentResponse>;
}
export declare class MsgClientImpl implements Msg {
    private readonly rpc;
    constructor(rpc: Rpc);
    AggregateExchangeRatePrevote(request: MsgAggregateExchangeRatePrevote): Promise<MsgAggregateExchangeRatePrevoteResponse>;
    AggregateExchangeRateVote(request: MsgAggregateExchangeRateVote): Promise<MsgAggregateExchangeRateVoteResponse>;
    AggregateExchangeRateCombinedVote(request: MsgAggregateExchangeRateCombinedVote): Promise<MsgAggregateExchangeRateCombinedVoteResponse>;
    DelegateFeedConsent(request: MsgDelegateFeedConsent): Promise<MsgDelegateFeedConsentResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
