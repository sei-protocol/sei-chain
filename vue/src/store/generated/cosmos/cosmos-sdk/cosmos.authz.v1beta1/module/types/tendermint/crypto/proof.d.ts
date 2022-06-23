import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "tendermint.crypto";
export interface Proof {
    total: number;
    index: number;
    leafHash: Uint8Array;
    aunts: Uint8Array[];
}
export interface ValueOp {
    /** Encoded in ProofOp.Key. */
    key: Uint8Array;
    /** To encode in ProofOp.Data */
    proof: Proof | undefined;
}
export interface DominoOp {
    key: string;
    input: string;
    output: string;
}
/**
 * ProofOp defines an operation used for calculating Merkle root
 * The data could be arbitrary format, providing nessecary data
 * for example neighbouring node hash
 */
export interface ProofOp {
    type: string;
    key: Uint8Array;
    data: Uint8Array;
}
/** ProofOps is Merkle proof defined by the list of ProofOps */
export interface ProofOps {
    ops: ProofOp[];
}
export declare const Proof: {
    encode(message: Proof, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Proof;
    fromJSON(object: any): Proof;
    toJSON(message: Proof): unknown;
    fromPartial(object: DeepPartial<Proof>): Proof;
};
export declare const ValueOp: {
    encode(message: ValueOp, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ValueOp;
    fromJSON(object: any): ValueOp;
    toJSON(message: ValueOp): unknown;
    fromPartial(object: DeepPartial<ValueOp>): ValueOp;
};
export declare const DominoOp: {
    encode(message: DominoOp, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): DominoOp;
    fromJSON(object: any): DominoOp;
    toJSON(message: DominoOp): unknown;
    fromPartial(object: DeepPartial<DominoOp>): DominoOp;
};
export declare const ProofOp: {
    encode(message: ProofOp, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ProofOp;
    fromJSON(object: any): ProofOp;
    toJSON(message: ProofOp): unknown;
    fromPartial(object: DeepPartial<ProofOp>): ProofOp;
};
export declare const ProofOps: {
    encode(message: ProofOps, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): ProofOps;
    fromJSON(object: any): ProofOps;
    toJSON(message: ProofOps): unknown;
    fromPartial(object: DeepPartial<ProofOps>): ProofOps;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
