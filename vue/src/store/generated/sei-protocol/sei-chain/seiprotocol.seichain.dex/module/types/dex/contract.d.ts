import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.dex";
export interface ContractInfo {
    codeId: number;
    contractAddr: string;
    needHook: boolean;
    needOrderMatching: boolean;
    dependencies: ContractDependencyInfo[];
    numIncomingDependencies: number;
}
export interface ContractDependencyInfo {
    dependency: string;
    immediateElderSibling: string;
    immediateYoungerSibling: string;
}
export interface LegacyContractInfo {
    codeId: number;
    contractAddr: string;
    needHook: boolean;
    needOrderMatching: boolean;
    dependentContractAddrs: string[];
}
export declare const ContractInfo: {
    encode(message: ContractInfo, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ContractInfo;
    fromJSON(object: any): ContractInfo;
    toJSON(message: ContractInfo): unknown;
    fromPartial(object: DeepPartial<ContractInfo>): ContractInfo;
};
export declare const ContractDependencyInfo: {
    encode(message: ContractDependencyInfo, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ContractDependencyInfo;
    fromJSON(object: any): ContractDependencyInfo;
    toJSON(message: ContractDependencyInfo): unknown;
    fromPartial(object: DeepPartial<ContractDependencyInfo>): ContractDependencyInfo;
};
export declare const LegacyContractInfo: {
    encode(message: LegacyContractInfo, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): LegacyContractInfo;
    fromJSON(object: any): LegacyContractInfo;
    toJSON(message: LegacyContractInfo): unknown;
    fromPartial(object: DeepPartial<LegacyContractInfo>): LegacyContractInfo;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
