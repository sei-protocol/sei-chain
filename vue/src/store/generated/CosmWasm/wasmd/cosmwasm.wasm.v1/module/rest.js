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
* - ACCESS_TYPE_UNSPECIFIED: AccessTypeUnspecified placeholder for empty value
 - ACCESS_TYPE_NOBODY: AccessTypeNobody forbidden
 - ACCESS_TYPE_ONLY_ADDRESS: AccessTypeOnlyAddress restricted to an address
 - ACCESS_TYPE_EVERYBODY: AccessTypeEverybody unrestricted
*/
export var V1AccessType;
(function (V1AccessType) {
    V1AccessType["ACCESS_TYPE_UNSPECIFIED"] = "ACCESS_TYPE_UNSPECIFIED";
    V1AccessType["ACCESS_TYPE_NOBODY"] = "ACCESS_TYPE_NOBODY";
    V1AccessType["ACCESS_TYPE_ONLY_ADDRESS"] = "ACCESS_TYPE_ONLY_ADDRESS";
    V1AccessType["ACCESS_TYPE_EVERYBODY"] = "ACCESS_TYPE_EVERYBODY";
})(V1AccessType || (V1AccessType = {}));
/**
* - CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED: ContractCodeHistoryOperationTypeUnspecified placeholder for empty value
 - CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT: ContractCodeHistoryOperationTypeInit on chain contract instantiation
 - CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE: ContractCodeHistoryOperationTypeMigrate code migration
 - CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS: ContractCodeHistoryOperationTypeGenesis based on genesis data
*/
export var V1ContractCodeHistoryOperationType;
(function (V1ContractCodeHistoryOperationType) {
    V1ContractCodeHistoryOperationType["CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED"] = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED";
    V1ContractCodeHistoryOperationType["CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT"] = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT";
    V1ContractCodeHistoryOperationType["CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE"] = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE";
    V1ContractCodeHistoryOperationType["CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS"] = "CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS";
})(V1ContractCodeHistoryOperationType || (V1ContractCodeHistoryOperationType = {}));
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
 * @title cosmwasm/wasm/v1/genesis.proto
 * @version version not set
 */
export class Api extends HttpClient {
    constructor() {
        super(...arguments);
        /**
         * No description
         *
         * @tags Query
         * @name QueryCodes
         * @summary Codes gets the metadata for all stored wasm codes
         * @request GET:/cosmwasm/wasm/v1/code
         */
        this.queryCodes = (query, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/code`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryCode
         * @summary Code gets the binary code and metadata for a singe wasm code
         * @request GET:/cosmwasm/wasm/v1/code/{codeId}
         */
        this.queryCode = (codeId, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/code/${codeId}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryContractsByCode
         * @summary ContractsByCode lists all smart contracts for a code id
         * @request GET:/cosmwasm/wasm/v1/code/{codeId}/contracts
         */
        this.queryContractsByCode = (codeId, query, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/code/${codeId}/contracts`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryPinnedCodes
         * @summary PinnedCodes gets the pinned code ids
         * @request GET:/cosmwasm/wasm/v1/codes/pinned
         */
        this.queryPinnedCodes = (query, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/codes/pinned`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryContractInfo
         * @summary ContractInfo gets the contract meta data
         * @request GET:/cosmwasm/wasm/v1/contract/{address}
         */
        this.queryContractInfo = (address, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/contract/${address}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryContractHistory
         * @summary ContractHistory gets the contract code history
         * @request GET:/cosmwasm/wasm/v1/contract/{address}/history
         */
        this.queryContractHistory = (address, query, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/contract/${address}/history`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryRawContractState
         * @summary RawContractState gets single key from the raw store data of a contract
         * @request GET:/cosmwasm/wasm/v1/contract/{address}/raw/{queryData}
         */
        this.queryRawContractState = (address, queryData, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/contract/${address}/raw/${queryData}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QuerySmartContractState
         * @summary SmartContractState get smart query result from the contract
         * @request GET:/cosmwasm/wasm/v1/contract/{address}/smart/{queryData}
         */
        this.querySmartContractState = (address, queryData, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/contract/${address}/smart/${queryData}`,
            method: "GET",
            format: "json",
            ...params,
        });
        /**
         * No description
         *
         * @tags Query
         * @name QueryAllContractState
         * @summary AllContractState gets all raw store data for a single contract
         * @request GET:/cosmwasm/wasm/v1/contract/{address}/state
         */
        this.queryAllContractState = (address, query, params = {}) => this.request({
            path: `/cosmwasm/wasm/v1/contract/${address}/state`,
            method: "GET",
            query: query,
            format: "json",
            ...params,
        });
    }
}
