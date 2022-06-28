import { Params, ExchangeRateTuple, AggregateExchangeRatePrevote, AggregateExchangeRateVote, VotePenaltyCounter } from "../oracle/oracle";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.oracle";
export interface GenesisState {
    params: Params | undefined;
    feederDelegations: FeederDelegation[];
    exchangeRates: ExchangeRateTuple[];
    penaltyCounters: PenaltyCounter[];
    aggregateExchangeRatePrevotes: AggregateExchangeRatePrevote[];
    aggregateExchangeRateVotes: AggregateExchangeRateVote[];
}
export interface FeederDelegation {
    feederAddress: string;
    validatorAddress: string;
}
export interface PenaltyCounter {
    validatorAddress: string;
    votePenaltyCounter: VotePenaltyCounter | undefined;
}
export declare const GenesisState: {
    encode(message: GenesisState, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): GenesisState;
    fromJSON(object: any): GenesisState;
    toJSON(message: GenesisState): unknown;
    fromPartial(object: DeepPartial<GenesisState>): GenesisState;
};
export declare const FeederDelegation: {
    encode(message: FeederDelegation, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): FeederDelegation;
    fromJSON(object: any): FeederDelegation;
    toJSON(message: FeederDelegation): unknown;
    fromPartial(object: DeepPartial<FeederDelegation>): FeederDelegation;
};
export declare const PenaltyCounter: {
    encode(message: PenaltyCounter, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PenaltyCounter;
    fromJSON(object: any): PenaltyCounter;
    toJSON(message: PenaltyCounter): unknown;
    fromPartial(object: DeepPartial<PenaltyCounter>): PenaltyCounter;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
