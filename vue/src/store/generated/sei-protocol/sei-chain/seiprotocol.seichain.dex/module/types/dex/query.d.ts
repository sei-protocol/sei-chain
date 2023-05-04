import { Reader, Writer } from "protobufjs/minimal";
import { Params } from "../dex/params";
import { LongBook } from "../dex/long_book";
import { PageRequest, PageResponse } from "../cosmos/base/query/v1beta1/pagination";
import { ShortBook } from "../dex/short_book";
import { Price, PriceCandlestick } from "../dex/price";
import { Twap } from "../dex/twap";
import { AssetMetadata } from "../dex/asset_list";
import { Pair } from "../dex/pair";
import { Order } from "../dex/order";
import { MatchResult } from "../dex/match_result";
export declare const protobufPackage = "seiprotocol.seichain.dex";
/** QueryParamsRequest is request type for the Query/Params RPC method. */
export interface QueryParamsRequest {
}
/** QueryParamsResponse is response type for the Query/Params RPC method. */
export interface QueryParamsResponse {
    /** params holds all the parameters of this module. */
    params: Params | undefined;
}
export interface QueryGetLongBookRequest {
    price: string;
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
}
export interface QueryGetLongBookResponse {
    LongBook: LongBook | undefined;
}
export interface QueryAllLongBookRequest {
    pagination: PageRequest | undefined;
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
}
export interface QueryAllLongBookResponse {
    LongBook: LongBook[];
    pagination: PageResponse | undefined;
}
export interface QueryGetShortBookRequest {
    price: string;
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
}
export interface QueryGetShortBookResponse {
    ShortBook: ShortBook | undefined;
}
export interface QueryAllShortBookRequest {
    pagination: PageRequest | undefined;
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
}
export interface QueryAllShortBookResponse {
    ShortBook: ShortBook[];
    pagination: PageResponse | undefined;
}
export interface QueryGetPricesRequest {
    priceDenom: string;
    assetDenom: string;
    contractAddr: string;
}
export interface QueryGetPricesResponse {
    prices: Price[];
}
export interface QueryGetTwapsRequest {
    contractAddr: string;
    lookbackSeconds: number;
}
export interface QueryGetTwapsResponse {
    twaps: Twap[];
}
export interface QueryAssetListRequest {
}
export interface QueryAssetListResponse {
    assetList: AssetMetadata[];
}
export interface QueryAssetMetadataRequest {
    denom: string;
}
export interface QueryAssetMetadataResponse {
    metadata: AssetMetadata | undefined;
}
export interface QueryRegisteredPairsRequest {
    contractAddr: string;
}
export interface QueryRegisteredPairsResponse {
    pairs: Pair[];
}
export interface QueryGetOrdersRequest {
    contractAddr: string;
    account: string;
}
export interface QueryGetOrdersResponse {
    orders: Order[];
}
export interface QueryGetOrderByIDRequest {
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
    id: number;
}
export interface QueryGetOrderByIDResponse {
    order: Order | undefined;
}
export interface QueryGetHistoricalPricesRequest {
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
    periodLengthInSeconds: number;
    numOfPeriods: number;
}
export interface QueryGetHistoricalPricesResponse {
    prices: PriceCandlestick[];
}
export interface QueryGetMarketSummaryRequest {
    contractAddr: string;
    priceDenom: string;
    assetDenom: string;
    lookbackInSeconds: number;
}
export interface QueryGetMarketSummaryResponse {
    totalVolume: string;
    totalVolumeNotional: string;
    highPrice: string;
    lowPrice: string;
    lastPrice: string;
}
export interface QueryOrderSimulationRequest {
    order: Order | undefined;
    contractAddr: string;
}
export interface QueryOrderSimulationResponse {
    ExecutedQuantity: string;
}
export interface QueryGetMatchResultRequest {
    contractAddr: string;
    height: number;
}
export interface QueryGetMatchResultResponse {
    result: MatchResult | undefined;
}
export declare const QueryParamsRequest: {
    encode(_: QueryParamsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryParamsRequest;
    fromJSON(_: any): QueryParamsRequest;
    toJSON(_: QueryParamsRequest): unknown;
    fromPartial(_: DeepPartial<QueryParamsRequest>): QueryParamsRequest;
};
export declare const QueryParamsResponse: {
    encode(message: QueryParamsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryParamsResponse;
    fromJSON(object: any): QueryParamsResponse;
    toJSON(message: QueryParamsResponse): unknown;
    fromPartial(object: DeepPartial<QueryParamsResponse>): QueryParamsResponse;
};
export declare const QueryGetLongBookRequest: {
    encode(message: QueryGetLongBookRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetLongBookRequest;
    fromJSON(object: any): QueryGetLongBookRequest;
    toJSON(message: QueryGetLongBookRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetLongBookRequest>): QueryGetLongBookRequest;
};
export declare const QueryGetLongBookResponse: {
    encode(message: QueryGetLongBookResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetLongBookResponse;
    fromJSON(object: any): QueryGetLongBookResponse;
    toJSON(message: QueryGetLongBookResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetLongBookResponse>): QueryGetLongBookResponse;
};
export declare const QueryAllLongBookRequest: {
    encode(message: QueryAllLongBookRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllLongBookRequest;
    fromJSON(object: any): QueryAllLongBookRequest;
    toJSON(message: QueryAllLongBookRequest): unknown;
    fromPartial(object: DeepPartial<QueryAllLongBookRequest>): QueryAllLongBookRequest;
};
export declare const QueryAllLongBookResponse: {
    encode(message: QueryAllLongBookResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllLongBookResponse;
    fromJSON(object: any): QueryAllLongBookResponse;
    toJSON(message: QueryAllLongBookResponse): unknown;
    fromPartial(object: DeepPartial<QueryAllLongBookResponse>): QueryAllLongBookResponse;
};
export declare const QueryGetShortBookRequest: {
    encode(message: QueryGetShortBookRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetShortBookRequest;
    fromJSON(object: any): QueryGetShortBookRequest;
    toJSON(message: QueryGetShortBookRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetShortBookRequest>): QueryGetShortBookRequest;
};
export declare const QueryGetShortBookResponse: {
    encode(message: QueryGetShortBookResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetShortBookResponse;
    fromJSON(object: any): QueryGetShortBookResponse;
    toJSON(message: QueryGetShortBookResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetShortBookResponse>): QueryGetShortBookResponse;
};
export declare const QueryAllShortBookRequest: {
    encode(message: QueryAllShortBookRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllShortBookRequest;
    fromJSON(object: any): QueryAllShortBookRequest;
    toJSON(message: QueryAllShortBookRequest): unknown;
    fromPartial(object: DeepPartial<QueryAllShortBookRequest>): QueryAllShortBookRequest;
};
export declare const QueryAllShortBookResponse: {
    encode(message: QueryAllShortBookResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAllShortBookResponse;
    fromJSON(object: any): QueryAllShortBookResponse;
    toJSON(message: QueryAllShortBookResponse): unknown;
    fromPartial(object: DeepPartial<QueryAllShortBookResponse>): QueryAllShortBookResponse;
};
export declare const QueryGetPricesRequest: {
    encode(message: QueryGetPricesRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetPricesRequest;
    fromJSON(object: any): QueryGetPricesRequest;
    toJSON(message: QueryGetPricesRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetPricesRequest>): QueryGetPricesRequest;
};
export declare const QueryGetPricesResponse: {
    encode(message: QueryGetPricesResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetPricesResponse;
    fromJSON(object: any): QueryGetPricesResponse;
    toJSON(message: QueryGetPricesResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetPricesResponse>): QueryGetPricesResponse;
};
export declare const QueryGetTwapsRequest: {
    encode(message: QueryGetTwapsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetTwapsRequest;
    fromJSON(object: any): QueryGetTwapsRequest;
    toJSON(message: QueryGetTwapsRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetTwapsRequest>): QueryGetTwapsRequest;
};
export declare const QueryGetTwapsResponse: {
    encode(message: QueryGetTwapsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetTwapsResponse;
    fromJSON(object: any): QueryGetTwapsResponse;
    toJSON(message: QueryGetTwapsResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetTwapsResponse>): QueryGetTwapsResponse;
};
export declare const QueryAssetListRequest: {
    encode(_: QueryAssetListRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAssetListRequest;
    fromJSON(_: any): QueryAssetListRequest;
    toJSON(_: QueryAssetListRequest): unknown;
    fromPartial(_: DeepPartial<QueryAssetListRequest>): QueryAssetListRequest;
};
export declare const QueryAssetListResponse: {
    encode(message: QueryAssetListResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAssetListResponse;
    fromJSON(object: any): QueryAssetListResponse;
    toJSON(message: QueryAssetListResponse): unknown;
    fromPartial(object: DeepPartial<QueryAssetListResponse>): QueryAssetListResponse;
};
export declare const QueryAssetMetadataRequest: {
    encode(message: QueryAssetMetadataRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAssetMetadataRequest;
    fromJSON(object: any): QueryAssetMetadataRequest;
    toJSON(message: QueryAssetMetadataRequest): unknown;
    fromPartial(object: DeepPartial<QueryAssetMetadataRequest>): QueryAssetMetadataRequest;
};
export declare const QueryAssetMetadataResponse: {
    encode(message: QueryAssetMetadataResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryAssetMetadataResponse;
    fromJSON(object: any): QueryAssetMetadataResponse;
    toJSON(message: QueryAssetMetadataResponse): unknown;
    fromPartial(object: DeepPartial<QueryAssetMetadataResponse>): QueryAssetMetadataResponse;
};
export declare const QueryRegisteredPairsRequest: {
    encode(message: QueryRegisteredPairsRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryRegisteredPairsRequest;
    fromJSON(object: any): QueryRegisteredPairsRequest;
    toJSON(message: QueryRegisteredPairsRequest): unknown;
    fromPartial(object: DeepPartial<QueryRegisteredPairsRequest>): QueryRegisteredPairsRequest;
};
export declare const QueryRegisteredPairsResponse: {
    encode(message: QueryRegisteredPairsResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryRegisteredPairsResponse;
    fromJSON(object: any): QueryRegisteredPairsResponse;
    toJSON(message: QueryRegisteredPairsResponse): unknown;
    fromPartial(object: DeepPartial<QueryRegisteredPairsResponse>): QueryRegisteredPairsResponse;
};
export declare const QueryGetOrdersRequest: {
    encode(message: QueryGetOrdersRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetOrdersRequest;
    fromJSON(object: any): QueryGetOrdersRequest;
    toJSON(message: QueryGetOrdersRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetOrdersRequest>): QueryGetOrdersRequest;
};
export declare const QueryGetOrdersResponse: {
    encode(message: QueryGetOrdersResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetOrdersResponse;
    fromJSON(object: any): QueryGetOrdersResponse;
    toJSON(message: QueryGetOrdersResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetOrdersResponse>): QueryGetOrdersResponse;
};
export declare const QueryGetOrderByIDRequest: {
    encode(message: QueryGetOrderByIDRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetOrderByIDRequest;
    fromJSON(object: any): QueryGetOrderByIDRequest;
    toJSON(message: QueryGetOrderByIDRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetOrderByIDRequest>): QueryGetOrderByIDRequest;
};
export declare const QueryGetOrderByIDResponse: {
    encode(message: QueryGetOrderByIDResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetOrderByIDResponse;
    fromJSON(object: any): QueryGetOrderByIDResponse;
    toJSON(message: QueryGetOrderByIDResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetOrderByIDResponse>): QueryGetOrderByIDResponse;
};
export declare const QueryGetHistoricalPricesRequest: {
    encode(message: QueryGetHistoricalPricesRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetHistoricalPricesRequest;
    fromJSON(object: any): QueryGetHistoricalPricesRequest;
    toJSON(message: QueryGetHistoricalPricesRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetHistoricalPricesRequest>): QueryGetHistoricalPricesRequest;
};
export declare const QueryGetHistoricalPricesResponse: {
    encode(message: QueryGetHistoricalPricesResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetHistoricalPricesResponse;
    fromJSON(object: any): QueryGetHistoricalPricesResponse;
    toJSON(message: QueryGetHistoricalPricesResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetHistoricalPricesResponse>): QueryGetHistoricalPricesResponse;
};
export declare const QueryGetMarketSummaryRequest: {
    encode(message: QueryGetMarketSummaryRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetMarketSummaryRequest;
    fromJSON(object: any): QueryGetMarketSummaryRequest;
    toJSON(message: QueryGetMarketSummaryRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetMarketSummaryRequest>): QueryGetMarketSummaryRequest;
};
export declare const QueryGetMarketSummaryResponse: {
    encode(message: QueryGetMarketSummaryResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetMarketSummaryResponse;
    fromJSON(object: any): QueryGetMarketSummaryResponse;
    toJSON(message: QueryGetMarketSummaryResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetMarketSummaryResponse>): QueryGetMarketSummaryResponse;
};
export declare const QueryOrderSimulationRequest: {
    encode(message: QueryOrderSimulationRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryOrderSimulationRequest;
    fromJSON(object: any): QueryOrderSimulationRequest;
    toJSON(message: QueryOrderSimulationRequest): unknown;
    fromPartial(object: DeepPartial<QueryOrderSimulationRequest>): QueryOrderSimulationRequest;
};
export declare const QueryOrderSimulationResponse: {
    encode(message: QueryOrderSimulationResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryOrderSimulationResponse;
    fromJSON(object: any): QueryOrderSimulationResponse;
    toJSON(message: QueryOrderSimulationResponse): unknown;
    fromPartial(object: DeepPartial<QueryOrderSimulationResponse>): QueryOrderSimulationResponse;
};
export declare const QueryGetMatchResultRequest: {
    encode(message: QueryGetMatchResultRequest, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetMatchResultRequest;
    fromJSON(object: any): QueryGetMatchResultRequest;
    toJSON(message: QueryGetMatchResultRequest): unknown;
    fromPartial(object: DeepPartial<QueryGetMatchResultRequest>): QueryGetMatchResultRequest;
};
export declare const QueryGetMatchResultResponse: {
    encode(message: QueryGetMatchResultResponse, writer?: Writer): Writer;
    decode(input: Reader | Uint8Array, length?: number): QueryGetMatchResultResponse;
    fromJSON(object: any): QueryGetMatchResultResponse;
    toJSON(message: QueryGetMatchResultResponse): unknown;
    fromPartial(object: DeepPartial<QueryGetMatchResultResponse>): QueryGetMatchResultResponse;
};
/** Query defines the gRPC querier service. */
export interface Query {
    /** Parameters queries the parameters of the module. */
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    /** Queries a LongBook by id. */
    LongBook(request: QueryGetLongBookRequest): Promise<QueryGetLongBookResponse>;
    /** Queries a list of LongBook items. */
    LongBookAll(request: QueryAllLongBookRequest): Promise<QueryAllLongBookResponse>;
    /** Queries a ShortBook by id. */
    ShortBook(request: QueryGetShortBookRequest): Promise<QueryGetShortBookResponse>;
    /** Queries a list of ShortBook items. */
    ShortBookAll(request: QueryAllShortBookRequest): Promise<QueryAllShortBookResponse>;
    GetPrices(request: QueryGetPricesRequest): Promise<QueryGetPricesResponse>;
    GetTwaps(request: QueryGetTwapsRequest): Promise<QueryGetTwapsResponse>;
    /** Returns the metadata for a specified denom / display type */
    AssetMetadata(request: QueryAssetMetadataRequest): Promise<QueryAssetMetadataResponse>;
    /** Returns metadata for all the assets */
    AssetList(request: QueryAssetListRequest): Promise<QueryAssetListResponse>;
    /** Returns all registered pairs for specified contract address */
    GetRegisteredPairs(request: QueryRegisteredPairsRequest): Promise<QueryRegisteredPairsResponse>;
    GetOrders(request: QueryGetOrdersRequest): Promise<QueryGetOrdersResponse>;
    GetOrder(request: QueryGetOrderByIDRequest): Promise<QueryGetOrderByIDResponse>;
    GetHistoricalPrices(request: QueryGetHistoricalPricesRequest): Promise<QueryGetHistoricalPricesResponse>;
    GetMarketSummary(request: QueryGetMarketSummaryRequest): Promise<QueryGetMarketSummaryResponse>;
    GetOrderSimulation(request: QueryOrderSimulationRequest): Promise<QueryOrderSimulationResponse>;
    GetMatchResult(request: QueryGetMatchResultRequest): Promise<QueryGetMatchResultResponse>;
}
export declare class QueryClientImpl implements Query {
    private readonly rpc;
    constructor(rpc: Rpc);
    Params(request: QueryParamsRequest): Promise<QueryParamsResponse>;
    LongBook(request: QueryGetLongBookRequest): Promise<QueryGetLongBookResponse>;
    LongBookAll(request: QueryAllLongBookRequest): Promise<QueryAllLongBookResponse>;
    ShortBook(request: QueryGetShortBookRequest): Promise<QueryGetShortBookResponse>;
    ShortBookAll(request: QueryAllShortBookRequest): Promise<QueryAllShortBookResponse>;
    GetPrices(request: QueryGetPricesRequest): Promise<QueryGetPricesResponse>;
    GetTwaps(request: QueryGetTwapsRequest): Promise<QueryGetTwapsResponse>;
    AssetMetadata(request: QueryAssetMetadataRequest): Promise<QueryAssetMetadataResponse>;
    AssetList(request: QueryAssetListRequest): Promise<QueryAssetListResponse>;
    GetRegisteredPairs(request: QueryRegisteredPairsRequest): Promise<QueryRegisteredPairsResponse>;
    GetOrders(request: QueryGetOrdersRequest): Promise<QueryGetOrdersResponse>;
    GetOrder(request: QueryGetOrderByIDRequest): Promise<QueryGetOrderByIDResponse>;
    GetHistoricalPrices(request: QueryGetHistoricalPricesRequest): Promise<QueryGetHistoricalPricesResponse>;
    GetMarketSummary(request: QueryGetMarketSummaryRequest): Promise<QueryGetMarketSummaryResponse>;
    GetOrderSimulation(request: QueryOrderSimulationRequest): Promise<QueryOrderSimulationResponse>;
    GetMatchResult(request: QueryGetMatchResultRequest): Promise<QueryGetMatchResultResponse>;
}
interface Rpc {
    request(service: string, method: string, data: Uint8Array): Promise<Uint8Array>;
}
declare type Builtin = Date | Function | Uint8Array | string | number | undefined;
export declare type DeepPartial<T> = T extends Builtin ? T : T extends Array<infer U> ? Array<DeepPartial<U>> : T extends ReadonlyArray<infer U> ? ReadonlyArray<DeepPartial<U>> : T extends {} ? {
    [K in keyof T]?: DeepPartial<T[K]>;
} : Partial<T>;
export {};
