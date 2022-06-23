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
export var DexDenom;
(function (DexDenom) {
    DexDenom["SEI"] = "SEI";
    DexDenom["ATOM"] = "ATOM";
    DexDenom["BTC"] = "BTC";
    DexDenom["ETH"] = "ETH";
    DexDenom["SOL"] = "SOL";
    DexDenom["AVAX"] = "AVAX";
    DexDenom["USDC"] = "USDC";
    DexDenom["NEAR"] = "NEAR";
    DexDenom["OSMO"] = "OSMO";
})(DexDenom || (DexDenom = {}));
export var DexOrderType;
(function (DexOrderType) {
    DexOrderType["LIMIT"] = "LIMIT";
    DexOrderType["MARKET"] = "MARKET";
    DexOrderType["LIQUIDATION"] = "LIQUIDATION";
})(DexOrderType || (DexOrderType = {}));
export var DexPositionDirection;
(function (DexPositionDirection) {
    DexPositionDirection["LONG"] = "LONG";
    DexPositionDirection["SHORT"] = "SHORT";
})(DexPositionDirection || (DexPositionDirection = {}));
export var DexPositionEffect;
(function (DexPositionEffect) {
    DexPositionEffect["OPEN"] = "OPEN";
    DexPositionEffect["CLOSE"] = "CLOSE";
})(DexPositionEffect || (DexPositionEffect = {}));
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
 * @title dex/contract.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
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
         * @name QuerySettlementsAll
         * @request GET:/sei-protocol/seichain/dex/settlement
         */
        this.querySettlementsAll = (query, params = {}) => this.request({
            path: `/sei-protocol/seichain/dex/settlement`,
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
