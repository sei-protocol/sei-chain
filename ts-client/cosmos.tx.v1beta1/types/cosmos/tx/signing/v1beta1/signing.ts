/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Any } from "../../../../google/protobuf/any";
import { CompactBitArray } from "../../../../cosmos/crypto/multisig/v1beta1/multisig";

export const protobufPackage = "cosmos.tx.signing.v1beta1";

/** SignMode represents a signing mode with its own security guarantees. */
export enum SignMode {
  /**
   * SIGN_MODE_UNSPECIFIED - SIGN_MODE_UNSPECIFIED specifies an unknown signing mode and will be
   * rejected
   */
  SIGN_MODE_UNSPECIFIED = 0,
  /**
   * SIGN_MODE_DIRECT - SIGN_MODE_DIRECT specifies a signing mode which uses SignDoc and is
   * verified with raw bytes from Tx
   */
  SIGN_MODE_DIRECT = 1,
  /**
   * SIGN_MODE_TEXTUAL - SIGN_MODE_TEXTUAL is a future signing mode that will verify some
   * human-readable textual representation on top of the binary representation
   * from SIGN_MODE_DIRECT
   */
  SIGN_MODE_TEXTUAL = 2,
  /**
   * SIGN_MODE_LEGACY_AMINO_JSON - SIGN_MODE_LEGACY_AMINO_JSON is a backwards compatibility mode which uses
   * Amino JSON and will be removed in the future
   */
  SIGN_MODE_LEGACY_AMINO_JSON = 127,
  /**
   * SIGN_MODE_EIP_191 - SIGN_MODE_EIP_191 specifies the sign mode for EIP 191 signing on the Cosmos
   * SDK. Ref: https://eips.ethereum.org/EIPS/eip-191
   *
   * Currently, SIGN_MODE_EIP_191 is registered as a SignMode enum variant,
   * but is not implemented on the SDK by default. To enable EIP-191, you need
   * to pass a custom `TxConfig` that has an implementation of
   * `SignModeHandler` for EIP-191. The SDK may decide to fully support
   * EIP-191 in the future.
   *
   * Since: cosmos-sdk 0.45.2
   */
  SIGN_MODE_EIP_191 = 191,
  UNRECOGNIZED = -1,
}

export function signModeFromJSON(object: any): SignMode {
  switch (object) {
    case 0:
    case "SIGN_MODE_UNSPECIFIED":
      return SignMode.SIGN_MODE_UNSPECIFIED;
    case 1:
    case "SIGN_MODE_DIRECT":
      return SignMode.SIGN_MODE_DIRECT;
    case 2:
    case "SIGN_MODE_TEXTUAL":
      return SignMode.SIGN_MODE_TEXTUAL;
    case 127:
    case "SIGN_MODE_LEGACY_AMINO_JSON":
      return SignMode.SIGN_MODE_LEGACY_AMINO_JSON;
    case 191:
    case "SIGN_MODE_EIP_191":
      return SignMode.SIGN_MODE_EIP_191;
    case -1:
    case "UNRECOGNIZED":
    default:
      return SignMode.UNRECOGNIZED;
  }
}

export function signModeToJSON(object: SignMode): string {
  switch (object) {
    case SignMode.SIGN_MODE_UNSPECIFIED:
      return "SIGN_MODE_UNSPECIFIED";
    case SignMode.SIGN_MODE_DIRECT:
      return "SIGN_MODE_DIRECT";
    case SignMode.SIGN_MODE_TEXTUAL:
      return "SIGN_MODE_TEXTUAL";
    case SignMode.SIGN_MODE_LEGACY_AMINO_JSON:
      return "SIGN_MODE_LEGACY_AMINO_JSON";
    case SignMode.SIGN_MODE_EIP_191:
      return "SIGN_MODE_EIP_191";
    default:
      return "UNKNOWN";
  }
}

/** SignatureDescriptors wraps multiple SignatureDescriptor's. */
export interface SignatureDescriptors {
  /** signatures are the signature descriptors */
  signatures: SignatureDescriptor[];
}

/**
 * SignatureDescriptor is a convenience type which represents the full data for
 * a signature including the public key of the signer, signing modes and the
 * signature itself. It is primarily used for coordinating signatures between
 * clients.
 */
export interface SignatureDescriptor {
  /** public_key is the public key of the signer */
  publicKey: Any | undefined;
  data: SignatureDescriptor_Data | undefined;
  /**
   * sequence is the sequence of the account, which describes the
   * number of committed transactions signed by a given address. It is used to prevent
   * replay attacks.
   */
  sequence: number;
}

/** Data represents signature data */
export interface SignatureDescriptor_Data {
  /** single represents a single signer */
  single: SignatureDescriptor_Data_Single | undefined;
  /** multi represents a multisig signer */
  multi: SignatureDescriptor_Data_Multi | undefined;
}

/** Single is the signature data for a single signer */
export interface SignatureDescriptor_Data_Single {
  /** mode is the signing mode of the single signer */
  mode: SignMode;
  /** signature is the raw signature bytes */
  signature: Uint8Array;
}

/** Multi is the signature data for a multisig public key */
export interface SignatureDescriptor_Data_Multi {
  /** bitarray specifies which keys within the multisig are signing */
  bitarray: CompactBitArray | undefined;
  /** signatures is the signatures of the multi-signature */
  signatures: SignatureDescriptor_Data[];
}

const baseSignatureDescriptors: object = {};

export const SignatureDescriptors = {
  encode(
    message: SignatureDescriptors,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.signatures) {
      SignatureDescriptor.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SignatureDescriptors {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSignatureDescriptors } as SignatureDescriptors;
    message.signatures = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.signatures.push(
            SignatureDescriptor.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SignatureDescriptors {
    const message = { ...baseSignatureDescriptors } as SignatureDescriptors;
    message.signatures = [];
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(SignatureDescriptor.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: SignatureDescriptors): unknown {
    const obj: any = {};
    if (message.signatures) {
      obj.signatures = message.signatures.map((e) =>
        e ? SignatureDescriptor.toJSON(e) : undefined
      );
    } else {
      obj.signatures = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<SignatureDescriptors>): SignatureDescriptors {
    const message = { ...baseSignatureDescriptors } as SignatureDescriptors;
    message.signatures = [];
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(SignatureDescriptor.fromPartial(e));
      }
    }
    return message;
  },
};

const baseSignatureDescriptor: object = { sequence: 0 };

export const SignatureDescriptor = {
  encode(
    message: SignatureDescriptor,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.publicKey !== undefined) {
      Any.encode(message.publicKey, writer.uint32(10).fork()).ldelim();
    }
    if (message.data !== undefined) {
      SignatureDescriptor_Data.encode(
        message.data,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.sequence !== 0) {
      writer.uint32(24).uint64(message.sequence);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SignatureDescriptor {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSignatureDescriptor } as SignatureDescriptor;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.publicKey = Any.decode(reader, reader.uint32());
          break;
        case 2:
          message.data = SignatureDescriptor_Data.decode(
            reader,
            reader.uint32()
          );
          break;
        case 3:
          message.sequence = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SignatureDescriptor {
    const message = { ...baseSignatureDescriptor } as SignatureDescriptor;
    if (object.publicKey !== undefined && object.publicKey !== null) {
      message.publicKey = Any.fromJSON(object.publicKey);
    } else {
      message.publicKey = undefined;
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = SignatureDescriptor_Data.fromJSON(object.data);
    } else {
      message.data = undefined;
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = Number(object.sequence);
    } else {
      message.sequence = 0;
    }
    return message;
  },

  toJSON(message: SignatureDescriptor): unknown {
    const obj: any = {};
    message.publicKey !== undefined &&
      (obj.publicKey = message.publicKey
        ? Any.toJSON(message.publicKey)
        : undefined);
    message.data !== undefined &&
      (obj.data = message.data
        ? SignatureDescriptor_Data.toJSON(message.data)
        : undefined);
    message.sequence !== undefined && (obj.sequence = message.sequence);
    return obj;
  },

  fromPartial(object: DeepPartial<SignatureDescriptor>): SignatureDescriptor {
    const message = { ...baseSignatureDescriptor } as SignatureDescriptor;
    if (object.publicKey !== undefined && object.publicKey !== null) {
      message.publicKey = Any.fromPartial(object.publicKey);
    } else {
      message.publicKey = undefined;
    }
    if (object.data !== undefined && object.data !== null) {
      message.data = SignatureDescriptor_Data.fromPartial(object.data);
    } else {
      message.data = undefined;
    }
    if (object.sequence !== undefined && object.sequence !== null) {
      message.sequence = object.sequence;
    } else {
      message.sequence = 0;
    }
    return message;
  },
};

const baseSignatureDescriptor_Data: object = {};

export const SignatureDescriptor_Data = {
  encode(
    message: SignatureDescriptor_Data,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.single !== undefined) {
      SignatureDescriptor_Data_Single.encode(
        message.single,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.multi !== undefined) {
      SignatureDescriptor_Data_Multi.encode(
        message.multi,
        writer.uint32(18).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): SignatureDescriptor_Data {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseSignatureDescriptor_Data,
    } as SignatureDescriptor_Data;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.single = SignatureDescriptor_Data_Single.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.multi = SignatureDescriptor_Data_Multi.decode(
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

  fromJSON(object: any): SignatureDescriptor_Data {
    const message = {
      ...baseSignatureDescriptor_Data,
    } as SignatureDescriptor_Data;
    if (object.single !== undefined && object.single !== null) {
      message.single = SignatureDescriptor_Data_Single.fromJSON(object.single);
    } else {
      message.single = undefined;
    }
    if (object.multi !== undefined && object.multi !== null) {
      message.multi = SignatureDescriptor_Data_Multi.fromJSON(object.multi);
    } else {
      message.multi = undefined;
    }
    return message;
  },

  toJSON(message: SignatureDescriptor_Data): unknown {
    const obj: any = {};
    message.single !== undefined &&
      (obj.single = message.single
        ? SignatureDescriptor_Data_Single.toJSON(message.single)
        : undefined);
    message.multi !== undefined &&
      (obj.multi = message.multi
        ? SignatureDescriptor_Data_Multi.toJSON(message.multi)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<SignatureDescriptor_Data>
  ): SignatureDescriptor_Data {
    const message = {
      ...baseSignatureDescriptor_Data,
    } as SignatureDescriptor_Data;
    if (object.single !== undefined && object.single !== null) {
      message.single = SignatureDescriptor_Data_Single.fromPartial(
        object.single
      );
    } else {
      message.single = undefined;
    }
    if (object.multi !== undefined && object.multi !== null) {
      message.multi = SignatureDescriptor_Data_Multi.fromPartial(object.multi);
    } else {
      message.multi = undefined;
    }
    return message;
  },
};

const baseSignatureDescriptor_Data_Single: object = { mode: 0 };

export const SignatureDescriptor_Data_Single = {
  encode(
    message: SignatureDescriptor_Data_Single,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.mode !== 0) {
      writer.uint32(8).int32(message.mode);
    }
    if (message.signature.length !== 0) {
      writer.uint32(18).bytes(message.signature);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): SignatureDescriptor_Data_Single {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseSignatureDescriptor_Data_Single,
    } as SignatureDescriptor_Data_Single;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.mode = reader.int32() as any;
          break;
        case 2:
          message.signature = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SignatureDescriptor_Data_Single {
    const message = {
      ...baseSignatureDescriptor_Data_Single,
    } as SignatureDescriptor_Data_Single;
    if (object.mode !== undefined && object.mode !== null) {
      message.mode = signModeFromJSON(object.mode);
    } else {
      message.mode = 0;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = bytesFromBase64(object.signature);
    }
    return message;
  },

  toJSON(message: SignatureDescriptor_Data_Single): unknown {
    const obj: any = {};
    message.mode !== undefined && (obj.mode = signModeToJSON(message.mode));
    message.signature !== undefined &&
      (obj.signature = base64FromBytes(
        message.signature !== undefined ? message.signature : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<SignatureDescriptor_Data_Single>
  ): SignatureDescriptor_Data_Single {
    const message = {
      ...baseSignatureDescriptor_Data_Single,
    } as SignatureDescriptor_Data_Single;
    if (object.mode !== undefined && object.mode !== null) {
      message.mode = object.mode;
    } else {
      message.mode = 0;
    }
    if (object.signature !== undefined && object.signature !== null) {
      message.signature = object.signature;
    } else {
      message.signature = new Uint8Array();
    }
    return message;
  },
};

const baseSignatureDescriptor_Data_Multi: object = {};

export const SignatureDescriptor_Data_Multi = {
  encode(
    message: SignatureDescriptor_Data_Multi,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.bitarray !== undefined) {
      CompactBitArray.encode(
        message.bitarray,
        writer.uint32(10).fork()
      ).ldelim();
    }
    for (const v of message.signatures) {
      SignatureDescriptor_Data.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): SignatureDescriptor_Data_Multi {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseSignatureDescriptor_Data_Multi,
    } as SignatureDescriptor_Data_Multi;
    message.signatures = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.bitarray = CompactBitArray.decode(reader, reader.uint32());
          break;
        case 2:
          message.signatures.push(
            SignatureDescriptor_Data.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SignatureDescriptor_Data_Multi {
    const message = {
      ...baseSignatureDescriptor_Data_Multi,
    } as SignatureDescriptor_Data_Multi;
    message.signatures = [];
    if (object.bitarray !== undefined && object.bitarray !== null) {
      message.bitarray = CompactBitArray.fromJSON(object.bitarray);
    } else {
      message.bitarray = undefined;
    }
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(SignatureDescriptor_Data.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: SignatureDescriptor_Data_Multi): unknown {
    const obj: any = {};
    message.bitarray !== undefined &&
      (obj.bitarray = message.bitarray
        ? CompactBitArray.toJSON(message.bitarray)
        : undefined);
    if (message.signatures) {
      obj.signatures = message.signatures.map((e) =>
        e ? SignatureDescriptor_Data.toJSON(e) : undefined
      );
    } else {
      obj.signatures = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<SignatureDescriptor_Data_Multi>
  ): SignatureDescriptor_Data_Multi {
    const message = {
      ...baseSignatureDescriptor_Data_Multi,
    } as SignatureDescriptor_Data_Multi;
    message.signatures = [];
    if (object.bitarray !== undefined && object.bitarray !== null) {
      message.bitarray = CompactBitArray.fromPartial(object.bitarray);
    } else {
      message.bitarray = undefined;
    }
    if (object.signatures !== undefined && object.signatures !== null) {
      for (const e of object.signatures) {
        message.signatures.push(SignatureDescriptor_Data.fromPartial(e));
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
