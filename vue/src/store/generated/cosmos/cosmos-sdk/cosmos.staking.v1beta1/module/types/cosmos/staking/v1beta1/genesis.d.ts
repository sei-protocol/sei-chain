import { Writer, Reader } from "protobufjs/minimal";
import { Params, Validator, Delegation, UnbondingDelegation, Redelegation } from "../../../cosmos/staking/v1beta1/staking";
export declare const protobufPackage = "cosmos.staking.v1beta1";
/** GenesisState defines the staking module's genesis state. */
export interface GenesisState {
    /** params defines all the paramaters of related to deposit. */
    params: Params | undefined;
    /**
     * last_total_power tracks the total amounts of bonded tokens recorded during
     * the previous end block.
     */
    lastTotalPower: Uint8Array;
    /**
     * last_validator_powers is a special index that provides a historical list
     * of the last-block's bonded validators.
     */
    lastValidatorPowers: LastValidatorPower[];
    /** delegations defines the validator set at genesis. */
    validators: Validator[];
    /** delegations defines the delegations active at genesis. */
    delegations: Delegation[];
    /** unbonding_delegations defines the unbonding delegations active at genesis. */
    unbondingDelegations: UnbondingDelegation[];
    /** redelegations defines the redelegations active at genesis. */
    redelegations: Redelegation[];
    exported: boolean;
}
/** LastValidatorPower required for validator set update logic. */
export interface LastValidatorPower {
    /** address is the address of the validator. */
    address: string;
    /** power defines the power of the validator. */
    power: number;
}
export declare const GenesisState: {
    encode(message: GenesisState, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): GenesisState;
    fromJSON(object: any): GenesisState;
    toJSON(message: GenesisState): unknown;
    fromPartial(object: DeepPartial<GenesisState>): GenesisState;
};
export declare const LastValidatorPower: {
    encode(message: LastValidatorPower, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): LastValidatorPower;
    fromJSON(object: any): LastValidatorPower;
    toJSON(message: LastValidatorPower): unknown;
    fromPartial(object: DeepPartial<LastValidatorPower>): LastValidatorPower;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
