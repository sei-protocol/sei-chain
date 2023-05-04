/* eslint-disable */
import { Coin } from "../../../cosmos/base/v1beta1/coin";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.staking.v1beta1";

/**
 * AuthorizationType defines the type of staking module authorization type
 *
 * Since: cosmos-sdk 0.43
 */
export enum AuthorizationType {
  /** AUTHORIZATION_TYPE_UNSPECIFIED - AUTHORIZATION_TYPE_UNSPECIFIED specifies an unknown authorization type */
  AUTHORIZATION_TYPE_UNSPECIFIED = 0,
  /** AUTHORIZATION_TYPE_DELEGATE - AUTHORIZATION_TYPE_DELEGATE defines an authorization type for Msg/Delegate */
  AUTHORIZATION_TYPE_DELEGATE = 1,
  /** AUTHORIZATION_TYPE_UNDELEGATE - AUTHORIZATION_TYPE_UNDELEGATE defines an authorization type for Msg/Undelegate */
  AUTHORIZATION_TYPE_UNDELEGATE = 2,
  /** AUTHORIZATION_TYPE_REDELEGATE - AUTHORIZATION_TYPE_REDELEGATE defines an authorization type for Msg/BeginRedelegate */
  AUTHORIZATION_TYPE_REDELEGATE = 3,
  UNRECOGNIZED = -1,
}

export function authorizationTypeFromJSON(object: any): AuthorizationType {
  switch (object) {
    case 0:
    case "AUTHORIZATION_TYPE_UNSPECIFIED":
      return AuthorizationType.AUTHORIZATION_TYPE_UNSPECIFIED;
    case 1:
    case "AUTHORIZATION_TYPE_DELEGATE":
      return AuthorizationType.AUTHORIZATION_TYPE_DELEGATE;
    case 2:
    case "AUTHORIZATION_TYPE_UNDELEGATE":
      return AuthorizationType.AUTHORIZATION_TYPE_UNDELEGATE;
    case 3:
    case "AUTHORIZATION_TYPE_REDELEGATE":
      return AuthorizationType.AUTHORIZATION_TYPE_REDELEGATE;
    case -1:
    case "UNRECOGNIZED":
    default:
      return AuthorizationType.UNRECOGNIZED;
  }
}

export function authorizationTypeToJSON(object: AuthorizationType): string {
  switch (object) {
    case AuthorizationType.AUTHORIZATION_TYPE_UNSPECIFIED:
      return "AUTHORIZATION_TYPE_UNSPECIFIED";
    case AuthorizationType.AUTHORIZATION_TYPE_DELEGATE:
      return "AUTHORIZATION_TYPE_DELEGATE";
    case AuthorizationType.AUTHORIZATION_TYPE_UNDELEGATE:
      return "AUTHORIZATION_TYPE_UNDELEGATE";
    case AuthorizationType.AUTHORIZATION_TYPE_REDELEGATE:
      return "AUTHORIZATION_TYPE_REDELEGATE";
    default:
      return "UNKNOWN";
  }
}

/**
 * StakeAuthorization defines authorization for delegate/undelegate/redelegate.
 *
 * Since: cosmos-sdk 0.43
 */
export interface StakeAuthorization {
  /**
   * max_tokens specifies the maximum amount of tokens can be delegate to a validator. If it is
   * empty, there is no spend limit and any amount of coins can be delegated.
   */
  max_tokens: Coin | undefined;
  /**
   * allow_list specifies list of validator addresses to whom grantee can delegate tokens on behalf of granter's
   * account.
   */
  allow_list: StakeAuthorization_Validators | undefined;
  /** deny_list specifies list of validator addresses to whom grantee can not delegate tokens. */
  deny_list: StakeAuthorization_Validators | undefined;
  /** authorization_type defines one of AuthorizationType. */
  authorization_type: AuthorizationType;
}

/** Validators defines list of validator addresses. */
export interface StakeAuthorization_Validators {
  address: string[];
}

const baseStakeAuthorization: object = { authorization_type: 0 };

export const StakeAuthorization = {
  encode(
    message: StakeAuthorization,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.max_tokens !== undefined) {
      Coin.encode(message.max_tokens, writer.uint32(10).fork()).ldelim();
    }
    if (message.allow_list !== undefined) {
      StakeAuthorization_Validators.encode(
        message.allow_list,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.deny_list !== undefined) {
      StakeAuthorization_Validators.encode(
        message.deny_list,
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.authorization_type !== 0) {
      writer.uint32(32).int32(message.authorization_type);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): StakeAuthorization {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseStakeAuthorization } as StakeAuthorization;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.max_tokens = Coin.decode(reader, reader.uint32());
          break;
        case 2:
          message.allow_list = StakeAuthorization_Validators.decode(
            reader,
            reader.uint32()
          );
          break;
        case 3:
          message.deny_list = StakeAuthorization_Validators.decode(
            reader,
            reader.uint32()
          );
          break;
        case 4:
          message.authorization_type = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): StakeAuthorization {
    const message = { ...baseStakeAuthorization } as StakeAuthorization;
    if (object.max_tokens !== undefined && object.max_tokens !== null) {
      message.max_tokens = Coin.fromJSON(object.max_tokens);
    } else {
      message.max_tokens = undefined;
    }
    if (object.allow_list !== undefined && object.allow_list !== null) {
      message.allow_list = StakeAuthorization_Validators.fromJSON(
        object.allow_list
      );
    } else {
      message.allow_list = undefined;
    }
    if (object.deny_list !== undefined && object.deny_list !== null) {
      message.deny_list = StakeAuthorization_Validators.fromJSON(
        object.deny_list
      );
    } else {
      message.deny_list = undefined;
    }
    if (
      object.authorization_type !== undefined &&
      object.authorization_type !== null
    ) {
      message.authorization_type = authorizationTypeFromJSON(
        object.authorization_type
      );
    } else {
      message.authorization_type = 0;
    }
    return message;
  },

  toJSON(message: StakeAuthorization): unknown {
    const obj: any = {};
    message.max_tokens !== undefined &&
      (obj.max_tokens = message.max_tokens
        ? Coin.toJSON(message.max_tokens)
        : undefined);
    message.allow_list !== undefined &&
      (obj.allow_list = message.allow_list
        ? StakeAuthorization_Validators.toJSON(message.allow_list)
        : undefined);
    message.deny_list !== undefined &&
      (obj.deny_list = message.deny_list
        ? StakeAuthorization_Validators.toJSON(message.deny_list)
        : undefined);
    message.authorization_type !== undefined &&
      (obj.authorization_type = authorizationTypeToJSON(
        message.authorization_type
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<StakeAuthorization>): StakeAuthorization {
    const message = { ...baseStakeAuthorization } as StakeAuthorization;
    if (object.max_tokens !== undefined && object.max_tokens !== null) {
      message.max_tokens = Coin.fromPartial(object.max_tokens);
    } else {
      message.max_tokens = undefined;
    }
    if (object.allow_list !== undefined && object.allow_list !== null) {
      message.allow_list = StakeAuthorization_Validators.fromPartial(
        object.allow_list
      );
    } else {
      message.allow_list = undefined;
    }
    if (object.deny_list !== undefined && object.deny_list !== null) {
      message.deny_list = StakeAuthorization_Validators.fromPartial(
        object.deny_list
      );
    } else {
      message.deny_list = undefined;
    }
    if (
      object.authorization_type !== undefined &&
      object.authorization_type !== null
    ) {
      message.authorization_type = object.authorization_type;
    } else {
      message.authorization_type = 0;
    }
    return message;
  },
};

const baseStakeAuthorization_Validators: object = { address: "" };

export const StakeAuthorization_Validators = {
  encode(
    message: StakeAuthorization_Validators,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.address) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): StakeAuthorization_Validators {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseStakeAuthorization_Validators,
    } as StakeAuthorization_Validators;
    message.address = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): StakeAuthorization_Validators {
    const message = {
      ...baseStakeAuthorization_Validators,
    } as StakeAuthorization_Validators;
    message.address = [];
    if (object.address !== undefined && object.address !== null) {
      for (const e of object.address) {
        message.address.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: StakeAuthorization_Validators): unknown {
    const obj: any = {};
    if (message.address) {
      obj.address = message.address.map((e) => e);
    } else {
      obj.address = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<StakeAuthorization_Validators>
  ): StakeAuthorization_Validators {
    const message = {
      ...baseStakeAuthorization_Validators,
    } as StakeAuthorization_Validators;
    message.address = [];
    if (object.address !== undefined && object.address !== null) {
      for (const e of object.address) {
        message.address.push(e);
      }
    }
    return message;
  },
};

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
