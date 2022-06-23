import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "cosmos.upgrade.v1beta1";
/** Plan specifies information about a planned upgrade and when it should occur. */
export interface Plan {
    /**
     * Sets the name for the upgrade. This name will be used by the upgraded
     * version of the software to apply any special "on-upgrade" commands during
     * the first BeginBlock method after the upgrade is applied. It is also used
     * to detect whether a software version can handle a given upgrade. If no
     * upgrade handler with this name has been set in the software, it will be
     * assumed that the software is out-of-date when the upgrade Time or Height is
     * reached and the software will exit.
     */
    name: string;
    /**
     * The time after which the upgrade must be performed.
     * Leave set to its zero value to use a pre-defined Height instead.
     */
    time: Date | undefined;
    /**
     * The height at which the upgrade must be performed.
     * Only used if Time is not set.
     */
    height: number;
    /**
     * Any application specific upgrade info to be included on-chain
     * such as a git commit that validators could automatically upgrade to
     */
    info: string;
}
/**
 * SoftwareUpgradeProposal is a gov Content type for initiating a software
 * upgrade.
 */
export interface SoftwareUpgradeProposal {
    title: string;
    description: string;
    plan: Plan | undefined;
}
/**
 * CancelSoftwareUpgradeProposal is a gov Content type for cancelling a software
 * upgrade.
 */
export interface CancelSoftwareUpgradeProposal {
    title: string;
    description: string;
}
export declare const Plan: {
    encode(message: Plan, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Plan;
    fromJSON(object: any): Plan;
    toJSON(message: Plan): unknown;
    fromPartial(object: DeepPartial<Plan>): Plan;
};
export declare const SoftwareUpgradeProposal: {
    encode(message: SoftwareUpgradeProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): SoftwareUpgradeProposal;
    fromJSON(object: any): SoftwareUpgradeProposal;
    toJSON(message: SoftwareUpgradeProposal): unknown;
    fromPartial(object: DeepPartial<SoftwareUpgradeProposal>): SoftwareUpgradeProposal;
};
export declare const CancelSoftwareUpgradeProposal: {
    encode(message: CancelSoftwareUpgradeProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): CancelSoftwareUpgradeProposal;
    fromJSON(object: any): CancelSoftwareUpgradeProposal;
    toJSON(message: CancelSoftwareUpgradeProposal): unknown;
    fromPartial(object: DeepPartial<CancelSoftwareUpgradeProposal>): CancelSoftwareUpgradeProposal;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
