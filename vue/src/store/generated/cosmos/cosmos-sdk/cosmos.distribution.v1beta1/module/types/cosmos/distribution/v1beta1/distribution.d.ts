import { Writer, Reader } from "protobufjs/minimal";
import { DecCoin, Coin } from "../../../cosmos/base/v1beta1/coin";
export declare const protobufPackage = "cosmos.distribution.v1beta1";
/** Params defines the set of params for the distribution module. */
export interface Params {
    communityTax: string;
    baseProposerReward: string;
    bonusProposerReward: string;
    withdrawAddrEnabled: boolean;
}
/**
 * ValidatorHistoricalRewards represents historical rewards for a validator.
 * Height is implicit within the store key.
 * Cumulative reward ratio is the sum from the zeroeth period
 * until this period of rewards / tokens, per the spec.
 * The reference count indicates the number of objects
 * which might need to reference this historical entry at any point.
 * ReferenceCount =
 *    number of outstanding delegations which ended the associated period (and
 *    might need to read that record)
 *  + number of slashes which ended the associated period (and might need to
 *  read that record)
 *  + one per validator for the zeroeth period, set on initialization
 */
export interface ValidatorHistoricalRewards {
    cumulativeRewardRatio: DecCoin[];
    referenceCount: number;
}
/**
 * ValidatorCurrentRewards represents current rewards and current
 * period for a validator kept as a running counter and incremented
 * each block as long as the validator's tokens remain constant.
 */
export interface ValidatorCurrentRewards {
    rewards: DecCoin[];
    period: number;
}
/**
 * ValidatorAccumulatedCommission represents accumulated commission
 * for a validator kept as a running counter, can be withdrawn at any time.
 */
export interface ValidatorAccumulatedCommission {
    commission: DecCoin[];
}
/**
 * ValidatorOutstandingRewards represents outstanding (un-withdrawn) rewards
 * for a validator inexpensive to track, allows simple sanity checks.
 */
export interface ValidatorOutstandingRewards {
    rewards: DecCoin[];
}
/**
 * ValidatorSlashEvent represents a validator slash event.
 * Height is implicit within the store key.
 * This is needed to calculate appropriate amount of staking tokens
 * for delegations which are withdrawn after a slash has occurred.
 */
export interface ValidatorSlashEvent {
    validatorPeriod: number;
    fraction: string;
}
/** ValidatorSlashEvents is a collection of ValidatorSlashEvent messages. */
export interface ValidatorSlashEvents {
    validatorSlashEvents: ValidatorSlashEvent[];
}
/** FeePool is the global fee pool for distribution. */
export interface FeePool {
    communityPool: DecCoin[];
}
/**
 * CommunityPoolSpendProposal details a proposal for use of community funds,
 * together with how many coins are proposed to be spent, and to which
 * recipient account.
 */
export interface CommunityPoolSpendProposal {
    title: string;
    description: string;
    recipient: string;
    amount: Coin[];
}
/**
 * DelegatorStartingInfo represents the starting info for a delegator reward
 * period. It tracks the previous validator period, the delegation's amount of
 * staking token, and the creation height (to check later on if any slashes have
 * occurred). NOTE: Even though validators are slashed to whole staking tokens,
 * the delegators within the validator may be left with less than a full token,
 * thus sdk.Dec is used.
 */
export interface DelegatorStartingInfo {
    previousPeriod: number;
    stake: string;
    height: number;
}
/**
 * DelegationDelegatorReward represents the properties
 * of a delegator's delegation reward.
 */
export interface DelegationDelegatorReward {
    validatorAddress: string;
    reward: DecCoin[];
}
/**
 * CommunityPoolSpendProposalWithDeposit defines a CommunityPoolSpendProposal
 * with a deposit
 */
export interface CommunityPoolSpendProposalWithDeposit {
    title: string;
    description: string;
    recipient: string;
    amount: string;
    deposit: string;
}
export declare const Params: {
    encode(message: Params, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Params;
    fromJSON(object: any): Params;
    toJSON(message: Params): unknown;
    fromPartial(object: DeepPartial<Params>): Params;
};
export declare const ValidatorHistoricalRewards: {
    encode(message: ValidatorHistoricalRewards, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValidatorHistoricalRewards;
    fromJSON(object: any): ValidatorHistoricalRewards;
    toJSON(message: ValidatorHistoricalRewards): unknown;
    fromPartial(object: DeepPartial<ValidatorHistoricalRewards>): ValidatorHistoricalRewards;
};
export declare const ValidatorCurrentRewards: {
    encode(message: ValidatorCurrentRewards, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValidatorCurrentRewards;
    fromJSON(object: any): ValidatorCurrentRewards;
    toJSON(message: ValidatorCurrentRewards): unknown;
    fromPartial(object: DeepPartial<ValidatorCurrentRewards>): ValidatorCurrentRewards;
};
export declare const ValidatorAccumulatedCommission: {
    encode(message: ValidatorAccumulatedCommission, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValidatorAccumulatedCommission;
    fromJSON(object: any): ValidatorAccumulatedCommission;
    toJSON(message: ValidatorAccumulatedCommission): unknown;
    fromPartial(object: DeepPartial<ValidatorAccumulatedCommission>): ValidatorAccumulatedCommission;
};
export declare const ValidatorOutstandingRewards: {
    encode(message: ValidatorOutstandingRewards, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValidatorOutstandingRewards;
    fromJSON(object: any): ValidatorOutstandingRewards;
    toJSON(message: ValidatorOutstandingRewards): unknown;
    fromPartial(object: DeepPartial<ValidatorOutstandingRewards>): ValidatorOutstandingRewards;
};
export declare const ValidatorSlashEvent: {
    encode(message: ValidatorSlashEvent, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValidatorSlashEvent;
    fromJSON(object: any): ValidatorSlashEvent;
    toJSON(message: ValidatorSlashEvent): unknown;
    fromPartial(object: DeepPartial<ValidatorSlashEvent>): ValidatorSlashEvent;
};
export declare const ValidatorSlashEvents: {
    encode(message: ValidatorSlashEvents, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValidatorSlashEvents;
    fromJSON(object: any): ValidatorSlashEvents;
    toJSON(message: ValidatorSlashEvents): unknown;
    fromPartial(object: DeepPartial<ValidatorSlashEvents>): ValidatorSlashEvents;
};
export declare const FeePool: {
    encode(message: FeePool, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): FeePool;
    fromJSON(object: any): FeePool;
    toJSON(message: FeePool): unknown;
    fromPartial(object: DeepPartial<FeePool>): FeePool;
};
export declare const CommunityPoolSpendProposal: {
    encode(message: CommunityPoolSpendProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): CommunityPoolSpendProposal;
    fromJSON(object: any): CommunityPoolSpendProposal;
    toJSON(message: CommunityPoolSpendProposal): unknown;
    fromPartial(object: DeepPartial<CommunityPoolSpendProposal>): CommunityPoolSpendProposal;
};
export declare const DelegatorStartingInfo: {
    encode(message: DelegatorStartingInfo, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): DelegatorStartingInfo;
    fromJSON(object: any): DelegatorStartingInfo;
    toJSON(message: DelegatorStartingInfo): unknown;
    fromPartial(object: DeepPartial<DelegatorStartingInfo>): DelegatorStartingInfo;
};
export declare const DelegationDelegatorReward: {
    encode(message: DelegationDelegatorReward, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): DelegationDelegatorReward;
    fromJSON(object: any): DelegationDelegatorReward;
    toJSON(message: DelegationDelegatorReward): unknown;
    fromPartial(object: DeepPartial<DelegationDelegatorReward>): DelegationDelegatorReward;
};
export declare const CommunityPoolSpendProposalWithDeposit: {
    encode(message: CommunityPoolSpendProposalWithDeposit, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): CommunityPoolSpendProposalWithDeposit;
    fromJSON(object: any): CommunityPoolSpendProposalWithDeposit;
    toJSON(message: CommunityPoolSpendProposalWithDeposit): unknown;
    fromPartial(object: DeepPartial<CommunityPoolSpendProposalWithDeposit>): CommunityPoolSpendProposalWithDeposit;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
