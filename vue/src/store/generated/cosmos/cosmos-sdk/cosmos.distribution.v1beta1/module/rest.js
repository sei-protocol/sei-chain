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
 * @title cosmos/distribution/v1beta1/distribution.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
        /**
         * No description
         *
         * @tags Query
         * @name QueryCommunityPool
         * @summary CommunityPool queries the community pool coins.
         * @request GET:/cosmos/distribution/v1beta1/community_pool
         */
        this.queryCommunityPool = (params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/community_pool`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryDelegationTotalRewards
       * @summary DelegationTotalRewards queries the total rewards accrued by a each
      validator.
       * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/rewards
       */
        this.queryDelegationTotalRewards = (delegatorAddress, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/delegators/${delegatorAddress}/rewards`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryDelegationRewards
         * @summary DelegationRewards queries the total rewards accrued by a delegation.
         * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/rewards/{validatorAddress}
         */
        this.queryDelegationRewards = (delegatorAddress, validatorAddress, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/delegators/${delegatorAddress}/rewards/${validatorAddress}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryDelegatorValidators
         * @summary DelegatorValidators queries the validators of a delegator.
         * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/validators
         */
        this.queryDelegatorValidators = (delegatorAddress, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/delegators/${delegatorAddress}/validators`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryDelegatorWithdrawAddress
         * @summary DelegatorWithdrawAddress queries withdraw address of a delegator.
         * @request GET:/cosmos/distribution/v1beta1/delegators/{delegatorAddress}/withdraw_address
         */
        this.queryDelegatorWithdrawAddress = (delegatorAddress, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/delegators/${delegatorAddress}/withdraw_address`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryParams
         * @summary Params queries params of the distribution module.
         * @request GET:/cosmos/distribution/v1beta1/params
         */
        this.queryParams = (params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/params`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidatorCommission
         * @summary ValidatorCommission queries accumulated commission for a validator.
         * @request GET:/cosmos/distribution/v1beta1/validators/{validatorAddress}/commission
         */
        this.queryValidatorCommission = (validatorAddress, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/validators/${validatorAddress}/commission`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidatorOutstandingRewards
         * @summary ValidatorOutstandingRewards queries rewards of a validator address.
         * @request GET:/cosmos/distribution/v1beta1/validators/{validatorAddress}/outstanding_rewards
         */
        this.queryValidatorOutstandingRewards = (validatorAddress, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/validators/${validatorAddress}/outstanding_rewards`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidatorSlashes
         * @summary ValidatorSlashes queries slash events of a validator.
         * @request GET:/cosmos/distribution/v1beta1/validators/{validatorAddress}/slashes
         */
        this.queryValidatorSlashes = (validatorAddress, query, params = {}) => this.request({
            path: `/cosmos/distribution/v1beta1/validators/${validatorAddress}/slashes`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
    }
}
