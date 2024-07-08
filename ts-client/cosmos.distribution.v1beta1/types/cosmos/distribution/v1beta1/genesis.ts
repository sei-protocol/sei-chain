/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { DecCoin } from "../../../cosmos/base/v1beta1/coin";
import {
  ValidatorAccumulatedCommission,
  ValidatorHistoricalRewards,
  ValidatorCurrentRewards,
  DelegatorStartingInfo,
  ValidatorSlashEvent,
  Params,
  FeePool,
} from "../../../cosmos/distribution/v1beta1/distribution";

export const protobufPackage = "cosmos.distribution.v1beta1";

/**
 * DelegatorWithdrawInfo is the address for where distributions rewards are
 * withdrawn to by default this struct is only used at genesis to feed in
 * default withdraw addresses.
 */
export interface DelegatorWithdrawInfo {
  /** delegator_address is the address of the delegator. */
  delegatorAddress: string;
  /** withdraw_address is the address to withdraw the delegation rewards to. */
  withdrawAddress: string;
}

/** ValidatorOutstandingRewardsRecord is used for import/export via genesis json. */
export interface ValidatorOutstandingRewardsRecord {
  /** validator_address is the address of the validator. */
  validatorAddress: string;
  /** outstanding_rewards represents the oustanding rewards of a validator. */
  outstandingRewards: DecCoin[];
}

/**
 * ValidatorAccumulatedCommissionRecord is used for import / export via genesis
 * json.
 */
export interface ValidatorAccumulatedCommissionRecord {
  /** validator_address is the address of the validator. */
  validatorAddress: string;
  /** accumulated is the accumulated commission of a validator. */
  accumulated: ValidatorAccumulatedCommission | undefined;
}

/**
 * ValidatorHistoricalRewardsRecord is used for import / export via genesis
 * json.
 */
export interface ValidatorHistoricalRewardsRecord {
  /** validator_address is the address of the validator. */
  validatorAddress: string;
  /** period defines the period the historical rewards apply to. */
  period: number;
  /** rewards defines the historical rewards of a validator. */
  rewards: ValidatorHistoricalRewards | undefined;
}

/** ValidatorCurrentRewardsRecord is used for import / export via genesis json. */
export interface ValidatorCurrentRewardsRecord {
  /** validator_address is the address of the validator. */
  validatorAddress: string;
  /** rewards defines the current rewards of a validator. */
  rewards: ValidatorCurrentRewards | undefined;
}

/** DelegatorStartingInfoRecord used for import / export via genesis json. */
export interface DelegatorStartingInfoRecord {
  /** delegator_address is the address of the delegator. */
  delegatorAddress: string;
  /** validator_address is the address of the validator. */
  validatorAddress: string;
  /** starting_info defines the starting info of a delegator. */
  startingInfo: DelegatorStartingInfo | undefined;
}

/** ValidatorSlashEventRecord is used for import / export via genesis json. */
export interface ValidatorSlashEventRecord {
  /** validator_address is the address of the validator. */
  validatorAddress: string;
  /** height defines the block height at which the slash event occured. */
  height: number;
  /** period is the period of the slash event. */
  period: number;
  /** validator_slash_event describes the slash event. */
  validatorSlashEvent: ValidatorSlashEvent | undefined;
}

/** GenesisState defines the distribution module's genesis state. */
export interface GenesisState {
  /** params defines all the paramaters of the module. */
  params: Params | undefined;
  /** fee_pool defines the fee pool at genesis. */
  feePool: FeePool | undefined;
  /** fee_pool defines the delegator withdraw infos at genesis. */
  delegatorWithdrawInfos: DelegatorWithdrawInfo[];
  /** fee_pool defines the previous proposer at genesis. */
  previousProposer: string;
  /** fee_pool defines the outstanding rewards of all validators at genesis. */
  outstandingRewards: ValidatorOutstandingRewardsRecord[];
  /** fee_pool defines the accumulated commisions of all validators at genesis. */
  validatorAccumulatedCommissions: ValidatorAccumulatedCommissionRecord[];
  /** fee_pool defines the historical rewards of all validators at genesis. */
  validatorHistoricalRewards: ValidatorHistoricalRewardsRecord[];
  /** fee_pool defines the current rewards of all validators at genesis. */
  validatorCurrentRewards: ValidatorCurrentRewardsRecord[];
  /** fee_pool defines the delegator starting infos at genesis. */
  delegatorStartingInfos: DelegatorStartingInfoRecord[];
  /** fee_pool defines the validator slash events at genesis. */
  validatorSlashEvents: ValidatorSlashEventRecord[];
}

const baseDelegatorWithdrawInfo: object = {
  delegatorAddress: "",
  withdrawAddress: "",
};

export const DelegatorWithdrawInfo = {
  encode(
    message: DelegatorWithdrawInfo,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegatorAddress !== "") {
      writer.uint32(10).string(message.delegatorAddress);
    }
    if (message.withdrawAddress !== "") {
      writer.uint32(18).string(message.withdrawAddress);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DelegatorWithdrawInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDelegatorWithdrawInfo } as DelegatorWithdrawInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegatorAddress = reader.string();
          break;
        case 2:
          message.withdrawAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DelegatorWithdrawInfo {
    const message = { ...baseDelegatorWithdrawInfo } as DelegatorWithdrawInfo;
    if (
      object.delegatorAddress !== undefined &&
      object.delegatorAddress !== null
    ) {
      message.delegatorAddress = String(object.delegatorAddress);
    } else {
      message.delegatorAddress = "";
    }
    if (
      object.withdrawAddress !== undefined &&
      object.withdrawAddress !== null
    ) {
      message.withdrawAddress = String(object.withdrawAddress);
    } else {
      message.withdrawAddress = "";
    }
    return message;
  },

  toJSON(message: DelegatorWithdrawInfo): unknown {
    const obj: any = {};
    message.delegatorAddress !== undefined &&
      (obj.delegatorAddress = message.delegatorAddress);
    message.withdrawAddress !== undefined &&
      (obj.withdrawAddress = message.withdrawAddress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DelegatorWithdrawInfo>
  ): DelegatorWithdrawInfo {
    const message = { ...baseDelegatorWithdrawInfo } as DelegatorWithdrawInfo;
    if (
      object.delegatorAddress !== undefined &&
      object.delegatorAddress !== null
    ) {
      message.delegatorAddress = object.delegatorAddress;
    } else {
      message.delegatorAddress = "";
    }
    if (
      object.withdrawAddress !== undefined &&
      object.withdrawAddress !== null
    ) {
      message.withdrawAddress = object.withdrawAddress;
    } else {
      message.withdrawAddress = "";
    }
    return message;
  },
};

const baseValidatorOutstandingRewardsRecord: object = { validatorAddress: "" };

export const ValidatorOutstandingRewardsRecord = {
  encode(
    message: ValidatorOutstandingRewardsRecord,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddress !== "") {
      writer.uint32(10).string(message.validatorAddress);
    }
    for (const v of message.outstandingRewards) {
      DecCoin.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorOutstandingRewardsRecord {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorOutstandingRewardsRecord,
    } as ValidatorOutstandingRewardsRecord;
    message.outstandingRewards = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddress = reader.string();
          break;
        case 2:
          message.outstandingRewards.push(
            DecCoin.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorOutstandingRewardsRecord {
    const message = {
      ...baseValidatorOutstandingRewardsRecord,
    } as ValidatorOutstandingRewardsRecord;
    message.outstandingRewards = [];
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (
      object.outstandingRewards !== undefined &&
      object.outstandingRewards !== null
    ) {
      for (const e of object.outstandingRewards) {
        message.outstandingRewards.push(DecCoin.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ValidatorOutstandingRewardsRecord): unknown {
    const obj: any = {};
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    if (message.outstandingRewards) {
      obj.outstandingRewards = message.outstandingRewards.map((e) =>
        e ? DecCoin.toJSON(e) : undefined
      );
    } else {
      obj.outstandingRewards = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorOutstandingRewardsRecord>
  ): ValidatorOutstandingRewardsRecord {
    const message = {
      ...baseValidatorOutstandingRewardsRecord,
    } as ValidatorOutstandingRewardsRecord;
    message.outstandingRewards = [];
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (
      object.outstandingRewards !== undefined &&
      object.outstandingRewards !== null
    ) {
      for (const e of object.outstandingRewards) {
        message.outstandingRewards.push(DecCoin.fromPartial(e));
      }
    }
    return message;
  },
};

const baseValidatorAccumulatedCommissionRecord: object = {
  validatorAddress: "",
};

export const ValidatorAccumulatedCommissionRecord = {
  encode(
    message: ValidatorAccumulatedCommissionRecord,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddress !== "") {
      writer.uint32(10).string(message.validatorAddress);
    }
    if (message.accumulated !== undefined) {
      ValidatorAccumulatedCommission.encode(
        message.accumulated,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorAccumulatedCommissionRecord {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorAccumulatedCommissionRecord,
    } as ValidatorAccumulatedCommissionRecord;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddress = reader.string();
          break;
        case 2:
          message.accumulated = ValidatorAccumulatedCommission.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorAccumulatedCommissionRecord {
    const message = {
      ...baseValidatorAccumulatedCommissionRecord,
    } as ValidatorAccumulatedCommissionRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (object.accumulated !== undefined && object.accumulated !== null) {
      message.accumulated = ValidatorAccumulatedCommission.fromJSON(
        object.accumulated
      );
    } else {
      message.accumulated = undefined;
    }
    return message;
  },

  toJSON(message: ValidatorAccumulatedCommissionRecord): unknown {
    const obj: any = {};
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.accumulated !== undefined &&
      (obj.accumulated = message.accumulated
        ? ValidatorAccumulatedCommission.toJSON(message.accumulated)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorAccumulatedCommissionRecord>
  ): ValidatorAccumulatedCommissionRecord {
    const message = {
      ...baseValidatorAccumulatedCommissionRecord,
    } as ValidatorAccumulatedCommissionRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (object.accumulated !== undefined && object.accumulated !== null) {
      message.accumulated = ValidatorAccumulatedCommission.fromPartial(
        object.accumulated
      );
    } else {
      message.accumulated = undefined;
    }
    return message;
  },
};

const baseValidatorHistoricalRewardsRecord: object = {
  validatorAddress: "",
  period: 0,
};

export const ValidatorHistoricalRewardsRecord = {
  encode(
    message: ValidatorHistoricalRewardsRecord,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddress !== "") {
      writer.uint32(10).string(message.validatorAddress);
    }
    if (message.period !== 0) {
      writer.uint32(16).uint64(message.period);
    }
    if (message.rewards !== undefined) {
      ValidatorHistoricalRewards.encode(
        message.rewards,
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorHistoricalRewardsRecord {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorHistoricalRewardsRecord,
    } as ValidatorHistoricalRewardsRecord;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddress = reader.string();
          break;
        case 2:
          message.period = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.rewards = ValidatorHistoricalRewards.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorHistoricalRewardsRecord {
    const message = {
      ...baseValidatorHistoricalRewardsRecord,
    } as ValidatorHistoricalRewardsRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = Number(object.period);
    } else {
      message.period = 0;
    }
    if (object.rewards !== undefined && object.rewards !== null) {
      message.rewards = ValidatorHistoricalRewards.fromJSON(object.rewards);
    } else {
      message.rewards = undefined;
    }
    return message;
  },

  toJSON(message: ValidatorHistoricalRewardsRecord): unknown {
    const obj: any = {};
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.period !== undefined && (obj.period = message.period);
    message.rewards !== undefined &&
      (obj.rewards = message.rewards
        ? ValidatorHistoricalRewards.toJSON(message.rewards)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorHistoricalRewardsRecord>
  ): ValidatorHistoricalRewardsRecord {
    const message = {
      ...baseValidatorHistoricalRewardsRecord,
    } as ValidatorHistoricalRewardsRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = object.period;
    } else {
      message.period = 0;
    }
    if (object.rewards !== undefined && object.rewards !== null) {
      message.rewards = ValidatorHistoricalRewards.fromPartial(object.rewards);
    } else {
      message.rewards = undefined;
    }
    return message;
  },
};

const baseValidatorCurrentRewardsRecord: object = { validatorAddress: "" };

export const ValidatorCurrentRewardsRecord = {
  encode(
    message: ValidatorCurrentRewardsRecord,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddress !== "") {
      writer.uint32(10).string(message.validatorAddress);
    }
    if (message.rewards !== undefined) {
      ValidatorCurrentRewards.encode(
        message.rewards,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorCurrentRewardsRecord {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorCurrentRewardsRecord,
    } as ValidatorCurrentRewardsRecord;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddress = reader.string();
          break;
        case 2:
          message.rewards = ValidatorCurrentRewards.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorCurrentRewardsRecord {
    const message = {
      ...baseValidatorCurrentRewardsRecord,
    } as ValidatorCurrentRewardsRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (object.rewards !== undefined && object.rewards !== null) {
      message.rewards = ValidatorCurrentRewards.fromJSON(object.rewards);
    } else {
      message.rewards = undefined;
    }
    return message;
  },

  toJSON(message: ValidatorCurrentRewardsRecord): unknown {
    const obj: any = {};
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.rewards !== undefined &&
      (obj.rewards = message.rewards
        ? ValidatorCurrentRewards.toJSON(message.rewards)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorCurrentRewardsRecord>
  ): ValidatorCurrentRewardsRecord {
    const message = {
      ...baseValidatorCurrentRewardsRecord,
    } as ValidatorCurrentRewardsRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (object.rewards !== undefined && object.rewards !== null) {
      message.rewards = ValidatorCurrentRewards.fromPartial(object.rewards);
    } else {
      message.rewards = undefined;
    }
    return message;
  },
};

const baseDelegatorStartingInfoRecord: object = {
  delegatorAddress: "",
  validatorAddress: "",
};

export const DelegatorStartingInfoRecord = {
  encode(
    message: DelegatorStartingInfoRecord,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegatorAddress !== "") {
      writer.uint32(10).string(message.delegatorAddress);
    }
    if (message.validatorAddress !== "") {
      writer.uint32(18).string(message.validatorAddress);
    }
    if (message.startingInfo !== undefined) {
      DelegatorStartingInfo.encode(
        message.startingInfo,
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): DelegatorStartingInfoRecord {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseDelegatorStartingInfoRecord,
    } as DelegatorStartingInfoRecord;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegatorAddress = reader.string();
          break;
        case 2:
          message.validatorAddress = reader.string();
          break;
        case 3:
          message.startingInfo = DelegatorStartingInfo.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DelegatorStartingInfoRecord {
    const message = {
      ...baseDelegatorStartingInfoRecord,
    } as DelegatorStartingInfoRecord;
    if (
      object.delegatorAddress !== undefined &&
      object.delegatorAddress !== null
    ) {
      message.delegatorAddress = String(object.delegatorAddress);
    } else {
      message.delegatorAddress = "";
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (object.startingInfo !== undefined && object.startingInfo !== null) {
      message.startingInfo = DelegatorStartingInfo.fromJSON(
        object.startingInfo
      );
    } else {
      message.startingInfo = undefined;
    }
    return message;
  },

  toJSON(message: DelegatorStartingInfoRecord): unknown {
    const obj: any = {};
    message.delegatorAddress !== undefined &&
      (obj.delegatorAddress = message.delegatorAddress);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.startingInfo !== undefined &&
      (obj.startingInfo = message.startingInfo
        ? DelegatorStartingInfo.toJSON(message.startingInfo)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DelegatorStartingInfoRecord>
  ): DelegatorStartingInfoRecord {
    const message = {
      ...baseDelegatorStartingInfoRecord,
    } as DelegatorStartingInfoRecord;
    if (
      object.delegatorAddress !== undefined &&
      object.delegatorAddress !== null
    ) {
      message.delegatorAddress = object.delegatorAddress;
    } else {
      message.delegatorAddress = "";
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (object.startingInfo !== undefined && object.startingInfo !== null) {
      message.startingInfo = DelegatorStartingInfo.fromPartial(
        object.startingInfo
      );
    } else {
      message.startingInfo = undefined;
    }
    return message;
  },
};

const baseValidatorSlashEventRecord: object = {
  validatorAddress: "",
  height: 0,
  period: 0,
};

export const ValidatorSlashEventRecord = {
  encode(
    message: ValidatorSlashEventRecord,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validatorAddress !== "") {
      writer.uint32(10).string(message.validatorAddress);
    }
    if (message.height !== 0) {
      writer.uint32(16).uint64(message.height);
    }
    if (message.period !== 0) {
      writer.uint32(24).uint64(message.period);
    }
    if (message.validatorSlashEvent !== undefined) {
      ValidatorSlashEvent.encode(
        message.validatorSlashEvent,
        writer.uint32(34).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ValidatorSlashEventRecord {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseValidatorSlashEventRecord,
    } as ValidatorSlashEventRecord;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validatorAddress = reader.string();
          break;
        case 2:
          message.height = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.period = longToNumber(reader.uint64() as Long);
          break;
        case 4:
          message.validatorSlashEvent = ValidatorSlashEvent.decode(
            reader,
            reader.uint32()
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorSlashEventRecord {
    const message = {
      ...baseValidatorSlashEventRecord,
    } as ValidatorSlashEventRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = Number(object.height);
    } else {
      message.height = 0;
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = Number(object.period);
    } else {
      message.period = 0;
    }
    if (
      object.validatorSlashEvent !== undefined &&
      object.validatorSlashEvent !== null
    ) {
      message.validatorSlashEvent = ValidatorSlashEvent.fromJSON(
        object.validatorSlashEvent
      );
    } else {
      message.validatorSlashEvent = undefined;
    }
    return message;
  },

  toJSON(message: ValidatorSlashEventRecord): unknown {
    const obj: any = {};
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.height !== undefined && (obj.height = message.height);
    message.period !== undefined && (obj.period = message.period);
    message.validatorSlashEvent !== undefined &&
      (obj.validatorSlashEvent = message.validatorSlashEvent
        ? ValidatorSlashEvent.toJSON(message.validatorSlashEvent)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ValidatorSlashEventRecord>
  ): ValidatorSlashEventRecord {
    const message = {
      ...baseValidatorSlashEventRecord,
    } as ValidatorSlashEventRecord;
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (object.height !== undefined && object.height !== null) {
      message.height = object.height;
    } else {
      message.height = 0;
    }
    if (object.period !== undefined && object.period !== null) {
      message.period = object.period;
    } else {
      message.period = 0;
    }
    if (
      object.validatorSlashEvent !== undefined &&
      object.validatorSlashEvent !== null
    ) {
      message.validatorSlashEvent = ValidatorSlashEvent.fromPartial(
        object.validatorSlashEvent
      );
    } else {
      message.validatorSlashEvent = undefined;
    }
    return message;
  },
};

const baseGenesisState: object = { previousProposer: "" };

export const GenesisState = {
  encode(message: GenesisState, writer: Writer = Writer.create()): Writer {
    if (message.params !== undefined) {
      Params.encode(message.params, writer.uint32(10).fork()).ldelim();
    }
    if (message.feePool !== undefined) {
      FeePool.encode(message.feePool, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.delegatorWithdrawInfos) {
      DelegatorWithdrawInfo.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    if (message.previousProposer !== "") {
      writer.uint32(34).string(message.previousProposer);
    }
    for (const v of message.outstandingRewards) {
      ValidatorOutstandingRewardsRecord.encode(
        v!,
        writer.uint32(42).fork()
      ).ldelim();
    }
    for (const v of message.validatorAccumulatedCommissions) {
      ValidatorAccumulatedCommissionRecord.encode(
        v!,
        writer.uint32(50).fork()
      ).ldelim();
    }
    for (const v of message.validatorHistoricalRewards) {
      ValidatorHistoricalRewardsRecord.encode(
        v!,
        writer.uint32(58).fork()
      ).ldelim();
    }
    for (const v of message.validatorCurrentRewards) {
      ValidatorCurrentRewardsRecord.encode(
        v!,
        writer.uint32(66).fork()
      ).ldelim();
    }
    for (const v of message.delegatorStartingInfos) {
      DelegatorStartingInfoRecord.encode(v!, writer.uint32(74).fork()).ldelim();
    }
    for (const v of message.validatorSlashEvents) {
      ValidatorSlashEventRecord.encode(v!, writer.uint32(82).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GenesisState {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGenesisState } as GenesisState;
    message.delegatorWithdrawInfos = [];
    message.outstandingRewards = [];
    message.validatorAccumulatedCommissions = [];
    message.validatorHistoricalRewards = [];
    message.validatorCurrentRewards = [];
    message.delegatorStartingInfos = [];
    message.validatorSlashEvents = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.params = Params.decode(reader, reader.uint32());
          break;
        case 2:
          message.feePool = FeePool.decode(reader, reader.uint32());
          break;
        case 3:
          message.delegatorWithdrawInfos.push(
            DelegatorWithdrawInfo.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.previousProposer = reader.string();
          break;
        case 5:
          message.outstandingRewards.push(
            ValidatorOutstandingRewardsRecord.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.validatorAccumulatedCommissions.push(
            ValidatorAccumulatedCommissionRecord.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.validatorHistoricalRewards.push(
            ValidatorHistoricalRewardsRecord.decode(reader, reader.uint32())
          );
          break;
        case 8:
          message.validatorCurrentRewards.push(
            ValidatorCurrentRewardsRecord.decode(reader, reader.uint32())
          );
          break;
        case 9:
          message.delegatorStartingInfos.push(
            DelegatorStartingInfoRecord.decode(reader, reader.uint32())
          );
          break;
        case 10:
          message.validatorSlashEvents.push(
            ValidatorSlashEventRecord.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.delegatorWithdrawInfos = [];
    message.outstandingRewards = [];
    message.validatorAccumulatedCommissions = [];
    message.validatorHistoricalRewards = [];
    message.validatorCurrentRewards = [];
    message.delegatorStartingInfos = [];
    message.validatorSlashEvents = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromJSON(object.params);
    } else {
      message.params = undefined;
    }
    if (object.feePool !== undefined && object.feePool !== null) {
      message.feePool = FeePool.fromJSON(object.feePool);
    } else {
      message.feePool = undefined;
    }
    if (
      object.delegatorWithdrawInfos !== undefined &&
      object.delegatorWithdrawInfos !== null
    ) {
      for (const e of object.delegatorWithdrawInfos) {
        message.delegatorWithdrawInfos.push(DelegatorWithdrawInfo.fromJSON(e));
      }
    }
    if (
      object.previousProposer !== undefined &&
      object.previousProposer !== null
    ) {
      message.previousProposer = String(object.previousProposer);
    } else {
      message.previousProposer = "";
    }
    if (
      object.outstandingRewards !== undefined &&
      object.outstandingRewards !== null
    ) {
      for (const e of object.outstandingRewards) {
        message.outstandingRewards.push(
          ValidatorOutstandingRewardsRecord.fromJSON(e)
        );
      }
    }
    if (
      object.validatorAccumulatedCommissions !== undefined &&
      object.validatorAccumulatedCommissions !== null
    ) {
      for (const e of object.validatorAccumulatedCommissions) {
        message.validatorAccumulatedCommissions.push(
          ValidatorAccumulatedCommissionRecord.fromJSON(e)
        );
      }
    }
    if (
      object.validatorHistoricalRewards !== undefined &&
      object.validatorHistoricalRewards !== null
    ) {
      for (const e of object.validatorHistoricalRewards) {
        message.validatorHistoricalRewards.push(
          ValidatorHistoricalRewardsRecord.fromJSON(e)
        );
      }
    }
    if (
      object.validatorCurrentRewards !== undefined &&
      object.validatorCurrentRewards !== null
    ) {
      for (const e of object.validatorCurrentRewards) {
        message.validatorCurrentRewards.push(
          ValidatorCurrentRewardsRecord.fromJSON(e)
        );
      }
    }
    if (
      object.delegatorStartingInfos !== undefined &&
      object.delegatorStartingInfos !== null
    ) {
      for (const e of object.delegatorStartingInfos) {
        message.delegatorStartingInfos.push(
          DelegatorStartingInfoRecord.fromJSON(e)
        );
      }
    }
    if (
      object.validatorSlashEvents !== undefined &&
      object.validatorSlashEvents !== null
    ) {
      for (const e of object.validatorSlashEvents) {
        message.validatorSlashEvents.push(
          ValidatorSlashEventRecord.fromJSON(e)
        );
      }
    }
    return message;
  },

  toJSON(message: GenesisState): unknown {
    const obj: any = {};
    message.params !== undefined &&
      (obj.params = message.params ? Params.toJSON(message.params) : undefined);
    message.feePool !== undefined &&
      (obj.feePool = message.feePool
        ? FeePool.toJSON(message.feePool)
        : undefined);
    if (message.delegatorWithdrawInfos) {
      obj.delegatorWithdrawInfos = message.delegatorWithdrawInfos.map((e) =>
        e ? DelegatorWithdrawInfo.toJSON(e) : undefined
      );
    } else {
      obj.delegatorWithdrawInfos = [];
    }
    message.previousProposer !== undefined &&
      (obj.previousProposer = message.previousProposer);
    if (message.outstandingRewards) {
      obj.outstandingRewards = message.outstandingRewards.map((e) =>
        e ? ValidatorOutstandingRewardsRecord.toJSON(e) : undefined
      );
    } else {
      obj.outstandingRewards = [];
    }
    if (message.validatorAccumulatedCommissions) {
      obj.validatorAccumulatedCommissions = message.validatorAccumulatedCommissions.map(
        (e) => (e ? ValidatorAccumulatedCommissionRecord.toJSON(e) : undefined)
      );
    } else {
      obj.validatorAccumulatedCommissions = [];
    }
    if (message.validatorHistoricalRewards) {
      obj.validatorHistoricalRewards = message.validatorHistoricalRewards.map(
        (e) => (e ? ValidatorHistoricalRewardsRecord.toJSON(e) : undefined)
      );
    } else {
      obj.validatorHistoricalRewards = [];
    }
    if (message.validatorCurrentRewards) {
      obj.validatorCurrentRewards = message.validatorCurrentRewards.map((e) =>
        e ? ValidatorCurrentRewardsRecord.toJSON(e) : undefined
      );
    } else {
      obj.validatorCurrentRewards = [];
    }
    if (message.delegatorStartingInfos) {
      obj.delegatorStartingInfos = message.delegatorStartingInfos.map((e) =>
        e ? DelegatorStartingInfoRecord.toJSON(e) : undefined
      );
    } else {
      obj.delegatorStartingInfos = [];
    }
    if (message.validatorSlashEvents) {
      obj.validatorSlashEvents = message.validatorSlashEvents.map((e) =>
        e ? ValidatorSlashEventRecord.toJSON(e) : undefined
      );
    } else {
      obj.validatorSlashEvents = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GenesisState>): GenesisState {
    const message = { ...baseGenesisState } as GenesisState;
    message.delegatorWithdrawInfos = [];
    message.outstandingRewards = [];
    message.validatorAccumulatedCommissions = [];
    message.validatorHistoricalRewards = [];
    message.validatorCurrentRewards = [];
    message.delegatorStartingInfos = [];
    message.validatorSlashEvents = [];
    if (object.params !== undefined && object.params !== null) {
      message.params = Params.fromPartial(object.params);
    } else {
      message.params = undefined;
    }
    if (object.feePool !== undefined && object.feePool !== null) {
      message.feePool = FeePool.fromPartial(object.feePool);
    } else {
      message.feePool = undefined;
    }
    if (
      object.delegatorWithdrawInfos !== undefined &&
      object.delegatorWithdrawInfos !== null
    ) {
      for (const e of object.delegatorWithdrawInfos) {
        message.delegatorWithdrawInfos.push(
          DelegatorWithdrawInfo.fromPartial(e)
        );
      }
    }
    if (
      object.previousProposer !== undefined &&
      object.previousProposer !== null
    ) {
      message.previousProposer = object.previousProposer;
    } else {
      message.previousProposer = "";
    }
    if (
      object.outstandingRewards !== undefined &&
      object.outstandingRewards !== null
    ) {
      for (const e of object.outstandingRewards) {
        message.outstandingRewards.push(
          ValidatorOutstandingRewardsRecord.fromPartial(e)
        );
      }
    }
    if (
      object.validatorAccumulatedCommissions !== undefined &&
      object.validatorAccumulatedCommissions !== null
    ) {
      for (const e of object.validatorAccumulatedCommissions) {
        message.validatorAccumulatedCommissions.push(
          ValidatorAccumulatedCommissionRecord.fromPartial(e)
        );
      }
    }
    if (
      object.validatorHistoricalRewards !== undefined &&
      object.validatorHistoricalRewards !== null
    ) {
      for (const e of object.validatorHistoricalRewards) {
        message.validatorHistoricalRewards.push(
          ValidatorHistoricalRewardsRecord.fromPartial(e)
        );
      }
    }
    if (
      object.validatorCurrentRewards !== undefined &&
      object.validatorCurrentRewards !== null
    ) {
      for (const e of object.validatorCurrentRewards) {
        message.validatorCurrentRewards.push(
          ValidatorCurrentRewardsRecord.fromPartial(e)
        );
      }
    }
    if (
      object.delegatorStartingInfos !== undefined &&
      object.delegatorStartingInfos !== null
    ) {
      for (const e of object.delegatorStartingInfos) {
        message.delegatorStartingInfos.push(
          DelegatorStartingInfoRecord.fromPartial(e)
        );
      }
    }
    if (
      object.validatorSlashEvents !== undefined &&
      object.validatorSlashEvents !== null
    ) {
      for (const e of object.validatorSlashEvents) {
        message.validatorSlashEvents.push(
          ValidatorSlashEventRecord.fromPartial(e)
        );
      }
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
