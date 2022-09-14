import { Writer, Reader } from "protobufjs/minimal";
export declare const protobufPackage = "seiprotocol.seichain.tokenfactory";
export interface AddCreatorsToDenomFeeWhitelistProposal {
    title: string;
    description: string;
    creatorList: string[];
}
export declare const AddCreatorsToDenomFeeWhitelistProposal: {
    encode(message: AddCreatorsToDenomFeeWhitelistProposal, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): AddCreatorsToDenomFeeWhitelistProposal;
    fromJSON(object: any): AddCreatorsToDenomFeeWhitelistProposal;
    toJSON(message: AddCreatorsToDenomFeeWhitelistProposal): unknown;
    fromPartial(object: DeepPartial<AddCreatorsToDenomFeeWhitelistProposal>): AddCreatorsToDenomFeeWhitelistProposal;
};
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
