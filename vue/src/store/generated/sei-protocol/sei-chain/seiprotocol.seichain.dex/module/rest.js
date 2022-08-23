/* eslint-disable */
/* tslint:disable */
/*
 * ---------------------------------------------------------------
 * ## THIS FILE WAS GENERATED VIA SWAGGER-TYPESCRIPT-API        ##
 * ##                                                           ##
 * ## AUTHOR: acacode                                           ##
 * ## SOURCE: https://github.com/acacode/swagger-typescript-api ##
 * ---------------------------------------------------------------
 */
export var DexCancellationInitiator;
(function (DexCancellationInitiator) {
    DexCancellationInitiator["USER"] = "USER";
    DexCancellationInitiator["LIQUIDATED"] = "LIQUIDATED";
})(DexCancellationInitiator || (DexCancellationInitiator = {}));
export var DexOrderStatus;
(function (DexOrderStatus) {
    DexOrderStatus["PLACED"] = "PLACED";
    DexOrderStatus["FAILED_TO_PLACE"] = "FAILED_TO_PLACE";
    DexOrderStatus["CANCELLED"] = "CANCELLED";
    DexOrderStatus["FULFILLED"] = "FULFILLED";
})(DexOrderStatus || (DexOrderStatus = {}));
export var DexOrderType;
(function (DexOrderType) {
    DexOrderType["LIMIT"] = "LIMIT";
    DexOrderType["MARKET"] = "MARKET";
    DexOrderType["LIQUIDATION"] = "LIQUIDATION";
    DexOrderType["FOKMARKET"] = "FOKMARKET";
})(DexOrderType || (DexOrderType = {}));
export var DexPositionDirection;
(function (DexPositionDirection) {
    DexPositionDirection["LONG"] = "LONG";
    DexPositionDirection["SHORT"] = "SHORT";
})(DexPositionDirection || (DexPositionDirection = {}));
export var ContentType;
(function (ContentType) {
    ContentType["Json"] = "application/json";
    ContentType["FormData"] = "multipart/form-data";
    ContentType["UrlEncoded"] = "application/x-www-form-urlencoded";
})(ContentType || (ContentType = {}));
export class HttpClient {
    constructor(apiConfig = {}) {
        this.baseUrl = "";
        this.securityData = null;
        this.securityWorker = null;
        this.abortControllers = new Map();
        this.baseApiParams = {
            credentials: "same-origin",
            headers: {},
            redirect: "follow",
            referrerPolicy: "no-referrer",
        };
        this.setSecurityData = (data) => {
            this.securityData = data;
        };
        this.contentFormatters = {
            [ContentType.Json]: (input) => input !== null && (typeof input === "object" || typeof input === "string") ? JSON.stringify(input) : input,
            [ContentType.FormData]: (input) => Object.keys(input || {}).reduce((data, key) => {
                data.append(key, input[key]);
                return data;
            }, new FormData()),
            [ContentType.UrlEncoded]: (input) => this.toQueryString(input),
        };
        this.createAbortSignal = (cancelToken) => {
            if (this.abortControllers.has(cancelToken)) {
                const abortController = this.abortControllers.get(cancelToken);
                if (abortController) {
                    return abortController.signal;
                }
                return void 0;
            }
            const abortController = new AbortController();
            this.abortControllers.set(cancelToken, abortController);
            return abortController.signal;
        };
        this.abortRequest = (cancelToken) => {
            const abortController = this.abortControllers.get(cancelToken);
            if (abortController) {
                abortController.abort();
                this.abortControllers.delete(cancelToken);
            }
        };
        this.request = ({ body, secure, path, type, query, format = "json", baseUrl, cancelToken, ...params }) => {
            const secureParams = (secure && this.securityWorker && this.securityWorker(this.securityData)) || {};
            const requestParams = this.mergeRequestParams(params, secureParams);
            const queryString = query && this.toQueryString(query);
            const payloadFormatter = this.contentFormatters[type || ContentType.Json];
            return fetch(`${baseUrl || this.baseUrl || ""}${path}${queryString ? `?${queryString}` : ""}`, {
                ...requestParams,
                headers: {
                    ...(type && type !== ContentType.FormData ? { "Content-Type": type } : {}),
                    ...(requestParams.headers || {}),
                },
                signal: cancelToken ? this.createAbortSignal(cancelToken) : void 0,
                body: typeof body === "undefined" || body === null ? null : payloadFormatter(body),
            }).then(async (response) => {
                const r = response;
                r.data = null;
                r.error = null;
                const data = await response[format]()
                    .then((data) => {
                    if (r.ok) {
                        r.data = data;
                    }
                    else {
                        r.error = data;
                    }
                    return r;
                })
                    .catch((e) => {
                    r.error = e;
                    return r;
                });
                if (cancelToken) {
                    this.abortControllers.delete(cancelToken);
                }
                if (!response.ok)
                    throw data;
                return data;
            });
        };
        Object.assign(this, apiConfig);
    }
    addQueryParam(query, key) {
        const value = query[key];
        return (encodeURIComponent(key) +
            "=" +
            encodeURIComponent(Array.isArray(value) ? value.join(",") : typeof value === "number" ? value : `${value}`));
    }
    toQueryString(rawQuery) {
        const query = rawQuery || {};
        const keys = Object.keys(query).filter((key) => "undefined" !== typeof query[key]);
        return keys
            .map((key) => typeof query[key] === "object" && !Array.isArray(query[key])
            ? this.toQueryString(query[key])
            : this.addQueryParam(query, key))
            .join("&");
    }
    addQueryParams(rawQuery) {
        const queryString = this.toQueryString(rawQuery);
        return queryString ? `?${queryString}` : "";
    }
    mergeRequestParams(params1, params2) {
        return {
            ...this.baseApiParams,
            ...params1,
            ...(params2 || {}),
            headers: {
                ...(this.baseApiParams.headers || {}),
                ...(params1.headers || {}),
                ...((params2 && params2.headers) || {}),
            },
        };
    }
}
/**
 * @title dex/asset_list.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
        /**
         * No description
         *
         * @tags Query
         * @name QueryAssetList
         * @summary Returns metadata for all the assets
         * @request GET:/sei-protocol/seichain/dex/asset_list
         */
        this.queryAssetList = (params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/asset_list`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryAssetMetadata
         * @summary Returns the metadata for a specified denom / display type
         * @request GET:/sei-protocol/seichain/dex/asset_list/{denom}
         */
        this.queryAssetMetadata = (denom, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/asset_list/${denom}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetHistoricalPrices
         * @request GET:/sei-protocol/seichain/dex/get_historical_prices/{contractAddr}/{priceDenom}/{assetDenom}/{periodLengthInSeconds}/{numOfPeriods}
         */
        this.queryGetHistoricalPrices = (contractAddr, priceDenom, assetDenom, periodLengthInSeconds, numOfPeriods, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/get_historical_prices/${contractAddr}/${priceDenom}/${assetDenom}/${periodLengthInSeconds}/${numOfPeriods}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetMarketSummary
         * @request GET:/sei-protocol/seichain/dex/get_market_summary/{contractAddr}/{priceDenom}/{assetDenom}/{lookbackInSeconds}
         */
        this.queryGetMarketSummary = (contractAddr, priceDenom, assetDenom, lookbackInSeconds, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/get_market_summary/${contractAddr}/${priceDenom}/${assetDenom}/${lookbackInSeconds}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetOrder
         * @request GET:/sei-protocol/seichain/dex/get_order_by_id/{contractAddr}/{priceDenom}/{assetDenom}/{id}
         */
        this.queryGetOrder = (contractAddr, priceDenom, assetDenom, id, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/get_order_by_id/${contractAddr}/${priceDenom}/${assetDenom}/${id}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetOrders
         * @request GET:/sei-protocol/seichain/dex/get_orders/{contractAddr}/{account}
         */
        this.queryGetOrders = (contractAddr, account, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/get_orders/${contractAddr}/${account}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetPrices
         * @request GET:/sei-protocol/seichain/dex/get_prices/{contractAddr}/{priceDenom}/{assetDenom}
         */
        this.queryGetPrices = (contractAddr, priceDenom, assetDenom, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/get_prices/${contractAddr}/${priceDenom}/${assetDenom}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetTwaps
         * @request GET:/sei-protocol/seichain/dex/get_twaps/{contractAddr}/{lookbackSeconds}
         */
        this.queryGetTwaps = (contractAddr, lookbackSeconds, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/get_twaps/${contractAddr}/${lookbackSeconds}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryLongBookAll
         * @summary Queries a list of LongBook items.
         * @request GET:/sei-protocol/seichain/dex/long_book/{contractAddr}/{priceDenom}/{assetDenom}
         */
        this.queryLongBookAll = (contractAddr, priceDenom, assetDenom, query, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/long_book/${contractAddr}/${priceDenom}/${assetDenom}`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryLongBook
         * @summary Queries a LongBook by id.
         * @request GET:/sei-protocol/seichain/dex/long_book/{contractAddr}/{priceDenom}/{assetDenom}/{price}
         */
        this.queryLongBook = (contractAddr, priceDenom, assetDenom, price, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/long_book/${contractAddr}/${priceDenom}/${assetDenom}/${price}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryParams
         * @summary Parameters queries the parameters of the module.
         * @request GET:/sei-protocol/seichain/dex/params
         */
        this.queryParams = (params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/params`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryGetRegisteredPairs
         * @summary Returns all registered pairs for specified contract address
         * @request GET:/sei-protocol/seichain/dex/registered_pairs
         */
        this.queryGetRegisteredPairs = (query, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/registered_pairs`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryShortBookAll
         * @summary Queries a list of ShortBook items.
         * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}
         */
        this.queryShortBookAll = (contractAddr, priceDenom, assetDenom, query, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/short_book/${contractAddr}/${priceDenom}/${assetDenom}`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryShortBook
         * @summary Queries a ShortBook by id.
         * @request GET:/sei-protocol/seichain/dex/short_book/{contractAddr}/{priceDenom}/{assetDenom}/{price}
         */
        this.queryShortBook = (contractAddr, priceDenom, assetDenom, price, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/short_book/${contractAddr}/${priceDenom}/${assetDenom}/${price}`,
            method: "GET",
            format: "json",
            ...params,
        });
    }
}
