/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { PublicKey } from "../../tendermint/crypto/keys";

export const protobufPackage = "tendermint.types";

export interface ValidatorSet {
  validators: Validator[];
  proposer: Validator | undefined;
  total_voting_power: number;
}

export interface Validator {
  address: Uint8Array;
  pub_key: PublicKey | undefined;
  voting_power: number;
  proposer_priority: number;
}

export interface SimpleValidator {
  pub_key: PublicKey | undefined;
  voting_power: number;
}

const baseValidatorSet: object = { total_voting_power: 0 };

export const ValidatorSet = {
  encode(message: ValidatorSet, writer: Writer = Writer.create()): Writer {
    for (const v of message.validators) {
      Validator.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    if (message.proposer !== undefined) {
      Validator.encode(message.proposer, writer.uint32(18).fork()).ldelim();
    }
    if (message.total_voting_power !== 0) {
      writer.uint32(24).int64(message.total_voting_power);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ValidatorSet {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidatorSet } as ValidatorSet;
    message.validators = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.validators.push(Validator.decode(reader, reader.uint32()));
          break;
        case 2:
          message.proposer = Validator.decode(reader, reader.uint32());
          break;
        case 3:
          message.total_voting_power = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ValidatorSet {
    const message = { ...baseValidatorSet } as ValidatorSet;
    message.validators = [];
    if (object.validators !== undefined && object.validators !== null) {
      for (const e of object.validators) {
        message.validators.push(Validator.fromJSON(e));
      }
    }
    if (object.proposer !== undefined && object.proposer !== null) {
      message.proposer = Validator.fromJSON(object.proposer);
    } else {
      message.proposer = undefined;
    }
    if (
      object.total_voting_power !== undefined &&
      object.total_voting_power !== null
    ) {
      message.total_voting_power = Number(object.total_voting_power);
    } else {
      message.total_voting_power = 0;
    }
    return message;
  },

  toJSON(message: ValidatorSet): unknown {
    const obj: any = {};
    if (message.validators) {
      obj.validators = message.validators.map((e) =>
        e ? Validator.toJSON(e) : undefined
      );
    } else {
      obj.validators = [];
    }
    message.proposer !== undefined &&
      (obj.proposer = message.proposer
        ? Validator.toJSON(message.proposer)
        : undefined);
    message.total_voting_power !== undefined &&
      (obj.total_voting_power = message.total_voting_power);
    return obj;
  },

  fromPartial(object: DeepPartial<ValidatorSet>): ValidatorSet {
    const message = { ...baseValidatorSet } as ValidatorSet;
    message.validators = [];
    if (object.validators !== undefined && object.validators !== null) {
      for (const e of object.validators) {
        message.validators.push(Validator.fromPartial(e));
      }
    }
    if (object.proposer !== undefined && object.proposer !== null) {
      message.proposer = Validator.fromPartial(object.proposer);
    } else {
      message.proposer = undefined;
    }
    if (
      object.total_voting_power !== undefined &&
      object.total_voting_power !== null
    ) {
      message.total_voting_power = object.total_voting_power;
    } else {
      message.total_voting_power = 0;
    }
    return message;
  },
};

const baseValidator: object = { voting_power: 0, proposer_priority: 0 };

export const Validator = {
  encode(message: Validator, writer: Writer = Writer.create()): Writer {
    if (message.address.length !== 0) {
      writer.uint32(10).bytes(message.address);
    }
    if (message.pub_key !== undefined) {
      PublicKey.encode(message.pub_key, writer.uint32(18).fork()).ldelim();
    }
    if (message.voting_power !== 0) {
      writer.uint32(24).int64(message.voting_power);
    }
    if (message.proposer_priority !== 0) {
      writer.uint32(32).int64(message.proposer_priority);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Validator {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseValidator } as Validator;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.address = reader.bytes();
          break;
        case 2:
          message.pub_key = PublicKey.decode(reader, reader.uint32());
          break;
        case 3:
          message.voting_power = longToNumber(reader.int64() as Long);
          break;
        case 4:
          message.proposer_priority = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Validator {
    const message = { ...baseValidator } as Validator;
    if (object.address !== undefined && object.address !== null) {
      message.address = bytesFromBase64(object.address);
    }
    if (object.pub_key !== undefined && object.pub_key !== null) {
      message.pub_key = PublicKey.fromJSON(object.pub_key);
    } else {
      message.pub_key = undefined;
    }
    if (object.voting_power !== undefined && object.voting_power !== null) {
      message.voting_power = Number(object.voting_power);
    } else {
      message.voting_power = 0;
    }
    if (
      object.proposer_priority !== undefined &&
      object.proposer_priority !== null
    ) {
      message.proposer_priority = Number(object.proposer_priority);
    } else {
      message.proposer_priority = 0;
    }
    return message;
  },

  toJSON(message: Validator): unknown {
    const obj: any = {};
    message.address !== undefined &&
      (obj.address = base64FromBytes(
        message.address !== undefined ? message.address : new Uint8Array()
      ));
    message.pub_key !== undefined &&
      (obj.pub_key = message.pub_key
        ? PublicKey.toJSON(message.pub_key)
        : undefined);
    message.voting_power !== undefined &&
      (obj.voting_power = message.voting_power);
    message.proposer_priority !== undefined &&
      (obj.proposer_priority = message.proposer_priority);
    return obj;
  },

  fromPartial(object: DeepPartial<Validator>): Validator {
    const message = { ...baseValidator } as Validator;
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = new Uint8Array();
    }
    if (object.pub_key !== undefined && object.pub_key !== null) {
      message.pub_key = PublicKey.fromPartial(object.pub_key);
    } else {
      message.pub_key = undefined;
    }
    if (object.voting_power !== undefined && object.voting_power !== null) {
      message.voting_power = object.voting_power;
    } else {
      message.voting_power = 0;
    }
    if (
      object.proposer_priority !== undefined &&
      object.proposer_priority !== null
    ) {
      message.proposer_priority = object.proposer_priority;
    } else {
      message.proposer_priority = 0;
    }
    return message;
  },
};

const baseSimpleValidator: object = { voting_power: 0 };

export const SimpleValidator = {
  encode(message: SimpleValidator, writer: Writer = Writer.create()): Writer {
    if (message.pub_key !== undefined) {
      PublicKey.encode(message.pub_key, writer.uint32(10).fork()).ldelim();
    }
    if (message.voting_power !== 0) {
      writer.uint32(16).int64(message.voting_power);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SimpleValidator {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSimpleValidator } as SimpleValidator;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.pub_key = PublicKey.decode(reader, reader.uint32());
          break;
        case 2:
          message.voting_power = longToNumber(reader.int64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SimpleValidator {
    const message = { ...baseSimpleValidator } as SimpleValidator;
    if (object.pub_key !== undefined && object.pub_key !== null) {
      message.pub_key = PublicKey.fromJSON(object.pub_key);
    } else {
      message.pub_key = undefined;
    }
    if (object.voting_power !== undefined && object.voting_power !== null) {
      message.voting_power = Number(object.voting_power);
    } else {
      message.voting_power = 0;
    }
    return message;
  },

  toJSON(message: SimpleValidator): unknown {
    const obj: any = {};
    message.pub_key !== undefined &&
      (obj.pub_key = message.pub_key
        ? PublicKey.toJSON(message.pub_key)
        : undefined);
    message.voting_power !== undefined &&
      (obj.voting_power = message.voting_power);
    return obj;
  },

  fromPartial(object: DeepPartial<SimpleValidator>): SimpleValidator {
    const message = { ...baseSimpleValidator } as SimpleValidator;
    if (object.pub_key !== undefined && object.pub_key !== null) {
      message.pub_key = PublicKey.fromPartial(object.pub_key);
    } else {
      message.pub_key = undefined;
    }
    if (object.voting_power !== undefined && object.voting_power !== null) {
      message.voting_power = object.voting_power;
    } else {
      message.voting_power = 0;
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

const atob: (b64: string) => string =
  globalThis.atob ||
  ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64: string): Uint8Array {
  const bin = atob(b64);
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; ++i) {
    arr[i] = bin.charCodeAt(i);
  }
  return arr;
}

const btoa: (bin: string) => string =
  globalThis.btoa ||
  ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr: Uint8Array): string {
  const bin: string[] = [];
  for (let i = 0; i < arr.byteLength; ++i) {
    bin.push(String.fromCharCode(arr[i]));
  }
  return btoa(bin.join(""));
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
