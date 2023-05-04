/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { DecCoin, Coin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.distribution.v1beta1";

/** Params defines the set of params for the distribution module. */
export interface Params {
  community_tax: string;
  base_proposer_reward: string;
  bonus_proposer_reward: string;
  withdraw_addr_enabled: boolean;
}

/**
 * ValidatorHistoricalRewards represents historical rewards for a validator.
 * Height is implicit within the store key.
 * Cumulative reward ratio is the sum from the zeroeth period
 * until this period of rewards / tokens, per the spec.
 * The reference count indicates the number of objects
 * which might need to reference this historical entry at any point.
 * ReferenceCount =
 *    number of outstanding delegations which ended the associated period (and
 *    might need to read that record)
 *  + number of slashes which ended the associated period (and might need to
 *  read that record)
 *  + one per validator for the zeroeth period, set on initialization
 */
export interface ValidatorHistoricalRewards {
  cumulative_reward_ratio: DecCoin[];
  reference_count: number;
}

/**
 * ValidatorCurrentRewards represents current rewards and current
 * period for a validator kept as a running counter and incremented
 * each block as long as the validator's tokens remain constant.
 */
export interface ValidatorCurrentRewards {
  rewards: DecCoin[];
  period: number;
}

/**
 * ValidatorAccumulatedCommission represents accumulated commission
 * for a validator kept as a running counter, can be withdrawn at any time.
 */
export interface ValidatorAccumulatedCommission {
  commission: DecCoin[];
}

/**
 * ValidatorOutstandingRewards represents outstanding (un-withdrawn) rewards
 * for a validator inexpensive to track, allows simple sanity checks.
 */
export interface ValidatorOutstandingRewards {
  rewards: DecCoin[];
}

/**
 * ValidatorSlashEvent represents a validator slash event.
 * Height is implicit within the store key.
 * This is needed to calculate appropriate amount of staking tokens
 * for delegations which are withdrawn after a slash has occurred.
 */
export interface ValidatorSlashEvent {
  validator_period: number;
  fraction: string;
}

/** ValidatorSlashEvents is a collection of ValidatorSlashEvent messages. */
export interface ValidatorSlashEvents {
  validator_slash_events: ValidatorSlashEvent[];
}

/** FeePool is the global fee pool for distribution. */
export interface FeePool {
  community_pool: DecCoin[];
}

/**
 * CommunityPoolSpendProposal details a proposal for use of community funds,
 * together with how many coins are proposed to be spent, and to which
 * recipient account.
 */
export interface CommunityPoolSpendProposal {
  title: string;
  description: string;
  recipient: string;
  amount: Coin[];
}

/**
 * DelegatorStartingInfo represents the starting info for a delegator reward
 * period. It tracks the previous validator period, the delegation's amount of
 * staking token, and the creation height (to check later on if any slashes have
 * occurred). NOTE: Even though validators are slashed to whole staking tokens,
 * the delegators within the validator may be left with less than a full token,
 * thus sdk.Dec is used.
 */
export interface DelegatorStartingInfo {
  previous_period: number;
  stake: string;
  height: number;
}

/**
 * DelegationDelegatorReward represents the properties
 * of a delegator's delegation reward.
 */
export interface DelegationDelegatorReward {
  validator_address: string;
  reward: DecCoin[];
}

/**
 * CommunityPoolSpendProposalWithDeposit defines a CommunityPoolSpendProposal
 * with a deposit
 */
export interface CommunityPoolSpendProposalWithDeposit {
  title: string;
  description: string;
  recipient: string;
  amount: string;
  deposit: string;
}

const baseParams: object = {
  community_tax: "",
  base_proposer_reward: "",
  bonus_proposer_reward: "",
  withdraw_addr_enabled: false,
};

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.community_tax !== "") {
      writer.uint32(10).string(message.community_tax);
    }
    if (message.base_proposer_reward !== "") {
      writer.uint32(18).string(message.base_proposer_reward);
    }
    if (message.bonus_proposer_reward !== "") {
      writer.uint32(26).string(message.bonus_proposer_reward);
    }
    if (message.withdraw_addr_enabled === true) {
      writer.uint32(32).bool(message.withdraw_addr_enabled);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Params {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseParams } as Params;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.community_tax = reader.string();
          break;
        case 2:
          message.base_proposer_reward = reader.string();
          break;
        case 3:
          message.bonus_proposer_reward = reader.string();
          break;
        case 4:
          message.withdraw_addr_enabled = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Params {
    const message = { ...baseParams } as Params;
    if (object.community_tax !== undefined && object.community_tax !== null) {
      message.community_tax = String(object.community_tax);
    } else {
      message.community_tax = "";
    }
    if (
      object.base_proposer_reward !== undefined &&
      object.base_proposer_reward !== null
    ) {
      message.base_proposer_reward = String(object.base_proposer_reward);
    } else {
      message.base_proposer_reward = "";
    }
    if (
      object.bonus_proposer_reward !== undefined &&
      object.bonus_proposer_reward !== null
    ) {
      message.bonus_proposer_reward = String(object.bonus_proposer_reward);
    } else {
      message.bonus_proposer_reward = "";
    }
    if (
      object.withdraw_addr_enabled !== undefined &&
      object.withdraw_addr_enabled !== null
    ) {
      message.withdraw_addr_enabled = Boolean(object.withdraw_addr_enabled);
    } else {
      message.withdraw_addr_enabled = false;
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.community_tax !== undefined &&
      (obj.community_tax = message.community_tax);
    message.base_proposer_reward !== undefined &&
      (obj.base_proposer_reward = message.base_proposer_reward);
    message.bonus_proposer_reward !== undefined &&
      (obj.bonus_proposer_reward = message.bonus_proposer_reward);
    message.withdraw_addr_enabled !== undefined &&
      (obj.withdraw_addr_enabled = message.withdraw_addr_enabled);
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (object.community_tax !== undefined && object.community_tax !== null) {
      message.community_tax = object.community_tax;
    } else {
      message.community_tax = "";
    }
    if (
      object.base_proposer_reward !== undefined &&
      object.base_proposer_reward !== null
    ) {
      message.base_proposer_reward = object.base_proposer_reward;
    } else {
      message.base_proposer_reward = "";
    }
    if (
      object.bonus_proposer_reward !== undefined &&
      object.bonus_proposer_reward !== null
    ) {
      message.bonus_proposer_reward = object.bonus_proposer_reward;
    } else {
      message.bonus_proposer_reward = "";
    }
    if (
      object.withdraw_addr_enabled !== undefined &&
      object.withdraw_addr_enabled !== null
    ) {
      message.withdraw_addr_enabled = object.withdraw_addr_enabled;
    } else {
      message.withdraw_addr_enabled = false;
    }
    return message;
  },
};

const baseValidatorHistoricalRewards: object = { reference_count: 0 };

export const ValidatorHistoricalRewards = {
  encode(
    message: ValidatorHistoricalRewards,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.cumulative_reward_ratio) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.reference_count !== 0) {
      writer.uint32(16).uint32(message.reference_count);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorHistoricalRewards {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorHistoricalRewards,
    } as ValidatorHistoricalRewards;
    message.cumulative_reward_ratio = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.cumulative_reward_ratio.push(
            DecCoin.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.reference_count = reader.uint32();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorHistoricalRewards {
    const message = {
      ...baseValidatorHistoricalRewards,
    } as ValidatorHistoricalRewards;
    message.cumulative_reward_ratio = [];
    if (
      object.cumulative_reward_ratio !== undefined &&
      object.cumulative_reward_ratio !== null
    ) {
      for (const e of object.cumulative_reward_ratio) {
        message.cumulative_reward_ratio.push(DecCoin.fromJSON(e));
      }
    }
    if (
      object.reference_count !== undefined &&
      object.reference_count !== null
    ) {
      message.reference_count = Number(object.reference_count);
    } else {
      message.reference_count = 0;
    }
    return message;
  },

  toJSON(message: ValidatorHistoricalRewards): unknown {
    const obj: any = {};
    if (message.cumulative_reward_ratio) {
      obj.cumulative_reward_ratio = message.cumulative_reward_ratio.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.cumulative_reward_ratio = [];
    }
    message.reference_count !== undefined &&
      (obj.reference_count = message.reference_count);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorHistoricalRewards>
  ): ValidatorHistoricalRewards {
    const message = {
      ...baseValidatorHistoricalRewards,
    } as ValidatorHistoricalRewards;
    message.cumulative_reward_ratio = [];
    if (
      object.cumulative_reward_ratio !== undefined &&
      object.cumulative_reward_ratio !== null
    ) {
      for (const e of object.cumulative_reward_ratio) {
        message.cumulative_reward_ratio.push(DecCoin.fromPartial(e));
      }
    }
    if (
      object.reference_count !== undefined &&
      object.reference_count !== null
    ) {
      message.reference_count = object.reference_count;
    } else {
      message.reference_count = 0;
    }
    return message;
  },
};

const baseValidatorCurrentRewards: object = { period: 0 };

export const ValidatorCurrentRewards = {
  encode(
    message: ValidatorCurrentRewards,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.rewards) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.period !== 0) {
      writer.uint32(16).uint64(message.period);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorCurrentRewards {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorCurrentRewards,
    } as ValidatorCurrentRewards;
    message.rewards = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rewards.push(DecCoin.decode(reader, reader.uint32()));
          break;
        case 2:
          message.period = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorCurrentRewards {
    const message = {
      ...baseValidatorCurrentRewards,
    } as ValidatorCurrentRewards;
    message.rewards = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DecCoin.fromJSON(e));
      }
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = Number(object.period);
    } else {
      message.period = 0;
    }
    return message;
  },

  toJSON(message: ValidatorCurrentRewards): unknown {
    const obj: any = {};
    if (message.rewards) {
      obj.rewards = message.rewards.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.rewards = [];
    }
    message.period !== undefined && (obj.period = message.period);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorCurrentRewards>
  ): ValidatorCurrentRewards {
    const message = {
      ...baseValidatorCurrentRewards,
    } as ValidatorCurrentRewards;
    message.rewards = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DecCoin.fromPartial(e));
      }
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = object.period;
    } else {
      message.period = 0;
    }
    return message;
  },
};

const baseValidatorAccumulatedCommission: object = {};

export const ValidatorAccumulatedCommission = {
  encode(
    message: ValidatorAccumulatedCommission,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.commission) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorAccumulatedCommission {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorAccumulatedCommission,
    } as ValidatorAccumulatedCommission;
    message.commission = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.commission.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorAccumulatedCommission {
    const message = {
      ...baseValidatorAccumulatedCommission,
    } as ValidatorAccumulatedCommission;
    message.commission = [];
    if (object.commission !== undefined && object.commission !== null) {
      for (const e of object.commission) {
        message.commission.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorAccumulatedCommission): unknown {
    const obj: any = {};
    if (message.commission) {
      obj.commission = message.commission.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.commission = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorAccumulatedCommission>
  ): ValidatorAccumulatedCommission {
    const message = {
      ...baseValidatorAccumulatedCommission,
    } as ValidatorAccumulatedCommission;
    message.commission = [];
    if (object.commission !== undefined && object.commission !== null) {
      for (const e of object.commission) {
        message.commission.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseValidatorOutstandingRewards: object = {};

export const ValidatorOutstandingRewards = {
  encode(
    message: ValidatorOutstandingRewards,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.rewards) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorOutstandingRewards {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorOutstandingRewards,
    } as ValidatorOutstandingRewards;
    message.rewards = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.rewards.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorOutstandingRewards {
    const message = {
      ...baseValidatorOutstandingRewards,
    } as ValidatorOutstandingRewards;
    message.rewards = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorOutstandingRewards): unknown {
    const obj: any = {};
    if (message.rewards) {
      obj.rewards = message.rewards.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.rewards = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorOutstandingRewards>
  ): ValidatorOutstandingRewards {
    const message = {
      ...baseValidatorOutstandingRewards,
    } as ValidatorOutstandingRewards;
    message.rewards = [];
    if (object.rewards !== undefined && object.rewards !== null) {
      for (const e of object.rewards) {
        message.rewards.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseValidatorSlashEvent: object = { validator_period: 0, fraction: "" };

export const ValidatorSlashEvent = {
  encode(
    message: ValidatorSlashEvent,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validator_period !== 0) {
      writer.uint32(8).uint64(message.validator_period);
    }
    if (message.fraction !== "") {
      writer.uint32(18).string(message.fraction);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorSlashEvent {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorSlashEvent } as ValidatorSlashEvent;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_period = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.fraction = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorSlashEvent {
    const message = { ...baseValidatorSlashEvent } as ValidatorSlashEvent;
    if (
      object.validator_period !== undefined &&
      object.validator_period !== null
    ) {
      message.validator_period = Number(object.validator_period);
    } else {
      message.validator_period = 0;
    }
    if (object.fraction !== undefined && object.fraction !== null) {
      message.fraction = String(object.fraction);
    } else {
      message.fraction = "";
    }
    return message;
  },

  toJSON(message: ValidatorSlashEvent): unknown {
    const obj: any = {};
    message.validator_period !== undefined &&
      (obj.validator_period = message.validator_period);
    message.fraction !== undefined && (obj.fraction = message.fraction);
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorSlashEvent>): ValidatorSlashEvent {
    const message = { ...baseValidatorSlashEvent } as ValidatorSlashEvent;
    if (
      object.validator_period !== undefined &&
      object.validator_period !== null
    ) {
      message.validator_period = object.validator_period;
    } else {
      message.validator_period = 0;
    }
    if (object.fraction !== undefined && object.fraction !== null) {
      message.fraction = object.fraction;
    } else {
      message.fraction = "";
    }
    return message;
  },
};

const baseValidatorSlashEvents: object = {};

export const ValidatorSlashEvents = {
  encode(
    message: ValidatorSlashEvents,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.validator_slash_events) {
      ValidatorSlashEvent.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorSlashEvents {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorSlashEvents } as ValidatorSlashEvents;
    message.validator_slash_events = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_slash_events.push(
            ValidatorSlashEvent.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorSlashEvents {
    const message = { ...baseValidatorSlashEvents } as ValidatorSlashEvents;
    message.validator_slash_events = [];
    if (
      object.validator_slash_events !== undefined &&
      object.validator_slash_events !== null
    ) {
      for (const e of object.validator_slash_events) {
        message.validator_slash_events.push(ValidatorSlashEvent.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorSlashEvents): unknown {
    const obj: any = {};
    if (message.validator_slash_events) {
      obj.validator_slash_events = message.validator_slash_events.map((e) =>
        e ? ValidatorSlashEvent.toJSON(e) : undefined
      );
    } else {
      obj.validator_slash_events = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorSlashEvents>): ValidatorSlashEvents {
    const message = { ...baseValidatorSlashEvents } as ValidatorSlashEvents;
    message.validator_slash_events = [];
    if (
      object.validator_slash_events !== undefined &&
      object.validator_slash_events !== null
    ) {
      for (const e of object.validator_slash_events) {
        message.validator_slash_events.push(ValidatorSlashEvent.fromPartial(e));
      }
    }
    return message;
  },
};

const baseFeePool: object = {};

export const FeePool = {
  encode(message: FeePool, writer: Writer = Writer.create()): Writer {
    for (const v of message.community_pool) {
      DecCoin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FeePool {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFeePool } as FeePool;
    message.community_pool = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.community_pool.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FeePool {
    const message = { ...baseFeePool } as FeePool;
    message.community_pool = [];
    if (object.community_pool !== undefined && object.community_pool !== null) {
      for (const e of object.community_pool) {
        message.community_pool.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: FeePool): unknown {
    const obj: any = {};
    if (message.community_pool) {
      obj.community_pool = message.community_pool.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.community_pool = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<FeePool>): FeePool {
    const message = { ...baseFeePool } as FeePool;
    message.community_pool = [];
    if (object.community_pool !== undefined && object.community_pool !== null) {
      for (const e of object.community_pool) {
        message.community_pool.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseCommunityPoolSpendProposal: object = {
  title: "",
  description: "",
  recipient: "",
};

export const CommunityPoolSpendProposal = {
  encode(
    message: CommunityPoolSpendProposal,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.recipient !== "") {
      writer.uint32(26).string(message.recipient);
    }
    for (const v of message.amount) {
      Coin.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): CommunityPoolSpendProposal {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseCommunityPoolSpendProposal,
    } as CommunityPoolSpendProposal;
    message.amount = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.title = reader.string();
          break;
        case 2:
          message.description = reader.string();
          break;
        case 3:
          message.recipient = reader.string();
          break;
        case 4:
          message.amount.push(Coin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): CommunityPoolSpendProposal {
    const message = {
      ...baseCommunityPoolSpendProposal,
    } as CommunityPoolSpendProposal;
    message.amount = [];
    if (object.title !== undefined && object.title !== null) {
      message.title = String(object.title);
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = String(object.description);
    } else {
      message.description = "";
    }
    if (object.recipient !== undefined && object.recipient !== null) {
      message.recipient = String(object.recipient);
    } else {
      message.recipient = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: CommunityPoolSpendProposal): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.recipient !== undefined && (obj.recipient = message.recipient);
    if (message.amount) {
      obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.amount = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<CommunityPoolSpendProposal>
  ): CommunityPoolSpendProposal {
    const message = {
      ...baseCommunityPoolSpendProposal,
    } as CommunityPoolSpendProposal;
    message.amount = [];
    if (object.title !== undefined && object.title !== null) {
      message.title = object.title;
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = object.description;
    } else {
      message.description = "";
    }
    if (object.recipient !== undefined && object.recipient !== null) {
      message.recipient = object.recipient;
    } else {
      message.recipient = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseDelegatorStartingInfo: object = {
  previous_period: 0,
  stake: "",
  height: 0,
};

export const DelegatorStartingInfo = {
  encode(
    message: DelegatorStartingInfo,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.previous_period !== 0) {
      writer.uint32(8).uint64(message.previous_period);
    }
    if (message.stake !== "") {
      writer.uint32(18).string(message.stake);
    }
    if (message.height !== 0) {
      writer.uint32(24).uint64(message.height);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DelegatorStartingInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDelegatorStartingInfo } as DelegatorStartingInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.previous_period = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.stake = reader.string();
          break;
        case 3:
          message.height = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DelegatorStartingInfo {
    const message = { ...baseDelegatorStartingInfo } as DelegatorStartingInfo;
    if (
      object.previous_period !== undefined &&
      object.previous_period !== null
    ) {
      message.previous_period = Number(object.previous_period);
    } else {
      message.previous_period = 0;
    }
    if (object.stake !== undefined && object.stake !== null) {
      message.stake = String(object.stake);
    } else {
      message.stake = "";
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    return message;
  },

  toJSON(message: DelegatorStartingInfo): unknown {
    const obj: any = {};
    message.previous_period !== undefined &&
      (obj.previous_period = message.previous_period);
    message.stake !== undefined && (obj.stake = message.stake);
    message.height !== undefined && (obj.height = message.height);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DelegatorStartingInfo>
  ): DelegatorStartingInfo {
    const message = { ...baseDelegatorStartingInfo } as DelegatorStartingInfo;
    if (
      object.previous_period !== undefined &&
      object.previous_period !== null
    ) {
      message.previous_period = object.previous_period;
    } else {
      message.previous_period = 0;
    }
    if (object.stake !== undefined && object.stake !== null) {
      message.stake = object.stake;
    } else {
      message.stake = "";
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    return message;
  },
};

const baseDelegationDelegatorReward: object = { validator_address: "" };

export const DelegationDelegatorReward = {
  encode(
    message: DelegationDelegatorReward,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validator_address !== "") {
      writer.uint32(10).string(message.validator_address);
    }
    for (const v of message.reward) {
      DecCoin.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): DelegationDelegatorReward {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseDelegationDelegatorReward,
    } as DelegationDelegatorReward;
    message.reward = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_address = reader.string();
          break;
        case 2:
          message.reward.push(DecCoin.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DelegationDelegatorReward {
    const message = {
      ...baseDelegationDelegatorReward,
    } as DelegationDelegatorReward;
    message.reward = [];
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    if (object.reward !== undefined && object.reward !== null) {
      for (const e of object.reward) {
        message.reward.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: DelegationDelegatorReward): unknown {
    const obj: any = {};
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    if (message.reward) {
      obj.reward = message.reward.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.reward = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<DelegationDelegatorReward>
  ): DelegationDelegatorReward {
    const message = {
      ...baseDelegationDelegatorReward,
    } as DelegationDelegatorReward;
    message.reward = [];
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    if (object.reward !== undefined && object.reward !== null) {
      for (const e of object.reward) {
        message.reward.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseCommunityPoolSpendProposalWithDeposit: object = {
  title: "",
  description: "",
  recipient: "",
  amount: "",
  deposit: "",
};

export const CommunityPoolSpendProposalWithDeposit = {
  encode(
    message: CommunityPoolSpendProposalWithDeposit,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.title !== "") {
      writer.uint32(10).string(message.title);
    }
    if (message.description !== "") {
      writer.uint32(18).string(message.description);
    }
    if (message.recipient !== "") {
      writer.uint32(26).string(message.recipient);
    }
    if (message.amount !== "") {
      writer.uint32(34).string(message.amount);
    }
    if (message.deposit !== "") {
      writer.uint32(42).string(message.deposit);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): CommunityPoolSpendProposalWithDeposit {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseCommunityPoolSpendProposalWithDeposit,
    } as CommunityPoolSpendProposalWithDeposit;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.title = reader.string();
          break;
        case 2:
          message.description = reader.string();
          break;
        case 3:
          message.recipient = reader.string();
          break;
        case 4:
          message.amount = reader.string();
          break;
        case 5:
          message.deposit = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): CommunityPoolSpendProposalWithDeposit {
    const message = {
      ...baseCommunityPoolSpendProposalWithDeposit,
    } as CommunityPoolSpendProposalWithDeposit;
    if (object.title !== undefined && object.title !== null) {
      message.title = String(object.title);
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = String(object.description);
    } else {
      message.description = "";
    }
    if (object.recipient !== undefined && object.recipient !== null) {
      message.recipient = String(object.recipient);
    } else {
      message.recipient = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = String(object.amount);
    } else {
      message.amount = "";
    }
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = String(object.deposit);
    } else {
      message.deposit = "";
    }
    return message;
  },

  toJSON(message: CommunityPoolSpendProposalWithDeposit): unknown {
    const obj: any = {};
    message.title !== undefined && (obj.title = message.title);
    message.description !== undefined &&
      (obj.description = message.description);
    message.recipient !== undefined && (obj.recipient = message.recipient);
    message.amount !== undefined && (obj.amount = message.amount);
    message.deposit !== undefined && (obj.deposit = message.deposit);
    return obj;
  },

  fromPartial(
    object: DeepPartial<CommunityPoolSpendProposalWithDeposit>
  ): CommunityPoolSpendProposalWithDeposit {
    const message = {
      ...baseCommunityPoolSpendProposalWithDeposit,
    } as CommunityPoolSpendProposalWithDeposit;
    if (object.title !== undefined && object.title !== null) {
      message.title = object.title;
    } else {
      message.title = "";
    }
    if (object.description !== undefined && object.description !== null) {
      message.description = object.description;
    } else {
      message.description = "";
    }
    if (object.recipient !== undefined && object.recipient !== null) {
      message.recipient = object.recipient;
    } else {
      message.recipient = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = object.amount;
    } else {
      message.amount = "";
    }
    if (object.deposit !== undefined && object.deposit !== null) {
      message.deposit = object.deposit;
    } else {
      message.deposit = "";
    }
    return message;
  },
};

declare var self: any | undefined;
declare var window: any | undefined;
var globalThis: any = (() => {
  if (typeof globalThis !== "undefined") return globalThis;
  if (typeof self !== "undefined") return self;
  if (typeof window !== "undefined") return window;
  if (typeof global !== "undefined") return global;
  throw "Unable to locate global object";
})();

type Builtin = Date | Function | Uint8Array | string | number | undefined;
export type DeepPartial<T> = T extends Builtin
  ? T
  : T extends Array<infer U>
  ? Array<DeepPartial<U>>
  : T extends ReadonlyArray<infer U>
  ? ReadonlyArray<DeepPartial<U>>
  : T extends {}
  ? { [K in keyof T]?: DeepPartial<T[K]> }
  : Partial<T>;

function longToNumber(long: Long): number {
  if (long.gt(Number.MAX_SAFE_INTEGER)) {
    throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
  }
  return long.toNumber();
}

if (util.Long !== Long) {
  util.Long = Long as any;
  configure();
}
