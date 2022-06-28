import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.oracle";
export interface Params {
    votePeriod: number;
    voteThreshold: string;
    rewardBand: string;
    whitelist: Denom[];
    slashFraction: string;
    slashWindow: number;
    minValidPerWindow: string;
    lookbackDuration: number;
}
export interface Denom {
    name: string;
}
export interface AggregateExchangeRatePrevote {
    hash: string;
    voter: string;
    submitBlock: number;
}
export interface AggregateExchangeRateVote {
    exchangeRateTuples: ExchangeRateTuple[];
    voter: string;
}
export interface ExchangeRateTuple {
    denom: string;
    exchangeRate: string;
}
export interface OracleExchangeRate {
    exchangeRate: string;
    lastUpdate: string;
}
export interface PriceSnapshotItem {
    denom: string;
    oracleExchangeRate: OracleExchangeRate | undefined;
}
export interface PriceSnapshot {
    snapshotTimestamp: number;
    priceSnapshotItems: PriceSnapshotItem[];
}
export interface OracleTwap {
    denom: string;
    twap: string;
    lookbackSeconds: number;
}
export interface VotePenaltyCounter {
    missCount: number;
    abstainCount: number;
}
export declare const Params: {
    encode(message: Params, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Params;
    fromJSON(object: any): Params;
    toJSON(message: Params): unknown;
    fromPartial(object: DeepPartial<Params>): Params;
};
export declare const Denom: {
    encode(message: Denom, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Denom;
    fromJSON(object: any): Denom;
    toJSON(message: Denom): unknown;
    fromPartial(object: DeepPartial<Denom>): Denom;
};
export declare const AggregateExchangeRatePrevote: {
    encode(message: AggregateExchangeRatePrevote, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): AggregateExchangeRatePrevote;
    fromJSON(object: any): AggregateExchangeRatePrevote;
    toJSON(message: AggregateExchangeRatePrevote): unknown;
    fromPartial(object: DeepPartial<AggregateExchangeRatePrevote>): AggregateExchangeRatePrevote;
};
export declare const AggregateExchangeRateVote: {
    encode(message: AggregateExchangeRateVote, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): AggregateExchangeRateVote;
    fromJSON(object: any): AggregateExchangeRateVote;
    toJSON(message: AggregateExchangeRateVote): unknown;
    fromPartial(object: DeepPartial<AggregateExchangeRateVote>): AggregateExchangeRateVote;
};
export declare const ExchangeRateTuple: {
    encode(message: ExchangeRateTuple, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ExchangeRateTuple;
    fromJSON(object: any): ExchangeRateTuple;
    toJSON(message: ExchangeRateTuple): unknown;
    fromPartial(object: DeepPartial<ExchangeRateTuple>): ExchangeRateTuple;
};
export declare const OracleExchangeRate: {
    encode(message: OracleExchangeRate, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): OracleExchangeRate;
    fromJSON(object: any): OracleExchangeRate;
    toJSON(message: OracleExchangeRate): unknown;
    fromPartial(object: DeepPartial<OracleExchangeRate>): OracleExchangeRate;
};
export declare const PriceSnapshotItem: {
    encode(message: PriceSnapshotItem, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PriceSnapshotItem;
    fromJSON(object: any): PriceSnapshotItem;
    toJSON(message: PriceSnapshotItem): unknown;
    fromPartial(object: DeepPartial<PriceSnapshotItem>): PriceSnapshotItem;
};
export declare const PriceSnapshot: {
    encode(message: PriceSnapshot, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PriceSnapshot;
    fromJSON(object: any): PriceSnapshot;
    toJSON(message: PriceSnapshot): unknown;
    fromPartial(object: DeepPartial<PriceSnapshot>): PriceSnapshot;
};
export declare const OracleTwap: {
    encode(message: OracleTwap, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): OracleTwap;
    fromJSON(object: any): OracleTwap;
    toJSON(message: OracleTwap): unknown;
    fromPartial(object: DeepPartial<OracleTwap>): OracleTwap;
};
export declare const VotePenaltyCounter: {
    encode(message: VotePenaltyCounter, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): VotePenaltyCounter;
    fromJSON(object: any): VotePenaltyCounter;
    toJSON(message: VotePenaltyCounter): unknown;
    fromPartial(object: DeepPartial<VotePenaltyCounter>): VotePenaltyCounter;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
