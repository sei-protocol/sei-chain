import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "cosmos.staking.v1beta1";
/**
 * AuthorizationType defines the type of staking module authorization type
 *
 * Since: cosmos-sdk 0.43
 */
export declare enum AuthorizationType {
    /** AUTHORIZATION_TYPE_UNSPECIFIED - AUTHORIZATION_TYPE_UNSPECIFIED specifies an unknown authorization type */
    AUTHORIZATION_TYPE_UNSPECIFIED = 0,
    /** AUTHORIZATION_TYPE_DELEGATE - AUTHORIZATION_TYPE_DELEGATE defines an authorization type for Msg/Delegate */
    AUTHORIZATION_TYPE_DELEGATE = 1,
    /** AUTHORIZATION_TYPE_UNDELEGATE - AUTHORIZATION_TYPE_UNDELEGATE defines an authorization type for Msg/Undelegate */
    AUTHORIZATION_TYPE_UNDELEGATE = 2,
    /** AUTHORIZATION_TYPE_REDELEGATE - AUTHORIZATION_TYPE_REDELEGATE defines an authorization type for Msg/BeginRedelegate */
    AUTHORIZATION_TYPE_REDELEGATE = 3,
    UNRECOGNIZED = -1
}
export declare function authorizationTypeFromJSON(object: any): AuthorizationType;
export declare function authorizationTypeToJSON(object: AuthorizationType): string;
/**
 * StakeAuthorization defines authorization for delegate/undelegate/redelegate.
 *
 * Since: cosmos-sdk 0.43
 */
export interface StakeAuthorization {
    /**
     * max_tokens specifies the maximum amount of tokens can be delegate to a validator. If it is
     * empty, there is no spend limit and any amount of coins can be delegated.
     */
    maxTokens: Coin | undefined;
    /**
     * allow_list specifies list of validator addresses to whom grantee can delegate tokens on behalf of granter's
     * account.
     */
    allowList: StakeAuthorization_Validators | undefined;
    /** deny_list specifies list of validator addresses to whom grantee can not delegate tokens. */
    denyList: StakeAuthorization_Validators | undefined;
    /** authorization_type defines one of AuthorizationType. */
    authorizationType: AuthorizationType;
}
/** Validators defines list of validator addresses. */
export interface StakeAuthorization_Validators {
    address: string[];
}
export declare const StakeAuthorization: {
    encode(message: StakeAuthorization, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): StakeAuthorization;
    fromJSON(object: any): StakeAuthorization;
    toJSON(message: StakeAuthorization): unknown;
    fromPartial(object: DeepPartial<StakeAuthorization>): StakeAuthorization;
};
export declare const StakeAuthorization_Validators: {
    encode(message: StakeAuthorization_Validators, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): StakeAuthorization_Validators;
    fromJSON(object: any): StakeAuthorization_Validators;
    toJSON(message: StakeAuthorization_Validators): unknown;
    fromPartial(object: DeepPartial<StakeAuthorization_Validators>): StakeAuthorization_Validators;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
