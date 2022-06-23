import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "tendermint.version";
/**
 * App includes the protocol and software version for the application.
 * This information is included in ResponseInfo. The App.Protocol can be
 * updated in ResponseEndBlock.
 */
export interface App {
    protocol: number;
    software: string;
}
/**
 * Consensus captures the consensus rules for processing a block in the blockchain,
 * including all blockchain data structures and the rules of the application's
 * state transition machine.
 */
export interface Consensus {
    block: number;
    app: number;
}
export declare const App: {
    encode(message: App, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): App;
    fromJSON(object: any): App;
    toJSON(message: App): unknown;
    fromPartial(object: DeepPartial<App>): App;
};
export declare const Consensus: {
    encode(message: Consensus, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): Consensus;
    fromJSON(object: any): Consensus;
    toJSON(message: Consensus): unknown;
    fromPartial(object: DeepPartial<Consensus>): Consensus;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
