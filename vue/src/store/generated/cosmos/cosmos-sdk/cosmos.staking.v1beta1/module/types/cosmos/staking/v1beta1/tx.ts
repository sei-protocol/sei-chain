/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Timestamp } from "../../../google/protobuf/timestamp";
import {
  Description,
  CommissionRates,
} from "../../../cosmos/staking/v1beta1/staking";
import { Any } from "../../../google/protobuf/any";
import { Coin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.staking.v1beta1";

/** MsgCreateValidator defines a SDK message for creating a new validator. */
export interface MsgCreateValidator {
  description: Description | undefined;
  commission: CommissionRates | undefined;
  minSelfDelegation: string;
  delegatorAddress: string;
  validatorAddress: string;
  pubkey: Any | undefined;
  value: Coin | undefined;
}

/** MsgCreateValidatorResponse defines the Msg/CreateValidator response type. */
export interface MsgCreateValidatorResponse {}

/** MsgEditValidator defines a SDK message for editing an existing validator. */
export interface MsgEditValidator {
  description: Description | undefined;
  validatorAddress: string;
  /**
   * We pass a reference to the new commission rate and min self delegation as
   * it's not mandatory to update. If not updated, the deserialized rate will be
   * zero with no way to distinguish if an update was intended.
   * REF: #2373
   */
  commissionRate: string;
  minSelfDelegation: string;
}

/** MsgEditValidatorResponse defines the Msg/EditValidator response type. */
export interface MsgEditValidatorResponse {}

/**
 * MsgDelegate defines a SDK message for performing a delegation of coins
 * from a delegator to a validator.
 */
export interface MsgDelegate {
  delegatorAddress: string;
  validatorAddress: string;
  amount: Coin | undefined;
}

/** MsgDelegateResponse defines the Msg/Delegate response type. */
export interface MsgDelegateResponse {}

/**
 * MsgBeginRedelegate defines a SDK message for performing a redelegation
 * of coins from a delegator and source validator to a destination validator.
 */
export interface MsgBeginRedelegate {
  delegatorAddress: string;
  validatorSrcAddress: string;
  validatorDstAddress: string;
  amount: Coin | undefined;
}

/** MsgBeginRedelegateResponse defines the Msg/BeginRedelegate response type. */
export interface MsgBeginRedelegateResponse {
  completionTime: Date | undefined;
}

/**
 * MsgUndelegate defines a SDK message for performing an undelegation from a
 * delegate and a validator.
 */
export interface MsgUndelegate {
  delegatorAddress: string;
  validatorAddress: string;
  amount: Coin | undefined;
}

/** MsgUndelegateResponse defines the Msg/Undelegate response type. */
export interface MsgUndelegateResponse {
  completionTime: Date | undefined;
}

const baseMsgCreateValidator: object = {
  minSelfDelegation: "",
  delegatorAddress: "",
  validatorAddress: "",
};

export const MsgCreateValidator = {
  encode(
    message: MsgCreateValidator,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.description !== undefined) {
      Description.encode(
        message.description,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.commission !== undefined) {
      CommissionRates.encode(
        message.commission,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.minSelfDelegation !== "") {
      writer.uint32(26).string(message.minSelfDelegation);
    }
    if (message.delegatorAddress !== "") {
      writer.uint32(34).string(message.delegatorAddress);
    }
    if (message.validatorAddress !== "") {
      writer.uint32(42).string(message.validatorAddress);
    }
    if (message.pubkey !== undefined) {
      Any.encode(message.pubkey, writer.uint32(50).fork()).ldelim();
    }
    if (message.value !== undefined) {
      Coin.encode(message.value, writer.uint32(58).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgCreateValidator {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgCreateValidator } as MsgCreateValidator;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.description = Description.decode(reader, reader.uint32());
          break;
        case 2:
          message.commission = CommissionRates.decode(reader, reader.uint32());
          break;
        case 3:
          message.minSelfDelegation = reader.string();
          break;
        case 4:
          message.delegatorAddress = reader.string();
          break;
        case 5:
          message.validatorAddress = reader.string();
          break;
        case 6:
          message.pubkey = Any.decode(reader, reader.uint32());
          break;
        case 7:
          message.value = Coin.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgCreateValidator {
    const message = { ...baseMsgCreateValidator } as MsgCreateValidator;
    if (object.description !== undefined && object.description !== null) {
      message.description = Description.fromJSON(object.description);
    } else {
      message.description = undefined;
    }
    if (object.commission !== undefined && object.commission !== null) {
      message.commission = CommissionRates.fromJSON(object.commission);
    } else {
      message.commission = undefined;
    }
    if (
      object.minSelfDelegation !== undefined &&
      object.minSelfDelegation !== null
    ) {
      message.minSelfDelegation = String(object.minSelfDelegation);
    } else {
      message.minSelfDelegation = "";
    }
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
    if (object.pubkey !== undefined && object.pubkey !== null) {
      message.pubkey = Any.fromJSON(object.pubkey);
    } else {
      message.pubkey = undefined;
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = Coin.fromJSON(object.value);
    } else {
      message.value = undefined;
    }
    return message;
  },

  toJSON(message: MsgCreateValidator): unknown {
    const obj: any = {};
    message.description !== undefined &&
      (obj.description = message.description
        ? Description.toJSON(message.description)
        : undefined);
    message.commission !== undefined &&
      (obj.commission = message.commission
        ? CommissionRates.toJSON(message.commission)
        : undefined);
    message.minSelfDelegation !== undefined &&
      (obj.minSelfDelegation = message.minSelfDelegation);
    message.delegatorAddress !== undefined &&
      (obj.delegatorAddress = message.delegatorAddress);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.pubkey !== undefined &&
      (obj.pubkey = message.pubkey ? Any.toJSON(message.pubkey) : undefined);
    message.value !== undefined &&
      (obj.value = message.value ? Coin.toJSON(message.value) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgCreateValidator>): MsgCreateValidator {
    const message = { ...baseMsgCreateValidator } as MsgCreateValidator;
    if (object.description !== undefined && object.description !== null) {
      message.description = Description.fromPartial(object.description);
    } else {
      message.description = undefined;
    }
    if (object.commission !== undefined && object.commission !== null) {
      message.commission = CommissionRates.fromPartial(object.commission);
    } else {
      message.commission = undefined;
    }
    if (
      object.minSelfDelegation !== undefined &&
      object.minSelfDelegation !== null
    ) {
      message.minSelfDelegation = object.minSelfDelegation;
    } else {
      message.minSelfDelegation = "";
    }
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
    if (object.pubkey !== undefined && object.pubkey !== null) {
      message.pubkey = Any.fromPartial(object.pubkey);
    } else {
      message.pubkey = undefined;
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = Coin.fromPartial(object.value);
    } else {
      message.value = undefined;
    }
    return message;
  },
};

const baseMsgCreateValidatorResponse: object = {};

export const MsgCreateValidatorResponse = {
  encode(
    _: MsgCreateValidatorResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgCreateValidatorResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgCreateValidatorResponse,
    } as MsgCreateValidatorResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgCreateValidatorResponse {
    const message = {
      ...baseMsgCreateValidatorResponse,
    } as MsgCreateValidatorResponse;
    return message;
  },

  toJSON(_: MsgCreateValidatorResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgCreateValidatorResponse>
  ): MsgCreateValidatorResponse {
    const message = {
      ...baseMsgCreateValidatorResponse,
    } as MsgCreateValidatorResponse;
    return message;
  },
};

const baseMsgEditValidator: object = {
  validatorAddress: "",
  commissionRate: "",
  minSelfDelegation: "",
};

export const MsgEditValidator = {
  encode(message: MsgEditValidator, writer: Writer = Writer.create()): Writer {
    if (message.description !== undefined) {
      Description.encode(
        message.description,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.validatorAddress !== "") {
      writer.uint32(18).string(message.validatorAddress);
    }
    if (message.commissionRate !== "") {
      writer.uint32(26).string(message.commissionRate);
    }
    if (message.minSelfDelegation !== "") {
      writer.uint32(34).string(message.minSelfDelegation);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgEditValidator {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgEditValidator } as MsgEditValidator;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.description = Description.decode(reader, reader.uint32());
          break;
        case 2:
          message.validatorAddress = reader.string();
          break;
        case 3:
          message.commissionRate = reader.string();
          break;
        case 4:
          message.minSelfDelegation = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgEditValidator {
    const message = { ...baseMsgEditValidator } as MsgEditValidator;
    if (object.description !== undefined && object.description !== null) {
      message.description = Description.fromJSON(object.description);
    } else {
      message.description = undefined;
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = String(object.validatorAddress);
    } else {
      message.validatorAddress = "";
    }
    if (object.commissionRate !== undefined && object.commissionRate !== null) {
      message.commissionRate = String(object.commissionRate);
    } else {
      message.commissionRate = "";
    }
    if (
      object.minSelfDelegation !== undefined &&
      object.minSelfDelegation !== null
    ) {
      message.minSelfDelegation = String(object.minSelfDelegation);
    } else {
      message.minSelfDelegation = "";
    }
    return message;
  },

  toJSON(message: MsgEditValidator): unknown {
    const obj: any = {};
    message.description !== undefined &&
      (obj.description = message.description
        ? Description.toJSON(message.description)
        : undefined);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.commissionRate !== undefined &&
      (obj.commissionRate = message.commissionRate);
    message.minSelfDelegation !== undefined &&
      (obj.minSelfDelegation = message.minSelfDelegation);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgEditValidator>): MsgEditValidator {
    const message = { ...baseMsgEditValidator } as MsgEditValidator;
    if (object.description !== undefined && object.description !== null) {
      message.description = Description.fromPartial(object.description);
    } else {
      message.description = undefined;
    }
    if (
      object.validatorAddress !== undefined &&
      object.validatorAddress !== null
    ) {
      message.validatorAddress = object.validatorAddress;
    } else {
      message.validatorAddress = "";
    }
    if (object.commissionRate !== undefined && object.commissionRate !== null) {
      message.commissionRate = object.commissionRate;
    } else {
      message.commissionRate = "";
    }
    if (
      object.minSelfDelegation !== undefined &&
      object.minSelfDelegation !== null
    ) {
      message.minSelfDelegation = object.minSelfDelegation;
    } else {
      message.minSelfDelegation = "";
    }
    return message;
  },
};

const baseMsgEditValidatorResponse: object = {};

export const MsgEditValidatorResponse = {
  encode(
    _: MsgEditValidatorResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgEditValidatorResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgEditValidatorResponse,
    } as MsgEditValidatorResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgEditValidatorResponse {
    const message = {
      ...baseMsgEditValidatorResponse,
    } as MsgEditValidatorResponse;
    return message;
  },

  toJSON(_: MsgEditValidatorResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgEditValidatorResponse>
  ): MsgEditValidatorResponse {
    const message = {
      ...baseMsgEditValidatorResponse,
    } as MsgEditValidatorResponse;
    return message;
  },
};

const baseMsgDelegate: object = { delegatorAddress: "", validatorAddress: "" };

export const MsgDelegate = {
  encode(message: MsgDelegate, writer: Writer = Writer.create()): Writer {
    if (message.delegatorAddress !== "") {
      writer.uint32(10).string(message.delegatorAddress);
    }
    if (message.validatorAddress !== "") {
      writer.uint32(18).string(message.validatorAddress);
    }
    if (message.amount !== undefined) {
      Coin.encode(message.amount, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgDelegate {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgDelegate } as MsgDelegate;
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
          message.amount = Coin.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgDelegate {
    const message = { ...baseMsgDelegate } as MsgDelegate;
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
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Coin.fromJSON(object.amount);
    } else {
      message.amount = undefined;
    }
    return message;
  },

  toJSON(message: MsgDelegate): unknown {
    const obj: any = {};
    message.delegatorAddress !== undefined &&
      (obj.delegatorAddress = message.delegatorAddress);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.amount !== undefined &&
      (obj.amount = message.amount ? Coin.toJSON(message.amount) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgDelegate>): MsgDelegate {
    const message = { ...baseMsgDelegate } as MsgDelegate;
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
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Coin.fromPartial(object.amount);
    } else {
      message.amount = undefined;
    }
    return message;
  },
};

const baseMsgDelegateResponse: object = {};

export const MsgDelegateResponse = {
  encode(_: MsgDelegateResponse, writer: Writer = Writer.create()): Writer {
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgDelegateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgDelegateResponse } as MsgDelegateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(_: any): MsgDelegateResponse {
    const message = { ...baseMsgDelegateResponse } as MsgDelegateResponse;
    return message;
  },

  toJSON(_: MsgDelegateResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(_: DeepPartial<MsgDelegateResponse>): MsgDelegateResponse {
    const message = { ...baseMsgDelegateResponse } as MsgDelegateResponse;
    return message;
  },
};

const baseMsgBeginRedelegate: object = {
  delegatorAddress: "",
  validatorSrcAddress: "",
  validatorDstAddress: "",
};

export const MsgBeginRedelegate = {
  encode(
    message: MsgBeginRedelegate,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegatorAddress !== "") {
      writer.uint32(10).string(message.delegatorAddress);
    }
    if (message.validatorSrcAddress !== "") {
      writer.uint32(18).string(message.validatorSrcAddress);
    }
    if (message.validatorDstAddress !== "") {
      writer.uint32(26).string(message.validatorDstAddress);
    }
    if (message.amount !== undefined) {
      Coin.encode(message.amount, writer.uint32(34).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgBeginRedelegate {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgBeginRedelegate } as MsgBeginRedelegate;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegatorAddress = reader.string();
          break;
        case 2:
          message.validatorSrcAddress = reader.string();
          break;
        case 3:
          message.validatorDstAddress = reader.string();
          break;
        case 4:
          message.amount = Coin.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgBeginRedelegate {
    const message = { ...baseMsgBeginRedelegate } as MsgBeginRedelegate;
    if (
      object.delegatorAddress !== undefined &&
      object.delegatorAddress !== null
    ) {
      message.delegatorAddress = String(object.delegatorAddress);
    } else {
      message.delegatorAddress = "";
    }
    if (
      object.validatorSrcAddress !== undefined &&
      object.validatorSrcAddress !== null
    ) {
      message.validatorSrcAddress = String(object.validatorSrcAddress);
    } else {
      message.validatorSrcAddress = "";
    }
    if (
      object.validatorDstAddress !== undefined &&
      object.validatorDstAddress !== null
    ) {
      message.validatorDstAddress = String(object.validatorDstAddress);
    } else {
      message.validatorDstAddress = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Coin.fromJSON(object.amount);
    } else {
      message.amount = undefined;
    }
    return message;
  },

  toJSON(message: MsgBeginRedelegate): unknown {
    const obj: any = {};
    message.delegatorAddress !== undefined &&
      (obj.delegatorAddress = message.delegatorAddress);
    message.validatorSrcAddress !== undefined &&
      (obj.validatorSrcAddress = message.validatorSrcAddress);
    message.validatorDstAddress !== undefined &&
      (obj.validatorDstAddress = message.validatorDstAddress);
    message.amount !== undefined &&
      (obj.amount = message.amount ? Coin.toJSON(message.amount) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgBeginRedelegate>): MsgBeginRedelegate {
    const message = { ...baseMsgBeginRedelegate } as MsgBeginRedelegate;
    if (
      object.delegatorAddress !== undefined &&
      object.delegatorAddress !== null
    ) {
      message.delegatorAddress = object.delegatorAddress;
    } else {
      message.delegatorAddress = "";
    }
    if (
      object.validatorSrcAddress !== undefined &&
      object.validatorSrcAddress !== null
    ) {
      message.validatorSrcAddress = object.validatorSrcAddress;
    } else {
      message.validatorSrcAddress = "";
    }
    if (
      object.validatorDstAddress !== undefined &&
      object.validatorDstAddress !== null
    ) {
      message.validatorDstAddress = object.validatorDstAddress;
    } else {
      message.validatorDstAddress = "";
    }
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Coin.fromPartial(object.amount);
    } else {
      message.amount = undefined;
    }
    return message;
  },
};

const baseMsgBeginRedelegateResponse: object = {};

export const MsgBeginRedelegateResponse = {
  encode(
    message: MsgBeginRedelegateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.completionTime !== undefined) {
      Timestamp.encode(
        toTimestamp(message.completionTime),
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgBeginRedelegateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgBeginRedelegateResponse,
    } as MsgBeginRedelegateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.completionTime = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgBeginRedelegateResponse {
    const message = {
      ...baseMsgBeginRedelegateResponse,
    } as MsgBeginRedelegateResponse;
    if (object.completionTime !== undefined && object.completionTime !== null) {
      message.completionTime = fromJsonTimestamp(object.completionTime);
    } else {
      message.completionTime = undefined;
    }
    return message;
  },

  toJSON(message: MsgBeginRedelegateResponse): unknown {
    const obj: any = {};
    message.completionTime !== undefined &&
      (obj.completionTime =
        message.completionTime !== undefined
          ? message.completionTime.toISOString()
          : null);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgBeginRedelegateResponse>
  ): MsgBeginRedelegateResponse {
    const message = {
      ...baseMsgBeginRedelegateResponse,
    } as MsgBeginRedelegateResponse;
    if (object.completionTime !== undefined && object.completionTime !== null) {
      message.completionTime = object.completionTime;
    } else {
      message.completionTime = undefined;
    }
    return message;
  },
};

const baseMsgUndelegate: object = {
  delegatorAddress: "",
  validatorAddress: "",
};

export const MsgUndelegate = {
  encode(message: MsgUndelegate, writer: Writer = Writer.create()): Writer {
    if (message.delegatorAddress !== "") {
      writer.uint32(10).string(message.delegatorAddress);
    }
    if (message.validatorAddress !== "") {
      writer.uint32(18).string(message.validatorAddress);
    }
    if (message.amount !== undefined) {
      Coin.encode(message.amount, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUndelegate {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUndelegate } as MsgUndelegate;
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
          message.amount = Coin.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUndelegate {
    const message = { ...baseMsgUndelegate } as MsgUndelegate;
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
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Coin.fromJSON(object.amount);
    } else {
      message.amount = undefined;
    }
    return message;
  },

  toJSON(message: MsgUndelegate): unknown {
    const obj: any = {};
    message.delegatorAddress !== undefined &&
      (obj.delegatorAddress = message.delegatorAddress);
    message.validatorAddress !== undefined &&
      (obj.validatorAddress = message.validatorAddress);
    message.amount !== undefined &&
      (obj.amount = message.amount ? Coin.toJSON(message.amount) : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgUndelegate>): MsgUndelegate {
    const message = { ...baseMsgUndelegate } as MsgUndelegate;
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
    if (object.amount !== undefined && object.amount !== null) {
      message.amount = Coin.fromPartial(object.amount);
    } else {
      message.amount = undefined;
    }
    return message;
  },
};

const baseMsgUndelegateResponse: object = {};

export const MsgUndelegateResponse = {
  encode(
    message: MsgUndelegateResponse,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.completionTime !== undefined) {
      Timestamp.encode(
        toTimestamp(message.completionTime),
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgUndelegateResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgUndelegateResponse } as MsgUndelegateResponse;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.completionTime = fromTimestamp(
            Timestamp.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgUndelegateResponse {
    const message = { ...baseMsgUndelegateResponse } as MsgUndelegateResponse;
    if (object.completionTime !== undefined && object.completionTime !== null) {
      message.completionTime = fromJsonTimestamp(object.completionTime);
    } else {
      message.completionTime = undefined;
    }
    return message;
  },

  toJSON(message: MsgUndelegateResponse): unknown {
    const obj: any = {};
    message.completionTime !== undefined &&
      (obj.completionTime =
        message.completionTime !== undefined
          ? message.completionTime.toISOString()
          : null);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgUndelegateResponse>
  ): MsgUndelegateResponse {
    const message = { ...baseMsgUndelegateResponse } as MsgUndelegateResponse;
    if (object.completionTime !== undefined && object.completionTime !== null) {
      message.completionTime = object.completionTime;
    } else {
      message.completionTime = undefined;
    }
    return message;
  },
};

/** Msg defines the staking Msg service. */
export interface Msg {
  /** CreateValidator defines a method for creating a new validator. */
  CreateValidator(
    request: MsgCreateValidator
  ): Promise<MsgCreateValidatorResponse>;
  /** EditValidator defines a method for editing an existing validator. */
  EditValidator(request: MsgEditValidator): Promise<MsgEditValidatorResponse>;
  /**
   * Delegate defines a method for performing a delegation of coins
   * from a delegator to a validator.
   */
  Delegate(request: MsgDelegate): Promise<MsgDelegateResponse>;
  /**
   * BeginRedelegate defines a method for performing a redelegation
   * of coins from a delegator and source validator to a destination validator.
   */
  BeginRedelegate(
    request: MsgBeginRedelegate
  ): Promise<MsgBeginRedelegateResponse>;
  /**
   * Undelegate defines a method for performing an undelegation from a
   * delegate and a validator.
   */
  Undelegate(request: MsgUndelegate): Promise<MsgUndelegateResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  CreateValidator(
    request: MsgCreateValidator
  ): Promise<MsgCreateValidatorResponse> {
    const data = MsgCreateValidator.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.staking.v1beta1.Msg",
      "CreateValidator",
      data
    );
    return promise.then((data) =>
      MsgCreateValidatorResponse.decode(new Reader(data))
    );
  }

  EditValidator(request: MsgEditValidator): Promise<MsgEditValidatorResponse> {
    const data = MsgEditValidator.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.staking.v1beta1.Msg",
      "EditValidator",
      data
    );
    return promise.then((data) =>
      MsgEditValidatorResponse.decode(new Reader(data))
    );
  }

  Delegate(request: MsgDelegate): Promise<MsgDelegateResponse> {
    const data = MsgDelegate.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.staking.v1beta1.Msg",
      "Delegate",
      data
    );
    return promise.then((data) => MsgDelegateResponse.decode(new Reader(data)));
  }

  BeginRedelegate(
    request: MsgBeginRedelegate
  ): Promise<MsgBeginRedelegateResponse> {
    const data = MsgBeginRedelegate.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.staking.v1beta1.Msg",
      "BeginRedelegate",
      data
    );
    return promise.then((data) =>
      MsgBeginRedelegateResponse.decode(new Reader(data))
    );
  }

  Undelegate(request: MsgUndelegate): Promise<MsgUndelegateResponse> {
    const data = MsgUndelegate.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.staking.v1beta1.Msg",
      "Undelegate",
      data
    );
    return promise.then((data) =>
      MsgUndelegateResponse.decode(new Reader(data))
    );
  }
}

interface Rpc {
  request(
    service: string,
    method: string,
    data: Uint8Array
  ): Promise<Uint8Array>;
}

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

function toTimestamp(date: Date): Timestamp {
  const seconds = date.getTime() / 1_000;
  const nanos = (date.getTime() % 1_000) * 1_000_000;
  return { seconds, nanos };
}

function fromTimestamp(t: Timestamp): Date {
  let millis = t.seconds * 1_000;
  millis += t.nanos / 1_000_000;
  return new Date(millis);
}

function fromJsonTimestamp(o: any): Date {
  if (o instanceof Date) {
    return o;
  } else if (typeof o === "string") {
    return new Date(o);
  } else {
    return fromTimestamp(Timestamp.fromJSON(o));
  }
}
