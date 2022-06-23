import { Writer, Reader } from "protobufjs/minimal";
import { BaseAccount } from "../../../cosmos/auth/v1beta1/auth";
import { Coin } from "../../../cosmos/base/v1beta1/coin";
export declare const protobufPackage = "cosmos.vesting.v1beta1";
/**
 * BaseVestingAccount implements the VestingAccount interface. It contains all
 * the necessary fields needed for any vesting account implementation.
 */
export interface BaseVestingAccount {
    baseAccount: BaseAccount | undefined;
    originalVesting: Coin[];
    delegatedFree: Coin[];
    delegatedVesting: Coin[];
    endTime: number;
}
/**
 * ContinuousVestingAccount implements the VestingAccount interface. It
 * continuously vests by unlocking coins linearly with respect to time.
 */
export interface ContinuousVestingAccount {
    baseVestingAccount: BaseVestingAccount | undefined;
    startTime: number;
}
/**
 * DelayedVestingAccount implements the VestingAccount interface. It vests all
 * coins after a specific time, but non prior. In other words, it keeps them
 * locked until a specified time.
 */
export interface DelayedVestingAccount {
    baseVestingAccount: BaseVestingAccount | undefined;
}
/** Period defines a length of time and amount of coins that will vest. */
export interface Period {
    length: number;
    amount: Coin[];
}
/**
 * PeriodicVestingAccount implements the VestingAccount interface. It
 * periodically vests by unlocking coins during each specified period.
 */
export interface PeriodicVestingAccount {
    baseVestingAccount: BaseVestingAccount | undefined;
    startTime: number;
    vestingPeriods: Period[];
}
/**
 * PermanentLockedAccount implements the VestingAccount interface. It does
 * not ever release coins, locking them indefinitely. Coins in this account can
 * still be used for delegating and for governance votes even while locked.
 *
 * Since: cosmos-sdk 0.43
 */
export interface PermanentLockedAccount {
    baseVestingAccount: BaseVestingAccount | undefined;
}
export declare const BaseVestingAccount: {
    encode(message: BaseVestingAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): BaseVestingAccount;
    fromJSON(object: any): BaseVestingAccount;
    toJSON(message: BaseVestingAccount): unknown;
    fromPartial(object: DeepPartial<BaseVestingAccount>): BaseVestingAccount;
};
export declare const ContinuousVestingAccount: {
    encode(message: ContinuousVestingAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ContinuousVestingAccount;
    fromJSON(object: any): ContinuousVestingAccount;
    toJSON(message: ContinuousVestingAccount): unknown;
    fromPartial(object: DeepPartial<ContinuousVestingAccount>): ContinuousVestingAccount;
};
export declare const DelayedVestingAccount: {
    encode(message: DelayedVestingAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): DelayedVestingAccount;
    fromJSON(object: any): DelayedVestingAccount;
    toJSON(message: DelayedVestingAccount): unknown;
    fromPartial(object: DeepPartial<DelayedVestingAccount>): DelayedVestingAccount;
};
export declare const Period: {
    encode(message: Period, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Period;
    fromJSON(object: any): Period;
    toJSON(message: Period): unknown;
    fromPartial(object: DeepPartial<Period>): Period;
};
export declare const PeriodicVestingAccount: {
    encode(message: PeriodicVestingAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PeriodicVestingAccount;
    fromJSON(object: any): PeriodicVestingAccount;
    toJSON(message: PeriodicVestingAccount): unknown;
    fromPartial(object: DeepPartial<PeriodicVestingAccount>): PeriodicVestingAccount;
};
export declare const PermanentLockedAccount: {
    encode(message: PermanentLockedAccount, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): PermanentLockedAccount;
    fromJSON(object: any): PermanentLockedAccount;
    toJSON(message: PermanentLockedAccount): unknown;
    fromPartial(object: DeepPartial<PermanentLockedAccount>): PermanentLockedAccount;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
