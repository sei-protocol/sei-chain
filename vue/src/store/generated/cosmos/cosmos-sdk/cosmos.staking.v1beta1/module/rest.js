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
/**
* BondStatus is the status of a validator.

 - BOND_STATUS_UNSPECIFIED: UNSPECIFIED defines an invalid validator status.
 - BOND_STATUS_UNBONDED: UNBONDED defines a validator that is not bonded.
 - BOND_STATUS_UNBONDING: UNBONDING defines a validator that is unbonding.
 - BOND_STATUS_BONDED: BONDED defines a validator that is bonded.
*/
export var V1Beta1BondStatus;
(function (V1Beta1BondStatus) {
    V1Beta1BondStatus["BOND_STATUS_UNSPECIFIED"] = "BOND_STATUS_UNSPECIFIED";
    V1Beta1BondStatus["BOND_STATUS_UNBONDED"] = "BOND_STATUS_UNBONDED";
    V1Beta1BondStatus["BOND_STATUS_UNBONDING"] = "BOND_STATUS_UNBONDING";
    V1Beta1BondStatus["BOND_STATUS_BONDED"] = "BOND_STATUS_BONDED";
})(V1Beta1BondStatus || (V1Beta1BondStatus = {}));
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
 * @title cosmos/staking/v1beta1/authz.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
        /**
         * No description
         *
         * @tags Query
         * @name QueryDelegatorDelegations
         * @summary DelegatorDelegations queries all delegations of a given delegator address.
         * @request GET:/cosmos/staking/v1beta1/delegations/{delegatorAddr}
         */
        this.queryDelegatorDelegations = (delegatorAddr, query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/delegations/${delegatorAddr}`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryRedelegations
         * @summary Redelegations queries redelegations of given address.
         * @request GET:/cosmos/staking/v1beta1/delegators/{delegatorAddr}/redelegations
         */
        this.queryRedelegations = (delegatorAddr, query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/delegators/${delegatorAddr}/redelegations`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryDelegatorUnbondingDelegations
       * @summary DelegatorUnbondingDelegations queries all unbonding delegations of a given
      delegator address.
       * @request GET:/cosmos/staking/v1beta1/delegators/{delegatorAddr}/unbonding_delegations
       */
        this.queryDelegatorUnbondingDelegations = (delegatorAddr, query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/delegators/${delegatorAddr}/unbonding_delegations`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryDelegatorValidators
       * @summary DelegatorValidators queries all validators info for given delegator
      address.
       * @request GET:/cosmos/staking/v1beta1/delegators/{delegatorAddr}/validators
       */
        this.queryDelegatorValidators = (delegatorAddr, query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/delegators/${delegatorAddr}/validators`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryDelegatorValidator
       * @summary DelegatorValidator queries validator info for given delegator validator
      pair.
       * @request GET:/cosmos/staking/v1beta1/delegators/{delegatorAddr}/validators/{validatorAddr}
       */
        this.queryDelegatorValidator = (delegatorAddr, validatorAddr, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/delegators/${delegatorAddr}/validators/${validatorAddr}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryHistoricalInfo
         * @summary HistoricalInfo queries the historical info for given height.
         * @request GET:/cosmos/staking/v1beta1/historical_info/{height}
         */
        this.queryHistoricalInfo = (height, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/historical_info/${height}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryParams
         * @summary Parameters queries the staking parameters.
         * @request GET:/cosmos/staking/v1beta1/params
         */
        this.queryParams = (params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/params`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryPool
         * @summary Pool queries the pool info.
         * @request GET:/cosmos/staking/v1beta1/pool
         */
        this.queryPool = (params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/pool`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidators
         * @summary Validators queries all validators that match the given status.
         * @request GET:/cosmos/staking/v1beta1/validators
         */
        this.queryValidators = (query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/validators`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidator
         * @summary Validator queries validator info for given validator address.
         * @request GET:/cosmos/staking/v1beta1/validators/{validatorAddr}
         */
        this.queryValidator = (validatorAddr, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/validators/${validatorAddr}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidatorDelegations
         * @summary ValidatorDelegations queries delegate info for given validator.
         * @request GET:/cosmos/staking/v1beta1/validators/{validatorAddr}/delegations
         */
        this.queryValidatorDelegations = (validatorAddr, query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/validators/${validatorAddr}/delegations`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryDelegation
         * @summary Delegation queries delegate info for given validator delegator pair.
         * @request GET:/cosmos/staking/v1beta1/validators/{validatorAddr}/delegations/{delegatorAddr}
         */
        this.queryDelegation = (validatorAddr, delegatorAddr, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/validators/${validatorAddr}/delegations/${delegatorAddr}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
       * No description
       *
       * @tags Query
       * @name QueryUnbondingDelegation
       * @summary UnbondingDelegation queries unbonding info for given validator delegator
      pair.
       * @request GET:/cosmos/staking/v1beta1/validators/{validatorAddr}/delegations/{delegatorAddr}/unbonding_delegation
       */
        this.queryUnbondingDelegation = (validatorAddr, delegatorAddr, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/validators/${validatorAddr}/delegations/${delegatorAddr}/unbonding_delegation`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryValidatorUnbondingDelegations
         * @summary ValidatorUnbondingDelegations queries unbonding delegations of a validator.
         * @request GET:/cosmos/staking/v1beta1/validators/{validatorAddr}/unbonding_delegations
         */
        this.queryValidatorUnbondingDelegations = (validatorAddr, query, params = {}) => this.request({
            path: `/cosmos/staking/v1beta1/validators/${validatorAddr}/unbonding_delegations`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
    }
}
