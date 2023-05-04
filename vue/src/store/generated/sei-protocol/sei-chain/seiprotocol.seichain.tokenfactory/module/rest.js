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
 * @title tokenfactory/authorityMetadata.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
        /**
       * No description
       *
       * @tags Query
       * @name QueryDenomCreationFeeWhitelist
       * @summary DenomCreationFeeWhitelist defines a gRPC query method for fetching all
      creators who are whitelisted from paying the denom creation fee.
       * @request GET:/sei-protocol/seichain/tokenfactory/denom_creation_fee_whitelist
       */
        this.queryDenomCreationFeeWhitelist = (params = {}) => this.request({
            path: `/sei-protocol/seichain/tokenfactory/denom_creation_fee_whitelist`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryCreatorInDenomFeeWhitelist
       * @summary CreatorInDenomFeeWhitelist defines a gRPC query method for fetching
      whether a creator is whitelisted from denom creation fees.
       * @request GET:/sei-protocol/seichain/tokenfactory/denom_creation_fee_whitelist/{creator}
       */
        this.queryCreatorInDenomFeeWhitelist = (creator, params = {}) => this.request({
            path: `/sei-protocol/seichain/tokenfactory/denom_creation_fee_whitelist/${creator}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryDenomAuthorityMetadata
       * @summary DenomAuthorityMetadata defines a gRPC query method for fetching
      DenomAuthorityMetadata for a particular denom.
       * @request GET:/sei-protocol/seichain/tokenfactory/denoms/{denom}/authority_metadata
       */
        this.queryDenomAuthorityMetadata = (denom, params = {}) => this.request({
            path: `/sei-protocol/seichain/tokenfactory/denoms/${denom}/authority_metadata`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryDenomsFromCreator
       * @summary DenomsFromCreator defines a gRPC query method for fetching all
      denominations created by a specific admin/creator.
       * @request GET:/sei-protocol/seichain/tokenfactory/denoms_from_creator/{creator}
       */
        this.queryDenomsFromCreator = (creator, params = {}) => this.request({
            path: `/sei-protocol/seichain/tokenfactory/denoms_from_creator/${creator}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryParams
       * @summary Params defines a gRPC query method that returns the tokenfactory module's
      parameters.
       * @request GET:/sei-protocol/seichain/tokenfactory/params
       */
        this.queryParams = (params = {}) => this.request({
            path: `/sei-protocol/seichain/tokenfactory/params`,
            method: "GET",
            format: "json",
            ...params,
        });
    }
}
