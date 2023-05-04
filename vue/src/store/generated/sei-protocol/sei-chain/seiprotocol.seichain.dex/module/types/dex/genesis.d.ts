import { Writer, Reader } from "protobufjs/minimal";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import { ShortBook } from "../dex/short_book";
import { Twap } from "../dex/twap";
import { TickSize } from "../dex/tick_size";
export declare const protobufPackage = "seiprotocol.seichain.dex";
/** GenesisState defines the dex module's genesis state. */
export interface GenesisState {
    params: Params | undefined;
    longBookList: LongBook[];
    shortBookList: ShortBook[];
    twapList: Twap[];
    /** if null, then no restriction, todo(zw) should set it to not nullable? */
    tickSizeList: TickSize[];
    /** this line is used by starport scaffolding # genesis/proto/state */
    lastEpoch: number;
}
export declare const GenesisState: {
    encode(message: GenesisState, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): GenesisState;
    fromJSON(object: any): GenesisState;
    toJSON(message: GenesisState): unknown;
    fromPartial(object: DeepPartial<GenesisState>): GenesisState;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
