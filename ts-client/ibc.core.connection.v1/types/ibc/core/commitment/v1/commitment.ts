/* eslint-disable */
import { CommitmentProof } from "../../../../proofs";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "ibc.core.commitment.v1";

/**
 * MerkleRoot defines a merkle root hash.
 * In the Cosmos SDK, the AppHash of a block header becomes the root.
 */
export interface MerkleRoot {
  hash: Uint8Array;
}

/**
 * MerklePrefix is merkle path prefixed to the key.
 * The constructed key from the Path and the key will be append(Path.KeyPath,
 * append(Path.KeyPrefix, key...))
 */
export interface MerklePrefix {
  keyPrefix: Uint8Array;
}

/**
 * MerklePath is the path used to verify commitment proofs, which can be an
 * arbitrary structured object (defined by a commitment type).
 * MerklePath is represented from root-to-leaf
 */
export interface MerklePath {
  keyPath: string[];
}

/**
 * MerkleProof is a wrapper type over a chain of CommitmentProofs.
 * It demonstrates membership or non-membership for an element or set of
 * elements, verifiable in conjunction with a known commitment root. Proofs
 * should be succinct.
 * MerkleProofs are ordered from leaf-to-root
 */
export interface MerkleProof {
  proofs: CommitmentProof[];
}

const baseMerkleRoot: object = {};

export const MerkleRoot = {
  encode(message: MerkleRoot, writer: Writer = Writer.create()): Writer {
    if (message.hash.length !== 0) {
      writer.uint32(10).bytes(message.hash);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MerkleRoot {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMerkleRoot } as MerkleRoot;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.hash = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MerkleRoot {
    const message = { ...baseMerkleRoot } as MerkleRoot;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = bytesFromBase64(object.hash);
    }
    return message;
  },

  toJSON(message: MerkleRoot): unknown {
    const obj: any = {};
    message.hash !== undefined &&
      (obj.hash = base64FromBytes(
        message.hash !== undefined ? message.hash : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<MerkleRoot>): MerkleRoot {
    const message = { ...baseMerkleRoot } as MerkleRoot;
    if (object.hash !== undefined && object.hash !== null) {
      message.hash = object.hash;
    } else {
      message.hash = new Uint8Array();
    }
    return message;
  },
};

const baseMerklePrefix: object = {};

export const MerklePrefix = {
  encode(message: MerklePrefix, writer: Writer = Writer.create()): Writer {
    if (message.keyPrefix.length !== 0) {
      writer.uint32(10).bytes(message.keyPrefix);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MerklePrefix {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMerklePrefix } as MerklePrefix;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.keyPrefix = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MerklePrefix {
    const message = { ...baseMerklePrefix } as MerklePrefix;
    if (object.keyPrefix !== undefined && object.keyPrefix !== null) {
      message.keyPrefix = bytesFromBase64(object.keyPrefix);
    }
    return message;
  },

  toJSON(message: MerklePrefix): unknown {
    const obj: any = {};
    message.keyPrefix !== undefined &&
      (obj.keyPrefix = base64FromBytes(
        message.keyPrefix !== undefined ? message.keyPrefix : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<MerklePrefix>): MerklePrefix {
    const message = { ...baseMerklePrefix } as MerklePrefix;
    if (object.keyPrefix !== undefined && object.keyPrefix !== null) {
      message.keyPrefix = object.keyPrefix;
    } else {
      message.keyPrefix = new Uint8Array();
    }
    return message;
  },
};

const baseMerklePath: object = { keyPath: "" };

export const MerklePath = {
  encode(message: MerklePath, writer: Writer = Writer.create()): Writer {
    for (const v of message.keyPath) {
      writer.uint32(10).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MerklePath {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMerklePath } as MerklePath;
    message.keyPath = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.keyPath.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MerklePath {
    const message = { ...baseMerklePath } as MerklePath;
    message.keyPath = [];
    if (object.keyPath !== undefined && object.keyPath !== null) {
      for (const e of object.keyPath) {
        message.keyPath.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: MerklePath): unknown {
    const obj: any = {};
    if (message.keyPath) {
      obj.keyPath = message.keyPath.map((e) => e);
    } else {
      obj.keyPath = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MerklePath>): MerklePath {
    const message = { ...baseMerklePath } as MerklePath;
    message.keyPath = [];
    if (object.keyPath !== undefined && object.keyPath !== null) {
      for (const e of object.keyPath) {
        message.keyPath.push(e);
      }
    }
    return message;
  },
};

const baseMerkleProof: object = {};

export const MerkleProof = {
  encode(message: MerkleProof, writer: Writer = Writer.create()): Writer {
    for (const v of message.proofs) {
      CommitmentProof.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MerkleProof {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMerkleProof } as MerkleProof;
    message.proofs = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.proofs.push(CommitmentProof.decode(reader, reader.uint32()));
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MerkleProof {
    const message = { ...baseMerkleProof } as MerkleProof;
    message.proofs = [];
    if (object.proofs !== undefined && object.proofs !== null) {
      for (const e of object.proofs) {
        message.proofs.push(CommitmentProof.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MerkleProof): unknown {
    const obj: any = {};
    if (message.proofs) {
      obj.proofs = message.proofs.map((e) =>
        e ? CommitmentProof.toJSON(e) : undefined
      );
    } else {
      obj.proofs = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MerkleProof>): MerkleProof {
    const message = { ...baseMerkleProof } as MerkleProof;
    message.proofs = [];
    if (object.proofs !== undefined && object.proofs !== null) {
      for (const e of object.proofs) {
        message.proofs.push(CommitmentProof.fromPartial(e));
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
