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
 * @title oracle/genesis.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
        /**
         * No description
         *
         * @tags Query
         * @name QueryActives
         * @summary Actives returns all active denoms
         * @request GET:/sei-protocol/sei-chain/oracle/denoms/actives
         */
        this.queryActives = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/denoms/actives`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryExchangeRates
         * @summary ExchangeRates returns exchange rates of all denoms
         * @request GET:/sei-protocol/sei-chain/oracle/denoms/exchange_rates
         */
        this.queryExchangeRates = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/denoms/exchange_rates`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryPriceSnapshotHistory
         * @summary PriceSnapshotHistory returns the history of price snapshots for all assets
         * @request GET:/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history
         */
        this.queryPriceSnapshotHistory = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/denoms/price_snapshot_history`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryTwaps
         * @request GET:/sei-protocol/sei-chain/oracle/denoms/twaps
         */
        this.queryTwaps = (query, params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/denoms/twaps`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryVoteTargets
         * @summary VoteTargets returns all vote target denoms
         * @request GET:/sei-protocol/sei-chain/oracle/denoms/vote_targets
         */
        this.queryVoteTargets = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/denoms/vote_targets`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryExchangeRate
         * @summary ExchangeRate returns exchange rate of a denom
         * @request GET:/sei-protocol/sei-chain/oracle/denoms/{denom}/exchange_rate
         */
        this.queryExchangeRate = (denom, params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/denoms/${denom}/exchange_rate`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryParams
         * @summary Params queries all parameters.
         * @request GET:/sei-protocol/sei-chain/oracle/params
         */
        this.queryParams = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/params`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryAggregatePrevotes
         * @summary AggregatePrevotes returns aggregate prevotes of all validators
         * @request GET:/sei-protocol/sei-chain/oracle/validators/aggregate_prevotes
         */
        this.queryAggregatePrevotes = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/validators/aggregate_prevotes`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryAggregateVotes
         * @summary AggregateVotes returns aggregate votes of all validators
         * @request GET:/sei-protocol/sei-chain/oracle/validators/aggregate_votes
         */
        this.queryAggregateVotes = (params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/validators/aggregate_votes`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryAggregatePrevote
         * @summary AggregatePrevote returns an aggregate prevote of a validator
         * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/aggregate_prevote
         */
        this.queryAggregatePrevote = (validatorAddr, params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/validators/${validatorAddr}/aggregate_prevote`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryAggregateVote
         * @summary AggregateVote returns an aggregate vote of a validator
         * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/aggregate_vote
         */
        this.queryAggregateVote = (validatorAddr, params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/validators/${validatorAddr}/aggregate_vote`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryFeederDelegation
         * @summary FeederDelegation returns feeder delegation of a validator
         * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/feeder
         */
        this.queryFeederDelegation = (validatorAddr, params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/validators/${validatorAddr}/feeder`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryVotePenaltyCounter
         * @summary MissCounter returns oracle miss counter of a validator
         * @request GET:/sei-protocol/sei-chain/oracle/validators/{validatorAddr}/vote_penalty_counter
         */
        this.queryVotePenaltyCounter = (validatorAddr, params = {}) => this.request({
            path: `/sei-protocol/sei-chain/oracle/validators/${validatorAddr}/vote_penalty_counter`,
            method: "GET",
            format: "json",
            ...params,
        });
    }
}
