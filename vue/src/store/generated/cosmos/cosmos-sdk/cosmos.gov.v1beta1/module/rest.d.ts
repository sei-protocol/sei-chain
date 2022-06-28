/**
* `Any` contains an arbitrary serialized protocol buffer message along with a
URL that describes the type of the serialized message.

Protobuf library provides support to pack/unpack Any values in the form
of utility functions or additional generated methods of the Any type.

Example 1: Pack and unpack a message in C++.

    Foo foo = ...;
    Any any;
    any.PackFrom(foo);
    ...
    if (any.UnpackTo(&foo)) {
      ...
    }

Example 2: Pack and unpack a message in Java.

    Foo foo = ...;
    Any any = Any.pack(foo);
    ...
    if (any.is(Foo.class)) {
      foo = any.unpack(Foo.class);
    }

 Example 3: Pack and unpack a message in Python.

    foo = Foo(...)
    any = Any()
    any.Pack(foo)
    ...
    if any.Is(Foo.DESCRIPTOR):
      any.Unpack(foo)
      ...

 Example 4: Pack and unpack a message in Go

     foo := &pb.Foo{...}
     any, err := anypb.New(foo)
     if err != nil {
       ...
     }
     ...
     foo := &pb.Foo{}
     if err := any.UnmarshalTo(foo); err != nil {
       ...
     }

The pack methods provided by protobuf library will by default use
'type.googleapis.com/full.type.name' as the type URL and the unpack
methods only use the fully qualified type name after the last '/'
in the type URL, for example "foo.bar.com/x/y.z" will yield type
name "y.z".


JSON
====
The JSON representation of an `Any` value uses the regular
representation of the deserialized, embedded message, with an
additional field `@type` which contains the type URL. Example:

    package google.profile;
    message Person {
      string first_name = 1;
      string last_name = 2;
    }

    {
      "@type": "type.googleapis.com/google.profile.Person",
      "firstName": <string>,
      "lastName": <string>
    }

If the embedded message type is well-known and has a custom JSON
representation, that representation will be embedded adding a field
`value` which holds the custom JSON in addition to the `@type`
field. Example (for message [google.protobuf.Duration][]):

    {
      "@type": "type.googleapis.com/google.protobuf.Duration",
      "value": "1.212s"
    }
*/
export interface ProtobufAny {
    /**
     * A URL/resource name that uniquely identifies the type of the serialized
     * protocol buffer message. This string must contain at least
     * one "/" character. The last segment of the URL's path must represent
     * the fully qualified name of the type (as in
     * `path/google.protobuf.Duration`). The name should be in a canonical form
     * (e.g., leading "." is not accepted).
     *
     * In practice, teams usually precompile into the binary all types that they
     * expect it to use in the context of Any. However, for URLs which use the
     * scheme `http`, `https`, or no scheme, one can optionally set up a type
     * server that maps type URLs to message definitions as follows:
     *
     * * If no scheme is provided, `https` is assumed.
     * * An HTTP GET on the URL must yield a [google.protobuf.Type][]
     *   value in binary format, or produce an error.
     * * Applications are allowed to cache lookup results based on the
     *   URL, or have them precompiled into a binary to avoid any
     *   lookup. Therefore, binary compatibility needs to be preserved
     *   on changes to types. (Use versioned type names to manage
     *   breaking changes.)
     *
     * Note: this functionality is not currently available in the official
     * protobuf release, and it is not used for type URLs beginning with
     * type.googleapis.com.
     *
     * Schemes other than `http`, `https` (or the empty scheme) might be
     * used with implementation specific semantics.
     */
    "@type"?: string;
}
export interface RpcStatus {
    /** @format int32 */
    code?: number;
    message?: string;
    details?: ProtobufAny[];
}
/**
* Coin defines a token with a denomination and an amount.

NOTE: The amount field is an Int which implements the custom method
signatures required by gogoproto.
*/
export interface V1Beta1Coin {
    denom?: string;
    amount?: string;
}
/**
* Deposit defines an amount deposited by an account address to an active
proposal.
*/
export interface V1Beta1Deposit {
    /** @format uint64 */
    proposalId?: string;
    depositor?: string;
    amount?: V1Beta1Coin[];
}
/**
 * DepositParams defines the params for deposits on governance proposals.
 */
export interface V1Beta1DepositParams {
    /** Minimum deposit for a proposal to enter voting period. */
    minDeposit?: V1Beta1Coin[];
    /**
     * Maximum period for Atom holders to deposit on a proposal. Initial value: 2
     *  months.
     */
    maxDepositPeriod?: string;
}
/**
 * MsgDepositResponse defines the Msg/Deposit response type.
 */
export declare type V1Beta1MsgDepositResponse = object;
/**
 * MsgSubmitProposalResponse defines the Msg/SubmitProposal response type.
 */
export interface V1Beta1MsgSubmitProposalResponse {
    /** @format uint64 */
    proposalId?: string;
}
/**
 * MsgVoteResponse defines the Msg/Vote response type.
 */
export declare type V1Beta1MsgVoteResponse = object;
/**
* MsgVoteWeightedResponse defines the Msg/VoteWeighted response type.

Since: cosmos-sdk 0.43
*/
export declare type V1Beta1MsgVoteWeightedResponse = object;
/**
* message SomeRequest {
         Foo some_parameter = 1;
         PageRequest pagination = 2;
 }
*/
export interface V1Beta1PageRequest {
    /**
     * key is a value returned in PageResponse.next_key to begin
     * querying the next page most efficiently. Only one of offset or key
     * should be set.
     * @format byte
     */
    key?: string;
    /**
     * offset is a numeric offset that can be used when key is unavailable.
     * It is less efficient than using key. Only one of offset or key should
     * be set.
     * @format uint64
     */
    offset?: string;
    /**
     * limit is the total number of results to be returned in the result page.
     * If left empty it will default to a value to be set by each app.
     * @format uint64
     */
    limit?: string;
    /**
     * count_total is set to true  to indicate that the result set should include
     * a count of the total number of items available for pagination in UIs.
     * count_total is only respected when offset is used. It is ignored when key
     * is set.
     */
    countTotal?: boolean;
    /**
     * reverse is set to true if results are to be returned in the descending order.
     *
     * Since: cosmos-sdk 0.43
     */
    reverse?: boolean;
}
/**
* PageResponse is to be embedded in gRPC response messages where the
corresponding request message has used PageRequest.

 message SomeResponse {
         repeated Bar results = 1;
         PageResponse page = 2;
 }
*/
export interface V1Beta1PageResponse {
    /** @format byte */
    nextKey?: string;
    /** @format uint64 */
    total?: string;
}
/**
 * Proposal defines the core field members of a governance proposal.
 */
export interface V1Beta1Proposal {
    /** @format uint64 */
    proposalId?: string;
    /**
     * `Any` contains an arbitrary serialized protocol buffer message along with a
     * URL that describes the type of the serialized message.
     *
     * Protobuf library provides support to pack/unpack Any values in the form
     * of utility functions or additional generated methods of the Any type.
     *
     * Example 1: Pack and unpack a message in C++.
     *
     *     Foo foo = ...;
     *     Any any;
     *     any.PackFrom(foo);
     *     ...
     *     if (any.UnpackTo(&foo)) {
     *       ...
     *     }
     *
     * Example 2: Pack and unpack a message in Java.
     *
     *     Foo foo = ...;
     *     Any any = Any.pack(foo);
     *     ...
     *     if (any.is(Foo.class)) {
     *       foo = any.unpack(Foo.class);
     *     }
     *
     *  Example 3: Pack and unpack a message in Python.
     *
     *     foo = Foo(...)
     *     any = Any()
     *     any.Pack(foo)
     *     ...
     *     if any.Is(Foo.DESCRIPTOR):
     *       any.Unpack(foo)
     *       ...
     *
     *  Example 4: Pack and unpack a message in Go
     *
     *      foo := &pb.Foo{...}
     *      any, err := anypb.New(foo)
     *      if err != nil {
     *        ...
     *      }
     *      ...
     *      foo := &pb.Foo{}
     *      if err := any.UnmarshalTo(foo); err != nil {
     *        ...
     *      }
     *
     * The pack methods provided by protobuf library will by default use
     * 'type.googleapis.com/full.type.name' as the type URL and the unpack
     * methods only use the fully qualified type name after the last '/'
     * in the type URL, for example "foo.bar.com/x/y.z" will yield type
     * name "y.z".
     *
     *
     * JSON
     * ====
     * The JSON representation of an `Any` value uses the regular
     * representation of the deserialized, embedded message, with an
     * additional field `@type` which contains the type URL. Example:
     *
     *     package google.profile;
     *     message Person {
     *       string first_name = 1;
     *       string last_name = 2;
     *     }
     *
     *     {
     *       "@type": "type.googleapis.com/google.profile.Person",
     *       "firstName": <string>,
     *       "lastName": <string>
     *     }
     *
     * If the embedded message type is well-known and has a custom JSON
     * representation, that representation will be embedded adding a field
     * `value` which holds the custom JSON in addition to the `@type`
     * field. Example (for message [google.protobuf.Duration][]):
     *
     *     {
     *       "@type": "type.googleapis.com/google.protobuf.Duration",
     *       "value": "1.212s"
     *     }
     */
    content?: ProtobufAny;
    /**
     * ProposalStatus enumerates the valid statuses of a proposal.
     *
     *  - PROPOSAL_STATUS_UNSPECIFIED: PROPOSAL_STATUS_UNSPECIFIED defines the default propopsal status.
     *  - PROPOSAL_STATUS_DEPOSIT_PERIOD: PROPOSAL_STATUS_DEPOSIT_PERIOD defines a proposal status during the deposit
     * period.
     *  - PROPOSAL_STATUS_VOTING_PERIOD: PROPOSAL_STATUS_VOTING_PERIOD defines a proposal status during the voting
     * period.
     *  - PROPOSAL_STATUS_PASSED: PROPOSAL_STATUS_PASSED defines a proposal status of a proposal that has
     * passed.
     *  - PROPOSAL_STATUS_REJECTED: PROPOSAL_STATUS_REJECTED defines a proposal status of a proposal that has
     * been rejected.
     *  - PROPOSAL_STATUS_FAILED: PROPOSAL_STATUS_FAILED defines a proposal status of a proposal that has
     * failed.
     */
    status?: V1Beta1ProposalStatus;
    /** TallyResult defines a standard tally for a governance proposal. */
    finalTallyResult?: V1Beta1TallyResult;
    /** @format date-time */
    submitTime?: string;
    /** @format date-time */
    depositEndTime?: string;
    totalDeposit?: V1Beta1Coin[];
    /** @format date-time */
    votingStartTime?: string;
    /** @format date-time */
    votingEndTime?: string;
}
/**
* ProposalStatus enumerates the valid statuses of a proposal.

 - PROPOSAL_STATUS_UNSPECIFIED: PROPOSAL_STATUS_UNSPECIFIED defines the default propopsal status.
 - PROPOSAL_STATUS_DEPOSIT_PERIOD: PROPOSAL_STATUS_DEPOSIT_PERIOD defines a proposal status during the deposit
period.
 - PROPOSAL_STATUS_VOTING_PERIOD: PROPOSAL_STATUS_VOTING_PERIOD defines a proposal status during the voting
period.
 - PROPOSAL_STATUS_PASSED: PROPOSAL_STATUS_PASSED defines a proposal status of a proposal that has
passed.
 - PROPOSAL_STATUS_REJECTED: PROPOSAL_STATUS_REJECTED defines a proposal status of a proposal that has
been rejected.
 - PROPOSAL_STATUS_FAILED: PROPOSAL_STATUS_FAILED defines a proposal status of a proposal that has
failed.
*/
export declare enum V1Beta1ProposalStatus {
    PROPOSAL_STATUS_UNSPECIFIED = "PROPOSAL_STATUS_UNSPECIFIED",
    PROPOSAL_STATUS_DEPOSIT_PERIOD = "PROPOSAL_STATUS_DEPOSIT_PERIOD",
    PROPOSAL_STATUS_VOTING_PERIOD = "PROPOSAL_STATUS_VOTING_PERIOD",
    PROPOSAL_STATUS_PASSED = "PROPOSAL_STATUS_PASSED",
    PROPOSAL_STATUS_REJECTED = "PROPOSAL_STATUS_REJECTED",
    PROPOSAL_STATUS_FAILED = "PROPOSAL_STATUS_FAILED"
}
/**
 * QueryDepositResponse is the response type for the Query/Deposit RPC method.
 */
export interface V1Beta1QueryDepositResponse {
    /** deposit defines the requested deposit. */
    deposit?: V1Beta1Deposit;
}
/**
 * QueryDepositsResponse is the response type for the Query/Deposits RPC method.
 */
export interface V1Beta1QueryDepositsResponse {
    deposits?: V1Beta1Deposit[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
 * QueryParamsResponse is the response type for the Query/Params RPC method.
 */
export interface V1Beta1QueryParamsResponse {
    /** voting_params defines the parameters related to voting. */
    votingParams?: V1Beta1VotingParams;
    /** deposit_params defines the parameters related to deposit. */
    depositParams?: V1Beta1DepositParams;
    /** tally_params defines the parameters related to tally. */
    tallyParams?: V1Beta1TallyParams;
}
/**
 * QueryProposalResponse is the response type for the Query/Proposal RPC method.
 */
export interface V1Beta1QueryProposalResponse {
    /** Proposal defines the core field members of a governance proposal. */
    proposal?: V1Beta1Proposal;
}
/**
* QueryProposalsResponse is the response type for the Query/Proposals RPC
method.
*/
export interface V1Beta1QueryProposalsResponse {
    proposals?: V1Beta1Proposal[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
 * QueryTallyResultResponse is the response type for the Query/Tally RPC method.
 */
export interface V1Beta1QueryTallyResultResponse {
    /** tally defines the requested tally. */
    tally?: V1Beta1TallyResult;
}
/**
 * QueryVoteResponse is the response type for the Query/Vote RPC method.
 */
export interface V1Beta1QueryVoteResponse {
    /** vote defined the queried vote. */
    vote?: V1Beta1Vote;
}
/**
 * QueryVotesResponse is the response type for the Query/Votes RPC method.
 */
export interface V1Beta1QueryVotesResponse {
    /** votes defined the queried votes. */
    votes?: V1Beta1Vote[];
    /** pagination defines the pagination in the response. */
    pagination?: V1Beta1PageResponse;
}
/**
 * TallyParams defines the params for tallying votes on governance proposals.
 */
export interface V1Beta1TallyParams {
    /**
     * Minimum percentage of total stake needed to vote for a result to be
     *  considered valid.
     * @format byte
     */
    quorum?: string;
    /**
     * Minimum proportion of Yes votes for proposal to pass. Default value: 0.5.
     * @format byte
     */
    threshold?: string;
    /**
     * Minimum value of Veto votes to Total votes ratio for proposal to be
     *  vetoed. Default value: 1/3.
     * @format byte
     */
    vetoThreshold?: string;
}
/**
 * TallyResult defines a standard tally for a governance proposal.
 */
export interface V1Beta1TallyResult {
    yes?: string;
    abstain?: string;
    no?: string;
    noWithVeto?: string;
}
/**
* Vote defines a vote on a governance proposal.
A Vote consists of a proposal ID, the voter, and the vote option.
*/
export interface V1Beta1Vote {
    /** @format uint64 */
    proposalId?: string;
    voter?: string;
    /**
     * Deprecated: Prefer to use `options` instead. This field is set in queries
     * if and only if `len(options) == 1` and that option has weight 1. In all
     * other cases, this field will default to VOTE_OPTION_UNSPECIFIED.
     */
    option?: V1Beta1VoteOption;
    options?: V1Beta1WeightedVoteOption[];
}
/**
* VoteOption enumerates the valid vote options for a given governance proposal.

 - VOTE_OPTION_UNSPECIFIED: VOTE_OPTION_UNSPECIFIED defines a no-op vote option.
 - VOTE_OPTION_YES: VOTE_OPTION_YES defines a yes vote option.
 - VOTE_OPTION_ABSTAIN: VOTE_OPTION_ABSTAIN defines an abstain vote option.
 - VOTE_OPTION_NO: VOTE_OPTION_NO defines a no vote option.
 - VOTE_OPTION_NO_WITH_VETO: VOTE_OPTION_NO_WITH_VETO defines a no with veto vote option.
*/
export declare enum V1Beta1VoteOption {
    VOTE_OPTION_UNSPECIFIED = "VOTE_OPTION_UNSPECIFIED",
    VOTE_OPTION_YES = "VOTE_OPTION_YES",
    VOTE_OPTION_ABSTAIN = "VOTE_OPTION_ABSTAIN",
    VOTE_OPTION_NO = "VOTE_OPTION_NO",
    VOTE_OPTION_NO_WITH_VETO = "VOTE_OPTION_NO_WITH_VETO"
}
/**
 * VotingParams defines the params for voting on governance proposals.
 */
export interface V1Beta1VotingParams {
    /** Length of the voting period. */
    votingPeriod?: string;
}
/**
* WeightedVoteOption defines a unit of vote for vote split.

Since: cosmos-sdk 0.43
*/
export interface V1Beta1WeightedVoteOption {
    /**
     * VoteOption enumerates the valid vote options for a given governance proposal.
     *
     *  - VOTE_OPTION_UNSPECIFIED: VOTE_OPTION_UNSPECIFIED defines a no-op vote option.
     *  - VOTE_OPTION_YES: VOTE_OPTION_YES defines a yes vote option.
     *  - VOTE_OPTION_ABSTAIN: VOTE_OPTION_ABSTAIN defines an abstain vote option.
     *  - VOTE_OPTION_NO: VOTE_OPTION_NO defines a no vote option.
     *  - VOTE_OPTION_NO_WITH_VETO: VOTE_OPTION_NO_WITH_VETO defines a no with veto vote option.
     */
    option?: V1Beta1VoteOption;
    weight?: string;
}
export declare type QueryParamsType = Record<string | number, any>;
export declare type ResponseFormat = keyof Omit<Body, "body" | "bodyUsed">;
export interface FullRequestParams extends Omit<RequestInit, "body"> {
    /** set parameter to `true` for call `securityWorker` for this request */
    secure?: boolean;
    /** request path */
    path: string;
    /** content type of request body */
    type?: ContentType;
    /** query params */
    query?: QueryParamsType;
    /** format of response (i.e. response.json() -> format: "json") */
    format?: keyof Omit<Body, "body" | "bodyUsed">;
    /** request body */
    body?: unknown;
    /** base url */
    baseUrl?: string;
    /** request cancellation token */
    cancelToken?: CancelToken;
}
export declare type RequestParams = Omit<FullRequestParams, "body" | "method" | "query" | "path">;
export interface ApiConfig<SecurityDataType = unknown> {
    baseUrl?: string;
    baseApiParams?: Omit<RequestParams, "baseUrl" | "cancelToken" | "signal">;
    securityWorker?: (securityData: SecurityDataType) => RequestParams | void;
}
export interface HttpResponse<D extends unknown, E extends unknown = unknown> extends Response {
    data: D;
    error: E;
}
declare type CancelToken = Symbol | string | number;
export declare enum ContentType {
    Json = "application/json",
    FormData = "multipart/form-data",
    UrlEncoded = "application/x-www-form-urlencoded"
}
export declare class HttpClient<SecurityDataType = unknown> {
    baseUrl: string;
    private securityData;
    private securityWorker;
    private abortControllers;
    private baseApiParams;
    constructor(apiConfig?: ApiConfig<SecurityDataType>);
    setSecurityData: (data: SecurityDataType) => void;
    private addQueryParam;
    protected toQueryString(rawQuery?: QueryParamsType): string;
    protected addQueryParams(rawQuery?: QueryParamsType): string;
    private contentFormatters;
    private mergeRequestParams;
    private createAbortSignal;
    abortRequest: (cancelToken: CancelToken) => void;
    request: <T = any, E = any>({ body, secure, path, type, query, format, baseUrl, cancelToken, ...params }: FullRequestParams) => Promise<HttpResponse<T, E>>;
}
/**
 * @title cosmos/gov/v1beta1/genesis.proto
 * @version version not set
 */
export declare class Api<SecurityDataType extends unknown> extends HttpClient<SecurityDataType> {
    /**
     * No description
     *
     * @tags Query
     * @name QueryParams
     * @summary Params queries all parameters of the gov module.
     * @request GET:/cosmos/gov/v1beta1/params/{paramsType}
     */
    queryParams: (paramsType: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryParamsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryProposals
     * @summary Proposals queries all proposals based on given status.
     * @request GET:/cosmos/gov/v1beta1/proposals
     */
    queryProposals: (query?: {
        proposalStatus?: "PROPOSAL_STATUS_UNSPECIFIED" | "PROPOSAL_STATUS_DEPOSIT_PERIOD" | "PROPOSAL_STATUS_VOTING_PERIOD" | "PROPOSAL_STATUS_PASSED" | "PROPOSAL_STATUS_REJECTED" | "PROPOSAL_STATUS_FAILED";
        voter?: string;
        depositor?: string;
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryProposalsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryProposal
     * @summary Proposal queries proposal details based on ProposalID.
     * @request GET:/cosmos/gov/v1beta1/proposals/{proposalId}
     */
    queryProposal: (proposalId: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryProposalResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDeposits
     * @summary Deposits queries all deposits of a single proposal.
     * @request GET:/cosmos/gov/v1beta1/proposals/{proposalId}/deposits
     */
    queryDeposits: (proposalId: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDepositsResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryDeposit
     * @summary Deposit queries single deposit information based proposalID, depositAddr.
     * @request GET:/cosmos/gov/v1beta1/proposals/{proposalId}/deposits/{depositor}
     */
    queryDeposit: (proposalId: string, depositor: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryDepositResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryTallyResult
     * @summary TallyResult queries the tally of a proposal vote.
     * @request GET:/cosmos/gov/v1beta1/proposals/{proposalId}/tally
     */
    queryTallyResult: (proposalId: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryTallyResultResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryVotes
     * @summary Votes queries votes of a given proposal.
     * @request GET:/cosmos/gov/v1beta1/proposals/{proposalId}/votes
     */
    queryVotes: (proposalId: string, query?: {
        "pagination.key"?: string;
        "pagination.offset"?: string;
        "pagination.limit"?: string;
        "pagination.countTotal"?: boolean;
        "pagination.reverse"?: boolean;
    }, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryVotesResponse, RpcStatus>>;
    /**
     * No description
     *
     * @tags Query
     * @name QueryVote
     * @summary Vote queries voted information based on proposalID, voterAddr.
     * @request GET:/cosmos/gov/v1beta1/proposals/{proposalId}/votes/{voter}
     */
    queryVote: (proposalId: string, voter: string, params?: RequestParams) => Promise<HttpResponse<V1Beta1QueryVoteResponse, RpcStatus>>;
}
export {};
