/* eslint-disable */
import { Reader, Writer } from "protobufjs/minimal";
import { Coin } from "../../../cosmos/base/v1beta1/coin";

export const protobufPackage = "cosmos.distribution.v1beta1";

/**
 * MsgSetWithdrawAddress sets the withdraw address for
 * a delegator (or validator self-delegation).
 */
export interface MsgSetWithdrawAddress {
  delegator_address: string;
  withdraw_address: string;
}

/** MsgSetWithdrawAddressResponse defines the Msg/SetWithdrawAddress response type. */
export interface MsgSetWithdrawAddressResponse {}

/**
 * MsgWithdrawDelegatorReward represents delegation withdrawal to a delegator
 * from a single validator.
 */
export interface MsgWithdrawDelegatorReward {
  delegator_address: string;
  validator_address: string;
}

/** MsgWithdrawDelegatorRewardResponse defines the Msg/WithdrawDelegatorReward response type. */
export interface MsgWithdrawDelegatorRewardResponse {}

/**
 * MsgWithdrawValidatorCommission withdraws the full commission to the validator
 * address.
 */
export interface MsgWithdrawValidatorCommission {
  validator_address: string;
}

/** MsgWithdrawValidatorCommissionResponse defines the Msg/WithdrawValidatorCommission response type. */
export interface MsgWithdrawValidatorCommissionResponse {}

/**
 * MsgFundCommunityPool allows an account to directly
 * fund the community pool.
 */
export interface MsgFundCommunityPool {
  amount: Coin[];
  depositor: string;
}

/** MsgFundCommunityPoolResponse defines the Msg/FundCommunityPool response type. */
export interface MsgFundCommunityPoolResponse {}

const baseMsgSetWithdrawAddress: object = {
  delegator_address: "",
  withdraw_address: "",
};

export const MsgSetWithdrawAddress = {
  encode(
    message: MsgSetWithdrawAddress,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.withdraw_address !== "") {
      writer.uint32(18).string(message.withdraw_address);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgSetWithdrawAddress {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgSetWithdrawAddress } as MsgSetWithdrawAddress;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.withdraw_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgSetWithdrawAddress {
    const message = { ...baseMsgSetWithdrawAddress } as MsgSetWithdrawAddress;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.withdraw_address !== undefined &&
      object.withdraw_address !== null
    ) {
      message.withdraw_address = String(object.withdraw_address);
    } else {
      message.withdraw_address = "";
    }
    return message;
  },

  toJSON(message: MsgSetWithdrawAddress): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.withdraw_address !== undefined &&
      (obj.withdraw_address = message.withdraw_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgSetWithdrawAddress>
  ): MsgSetWithdrawAddress {
    const message = { ...baseMsgSetWithdrawAddress } as MsgSetWithdrawAddress;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.withdraw_address !== undefined &&
      object.withdraw_address !== null
    ) {
      message.withdraw_address = object.withdraw_address;
    } else {
      message.withdraw_address = "";
    }
    return message;
  },
};

const baseMsgSetWithdrawAddressResponse: object = {};

export const MsgSetWithdrawAddressResponse = {
  encode(
    _: MsgSetWithdrawAddressResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgSetWithdrawAddressResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgSetWithdrawAddressResponse,
    } as MsgSetWithdrawAddressResponse;
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

  fromJSON(_: any): MsgSetWithdrawAddressResponse {
    const message = {
      ...baseMsgSetWithdrawAddressResponse,
    } as MsgSetWithdrawAddressResponse;
    return message;
  },

  toJSON(_: MsgSetWithdrawAddressResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgSetWithdrawAddressResponse>
  ): MsgSetWithdrawAddressResponse {
    const message = {
      ...baseMsgSetWithdrawAddressResponse,
    } as MsgSetWithdrawAddressResponse;
    return message;
  },
};

const baseMsgWithdrawDelegatorReward: object = {
  delegator_address: "",
  validator_address: "",
};

export const MsgWithdrawDelegatorReward = {
  encode(
    message: MsgWithdrawDelegatorReward,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.delegator_address !== "") {
      writer.uint32(10).string(message.delegator_address);
    }
    if (message.validator_address !== "") {
      writer.uint32(18).string(message.validator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgWithdrawDelegatorReward {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgWithdrawDelegatorReward,
    } as MsgWithdrawDelegatorReward;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.delegator_address = reader.string();
          break;
        case 2:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgWithdrawDelegatorReward {
    const message = {
      ...baseMsgWithdrawDelegatorReward,
    } as MsgWithdrawDelegatorReward;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = String(object.delegator_address);
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: MsgWithdrawDelegatorReward): unknown {
    const obj: any = {};
    message.delegator_address !== undefined &&
      (obj.delegator_address = message.delegator_address);
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgWithdrawDelegatorReward>
  ): MsgWithdrawDelegatorReward {
    const message = {
      ...baseMsgWithdrawDelegatorReward,
    } as MsgWithdrawDelegatorReward;
    if (
      object.delegator_address !== undefined &&
      object.delegator_address !== null
    ) {
      message.delegator_address = object.delegator_address;
    } else {
      message.delegator_address = "";
    }
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const baseMsgWithdrawDelegatorRewardResponse: object = {};

export const MsgWithdrawDelegatorRewardResponse = {
  encode(
    _: MsgWithdrawDelegatorRewardResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgWithdrawDelegatorRewardResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgWithdrawDelegatorRewardResponse,
    } as MsgWithdrawDelegatorRewardResponse;
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

  fromJSON(_: any): MsgWithdrawDelegatorRewardResponse {
    const message = {
      ...baseMsgWithdrawDelegatorRewardResponse,
    } as MsgWithdrawDelegatorRewardResponse;
    return message;
  },

  toJSON(_: MsgWithdrawDelegatorRewardResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgWithdrawDelegatorRewardResponse>
  ): MsgWithdrawDelegatorRewardResponse {
    const message = {
      ...baseMsgWithdrawDelegatorRewardResponse,
    } as MsgWithdrawDelegatorRewardResponse;
    return message;
  },
};

const baseMsgWithdrawValidatorCommission: object = { validator_address: "" };

export const MsgWithdrawValidatorCommission = {
  encode(
    message: MsgWithdrawValidatorCommission,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.validator_address !== "") {
      writer.uint32(10).string(message.validator_address);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgWithdrawValidatorCommission {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgWithdrawValidatorCommission,
    } as MsgWithdrawValidatorCommission;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validator_address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgWithdrawValidatorCommission {
    const message = {
      ...baseMsgWithdrawValidatorCommission,
    } as MsgWithdrawValidatorCommission;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = String(object.validator_address);
    } else {
      message.validator_address = "";
    }
    return message;
  },

  toJSON(message: MsgWithdrawValidatorCommission): unknown {
    const obj: any = {};
    message.validator_address !== undefined &&
      (obj.validator_address = message.validator_address);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MsgWithdrawValidatorCommission>
  ): MsgWithdrawValidatorCommission {
    const message = {
      ...baseMsgWithdrawValidatorCommission,
    } as MsgWithdrawValidatorCommission;
    if (
      object.validator_address !== undefined &&
      object.validator_address !== null
    ) {
      message.validator_address = object.validator_address;
    } else {
      message.validator_address = "";
    }
    return message;
  },
};

const baseMsgWithdrawValidatorCommissionResponse: object = {};

export const MsgWithdrawValidatorCommissionResponse = {
  encode(
    _: MsgWithdrawValidatorCommissionResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgWithdrawValidatorCommissionResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgWithdrawValidatorCommissionResponse,
    } as MsgWithdrawValidatorCommissionResponse;
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

  fromJSON(_: any): MsgWithdrawValidatorCommissionResponse {
    const message = {
      ...baseMsgWithdrawValidatorCommissionResponse,
    } as MsgWithdrawValidatorCommissionResponse;
    return message;
  },

  toJSON(_: MsgWithdrawValidatorCommissionResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgWithdrawValidatorCommissionResponse>
  ): MsgWithdrawValidatorCommissionResponse {
    const message = {
      ...baseMsgWithdrawValidatorCommissionResponse,
    } as MsgWithdrawValidatorCommissionResponse;
    return message;
  },
};

const baseMsgFundCommunityPool: object = { depositor: "" };

export const MsgFundCommunityPool = {
  encode(
    message: MsgFundCommunityPool,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.amount) {
      Coin.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.depositor !== "") {
      writer.uint32(18).string(message.depositor);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MsgFundCommunityPool {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMsgFundCommunityPool } as MsgFundCommunityPool;
    message.amount = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.amount.push(Coin.decode(reader, reader.uint32()));
          break;
        case 2:
          message.depositor = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MsgFundCommunityPool {
    const message = { ...baseMsgFundCommunityPool } as MsgFundCommunityPool;
    message.amount = [];
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromJSON(e));
      }
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = String(object.depositor);
    } else {
      message.depositor = "";
    }
    return message;
  },

  toJSON(message: MsgFundCommunityPool): unknown {
    const obj: any = {};
    if (message.amount) {
      obj.amount = message.amount.map((e) => (e ? Coin.toJSON(e) : undefined));
    } else {
      obj.amount = [];
    }
    message.depositor !== undefined && (obj.depositor = message.depositor);
    return obj;
  },

  fromPartial(object: DeepPartial<MsgFundCommunityPool>): MsgFundCommunityPool {
    const message = { ...baseMsgFundCommunityPool } as MsgFundCommunityPool;
    message.amount = [];
    if (object.amount !== undefined && object.amount !== null) {
      for (const e of object.amount) {
        message.amount.push(Coin.fromPartial(e));
      }
    }
    if (object.depositor !== undefined && object.depositor !== null) {
      message.depositor = object.depositor;
    } else {
      message.depositor = "";
    }
    return message;
  },
};

const baseMsgFundCommunityPoolResponse: object = {};

export const MsgFundCommunityPoolResponse = {
  encode(
    _: MsgFundCommunityPoolResponse,
    writer: Writer = Writer.create()
  ): Writer {
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MsgFundCommunityPoolResponse {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMsgFundCommunityPoolResponse,
    } as MsgFundCommunityPoolResponse;
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

  fromJSON(_: any): MsgFundCommunityPoolResponse {
    const message = {
      ...baseMsgFundCommunityPoolResponse,
    } as MsgFundCommunityPoolResponse;
    return message;
  },

  toJSON(_: MsgFundCommunityPoolResponse): unknown {
    const obj: any = {};
    return obj;
  },

  fromPartial(
    _: DeepPartial<MsgFundCommunityPoolResponse>
  ): MsgFundCommunityPoolResponse {
    const message = {
      ...baseMsgFundCommunityPoolResponse,
    } as MsgFundCommunityPoolResponse;
    return message;
  },
};

/** Msg defines the distribution Msg service. */
export interface Msg {
  /**
   * SetWithdrawAddress defines a method to change the withdraw address
   * for a delegator (or validator self-delegation).
   */
  SetWithdrawAddress(
    request: MsgSetWithdrawAddress
  ): Promise<MsgSetWithdrawAddressResponse>;
  /**
   * WithdrawDelegatorReward defines a method to withdraw rewards of delegator
   * from a single validator.
   */
  WithdrawDelegatorReward(
    request: MsgWithdrawDelegatorReward
  ): Promise<MsgWithdrawDelegatorRewardResponse>;
  /**
   * WithdrawValidatorCommission defines a method to withdraw the
   * full commission to the validator address.
   */
  WithdrawValidatorCommission(
    request: MsgWithdrawValidatorCommission
  ): Promise<MsgWithdrawValidatorCommissionResponse>;
  /**
   * FundCommunityPool defines a method to allow an account to directly
   * fund the community pool.
   */
  FundCommunityPool(
    request: MsgFundCommunityPool
  ): Promise<MsgFundCommunityPoolResponse>;
}

export class MsgClientImpl implements Msg {
  private readonly rpc: Rpc;
  constructor(rpc: Rpc) {
    this.rpc = rpc;
  }
  SetWithdrawAddress(
    request: MsgSetWithdrawAddress
  ): Promise<MsgSetWithdrawAddressResponse> {
    const data = MsgSetWithdrawAddress.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Msg",
      "SetWithdrawAddress",
      data
    );
    return promise.then((data) =>
      MsgSetWithdrawAddressResponse.decode(new Reader(data))
    );
  }

  WithdrawDelegatorReward(
    request: MsgWithdrawDelegatorReward
  ): Promise<MsgWithdrawDelegatorRewardResponse> {
    const data = MsgWithdrawDelegatorReward.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Msg",
      "WithdrawDelegatorReward",
      data
    );
    return promise.then((data) =>
      MsgWithdrawDelegatorRewardResponse.decode(new Reader(data))
    );
  }

  WithdrawValidatorCommission(
    request: MsgWithdrawValidatorCommission
  ): Promise<MsgWithdrawValidatorCommissionResponse> {
    const data = MsgWithdrawValidatorCommission.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Msg",
      "WithdrawValidatorCommission",
      data
    );
    return promise.then((data) =>
      MsgWithdrawValidatorCommissionResponse.decode(new Reader(data))
    );
  }

  FundCommunityPool(
    request: MsgFundCommunityPool
  ): Promise<MsgFundCommunityPoolResponse> {
    const data = MsgFundCommunityPool.encode(request).finish();
    const promise = this.rpc.request(
      "cosmos.distribution.v1beta1.Msg",
      "FundCommunityPool",
      data
    );
    return promise.then((data) =>
      MsgFundCommunityPoolResponse.decode(new Reader(data))
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
