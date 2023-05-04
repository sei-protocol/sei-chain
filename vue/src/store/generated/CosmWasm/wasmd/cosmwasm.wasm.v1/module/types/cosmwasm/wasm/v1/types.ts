/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
import { Any } from "../../../google/protobuf/any";

export const protobufPackage = "cosmwasm.wasm.v1";

/** AccessType permission types */
export enum AccessType {
  /** ACCESS_TYPE_UNSPECIFIED - AccessTypeUnspecified placeholder for empty value */
  ACCESS_TYPE_UNSPECIFIED = 0,
  /** ACCESS_TYPE_NOBODY - AccessTypeNobody forbidden */
  ACCESS_TYPE_NOBODY = 1,
  /** ACCESS_TYPE_ONLY_ADDRESS - AccessTypeOnlyAddress restricted to an address */
  ACCESS_TYPE_ONLY_ADDRESS = 2,
  /** ACCESS_TYPE_EVERYBODY - AccessTypeEverybody unrestricted */
  ACCESS_TYPE_EVERYBODY = 3,
  UNRECOGNIZED = -1,
}

export function accessTypeFromJSON(object: any): AccessType {
  switch (object) {
    case 0:
    case "ACCESS_TYPE_UNSPECIFIED":
      return AccessType.ACCESS_TYPE_UNSPECIFIED;
    case 1:
    case "ACCESS_TYPE_NOBODY":
      return AccessType.ACCESS_TYPE_NOBODY;
    case 2:
    case "ACCESS_TYPE_ONLY_ADDRESS":
      return AccessType.ACCESS_TYPE_ONLY_ADDRESS;
    case 3:
    case "ACCESS_TYPE_EVERYBODY":
      return AccessType.ACCESS_TYPE_EVERYBODY;
    case -1:
    case "UNRECOGNIZED":
    default:
      return AccessType.UNRECOGNIZED;
  }
}

export function accessTypeToJSON(object: AccessType): string {
  switch (object) {
    case AccessType.ACCESS_TYPE_UNSPECIFIED:
      return "ACCESS_TYPE_UNSPECIFIED";
    case AccessType.ACCESS_TYPE_NOBODY:
      return "ACCESS_TYPE_NOBODY";
    case AccessType.ACCESS_TYPE_ONLY_ADDRESS:
      return "ACCESS_TYPE_ONLY_ADDRESS";
    case AccessType.ACCESS_TYPE_EVERYBODY:
      return "ACCESS_TYPE_EVERYBODY";
    default:
      return "UNKNOWN";
  }
}

/** ContractCodeHistoryOperationType actions that caused a code change */
export enum ContractCodeHistoryOperationType {
  /** CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED - ContractCodeHistoryOperationTypeUnspecified placeholder for empty value */
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED = 0,
  /** CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT - ContractCodeHistoryOperationTypeInit on chain contract instantiation */
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT = 1,
  /** CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE - ContractCodeHistoryOperationTypeMigrate code migration */
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE = 2,
  /** CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS - ContractCodeHistoryOperationTypeGenesis based on genesis data */
  CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS = 3,
  UNRECOGNIZED = -1,
}

export function contractCodeHistoryOperationTypeFromJSON(
  object: any
): ContractCodeHistoryOperationType {
  switch (object) {
    case 0:
    case "CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED":
      return ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED;
    case 1:
    case "CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT":
      return ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT;
    case 2:
    case "CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE":
      return ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE;
    case 3:
    case "CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS":
      return ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS;
    case -1:
    case "UNRECOGNIZED":
    default:
      return ContractCodeHistoryOperationType.UNRECOGNIZED;
  }
}

export function contractCodeHistoryOperationTypeToJSON(
  object: ContractCodeHistoryOperationType
): string {
  switch (object) {
    case ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED:
      return "CONTRACT_CODE_HISTORY_OPERATION_TYPE_UNSPECIFIED";
    case ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT:
      return "CONTRACT_CODE_HISTORY_OPERATION_TYPE_INIT";
    case ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE:
      return "CONTRACT_CODE_HISTORY_OPERATION_TYPE_MIGRATE";
    case ContractCodeHistoryOperationType.CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS:
      return "CONTRACT_CODE_HISTORY_OPERATION_TYPE_GENESIS";
    default:
      return "UNKNOWN";
  }
}

/** AccessTypeParam */
export interface AccessTypeParam {
  value: AccessType;
}

/** AccessConfig access control type. */
export interface AccessConfig {
  permission: AccessType;
  address: string;
}

/** Params defines the set of wasm parameters. */
export interface Params {
  code_upload_access: AccessConfig | undefined;
  instantiate_default_permission: AccessType;
}

/** CodeInfo is data for the uploaded contract WASM code */
export interface CodeInfo {
  /** CodeHash is the unique identifier created by wasmvm */
  code_hash: Uint8Array;
  /** Creator address who initially stored the code */
  creator: string;
  /** InstantiateConfig access control to apply on contract creation, optional */
  instantiate_config: AccessConfig | undefined;
}

/** ContractInfo stores a WASM contract instance */
export interface ContractInfo {
  /** CodeID is the reference to the stored Wasm code */
  code_id: number;
  /** Creator address who initially instantiated the contract */
  creator: string;
  /** Admin is an optional address that can execute migrations */
  admin: string;
  /** Label is optional metadata to be stored with a contract instance. */
  label: string;
  /**
   * Created Tx position when the contract was instantiated.
   * This data should kept internal and not be exposed via query results. Just
   * use for sorting
   */
  created: AbsoluteTxPosition | undefined;
  ibc_port_id: string;
  /**
   * Extension is an extension point to store custom metadata within the
   * persistence model.
   */
  extension: Any | undefined;
}

/** ContractCodeHistoryEntry metadata to a contract. */
export interface ContractCodeHistoryEntry {
  operation: ContractCodeHistoryOperationType;
  /** CodeID is the reference to the stored WASM code */
  code_id: number;
  /** Updated Tx position when the operation was executed. */
  updated: AbsoluteTxPosition | undefined;
  msg: Uint8Array;
}

/**
 * AbsoluteTxPosition is a unique transaction position that allows for global
 * ordering of transactions.
 */
export interface AbsoluteTxPosition {
  /** BlockHeight is the block the contract was created at */
  block_height: number;
  /**
   * TxIndex is a monotonic counter within the block (actual transaction index,
   * or gas consumed)
   */
  tx_index: number;
}

/** Model is a struct that holds a KV pair */
export interface Model {
  /** hex-encode key to read it better (this is often ascii) */
  key: Uint8Array;
  /** base64-encode raw value */
  value: Uint8Array;
}

const baseAccessTypeParam: object = { value: 0 };

export const AccessTypeParam = {
  encode(message: AccessTypeParam, writer: Writer = Writer.create()): Writer {
    if (message.value !== 0) {
      writer.uint32(8).int32(message.value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): AccessTypeParam {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseAccessTypeParam } as AccessTypeParam;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.value = reader.int32() as any;
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AccessTypeParam {
    const message = { ...baseAccessTypeParam } as AccessTypeParam;
    if (object.value !== undefined && object.value !== null) {
      message.value = accessTypeFromJSON(object.value);
    } else {
      message.value = 0;
    }
    return message;
  },

  toJSON(message: AccessTypeParam): unknown {
    const obj: any = {};
    message.value !== undefined &&
      (obj.value = accessTypeToJSON(message.value));
    return obj;
  },

  fromPartial(object: DeepPartial<AccessTypeParam>): AccessTypeParam {
    const message = { ...baseAccessTypeParam } as AccessTypeParam;
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = 0;
    }
    return message;
  },
};

const baseAccessConfig: object = { permission: 0, address: "" };

export const AccessConfig = {
  encode(message: AccessConfig, writer: Writer = Writer.create()): Writer {
    if (message.permission !== 0) {
      writer.uint32(8).int32(message.permission);
    }
    if (message.address !== "") {
      writer.uint32(18).string(message.address);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): AccessConfig {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseAccessConfig } as AccessConfig;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.permission = reader.int32() as any;
          break;
        case 2:
          message.address = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AccessConfig {
    const message = { ...baseAccessConfig } as AccessConfig;
    if (object.permission !== undefined && object.permission !== null) {
      message.permission = accessTypeFromJSON(object.permission);
    } else {
      message.permission = 0;
    }
    if (object.address !== undefined && object.address !== null) {
      message.address = String(object.address);
    } else {
      message.address = "";
    }
    return message;
  },

  toJSON(message: AccessConfig): unknown {
    const obj: any = {};
    message.permission !== undefined &&
      (obj.permission = accessTypeToJSON(message.permission));
    message.address !== undefined && (obj.address = message.address);
    return obj;
  },

  fromPartial(object: DeepPartial<AccessConfig>): AccessConfig {
    const message = { ...baseAccessConfig } as AccessConfig;
    if (object.permission !== undefined && object.permission !== null) {
      message.permission = object.permission;
    } else {
      message.permission = 0;
    }
    if (object.address !== undefined && object.address !== null) {
      message.address = object.address;
    } else {
      message.address = "";
    }
    return message;
  },
};

const baseParams: object = { instantiate_default_permission: 0 };

export const Params = {
  encode(message: Params, writer: Writer = Writer.create()): Writer {
    if (message.code_upload_access !== undefined) {
      AccessConfig.encode(
        message.code_upload_access,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.instantiate_default_permission !== 0) {
      writer.uint32(16).int32(message.instantiate_default_permission);
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
          message.code_upload_access = AccessConfig.decode(
            reader,
            reader.uint32()
          );
          break;
        case 2:
          message.instantiate_default_permission = reader.int32() as any;
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
    if (
      object.code_upload_access !== undefined &&
      object.code_upload_access !== null
    ) {
      message.code_upload_access = AccessConfig.fromJSON(
        object.code_upload_access
      );
    } else {
      message.code_upload_access = undefined;
    }
    if (
      object.instantiate_default_permission !== undefined &&
      object.instantiate_default_permission !== null
    ) {
      message.instantiate_default_permission = accessTypeFromJSON(
        object.instantiate_default_permission
      );
    } else {
      message.instantiate_default_permission = 0;
    }
    return message;
  },

  toJSON(message: Params): unknown {
    const obj: any = {};
    message.code_upload_access !== undefined &&
      (obj.code_upload_access = message.code_upload_access
        ? AccessConfig.toJSON(message.code_upload_access)
        : undefined);
    message.instantiate_default_permission !== undefined &&
      (obj.instantiate_default_permission = accessTypeToJSON(
        message.instantiate_default_permission
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Params>): Params {
    const message = { ...baseParams } as Params;
    if (
      object.code_upload_access !== undefined &&
      object.code_upload_access !== null
    ) {
      message.code_upload_access = AccessConfig.fromPartial(
        object.code_upload_access
      );
    } else {
      message.code_upload_access = undefined;
    }
    if (
      object.instantiate_default_permission !== undefined &&
      object.instantiate_default_permission !== null
    ) {
      message.instantiate_default_permission =
        object.instantiate_default_permission;
    } else {
      message.instantiate_default_permission = 0;
    }
    return message;
  },
};

const baseCodeInfo: object = { creator: "" };

export const CodeInfo = {
  encode(message: CodeInfo, writer: Writer = Writer.create()): Writer {
    if (message.code_hash.length !== 0) {
      writer.uint32(10).bytes(message.code_hash);
    }
    if (message.creator !== "") {
      writer.uint32(18).string(message.creator);
    }
    if (message.instantiate_config !== undefined) {
      AccessConfig.encode(
        message.instantiate_config,
        writer.uint32(42).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): CodeInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseCodeInfo } as CodeInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.code_hash = reader.bytes();
          break;
        case 2:
          message.creator = reader.string();
          break;
        case 5:
          message.instantiate_config = AccessConfig.decode(
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

  fromJSON(object: any): CodeInfo {
    const message = { ...baseCodeInfo } as CodeInfo;
    if (object.code_hash !== undefined && object.code_hash !== null) {
      message.code_hash = bytesFromBase64(object.code_hash);
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (
      object.instantiate_config !== undefined &&
      object.instantiate_config !== null
    ) {
      message.instantiate_config = AccessConfig.fromJSON(
        object.instantiate_config
      );
    } else {
      message.instantiate_config = undefined;
    }
    return message;
  },

  toJSON(message: CodeInfo): unknown {
    const obj: any = {};
    message.code_hash !== undefined &&
      (obj.code_hash = base64FromBytes(
        message.code_hash !== undefined ? message.code_hash : new Uint8Array()
      ));
    message.creator !== undefined && (obj.creator = message.creator);
    message.instantiate_config !== undefined &&
      (obj.instantiate_config = message.instantiate_config
        ? AccessConfig.toJSON(message.instantiate_config)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<CodeInfo>): CodeInfo {
    const message = { ...baseCodeInfo } as CodeInfo;
    if (object.code_hash !== undefined && object.code_hash !== null) {
      message.code_hash = object.code_hash;
    } else {
      message.code_hash = new Uint8Array();
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (
      object.instantiate_config !== undefined &&
      object.instantiate_config !== null
    ) {
      message.instantiate_config = AccessConfig.fromPartial(
        object.instantiate_config
      );
    } else {
      message.instantiate_config = undefined;
    }
    return message;
  },
};

const baseContractInfo: object = {
  code_id: 0,
  creator: "",
  admin: "",
  label: "",
  ibc_port_id: "",
};

export const ContractInfo = {
  encode(message: ContractInfo, writer: Writer = Writer.create()): Writer {
    if (message.code_id !== 0) {
      writer.uint32(8).uint64(message.code_id);
    }
    if (message.creator !== "") {
      writer.uint32(18).string(message.creator);
    }
    if (message.admin !== "") {
      writer.uint32(26).string(message.admin);
    }
    if (message.label !== "") {
      writer.uint32(34).string(message.label);
    }
    if (message.created !== undefined) {
      AbsoluteTxPosition.encode(
        message.created,
        writer.uint32(42).fork()
      ).ldelim();
    }
    if (message.ibc_port_id !== "") {
      writer.uint32(50).string(message.ibc_port_id);
    }
    if (message.extension !== undefined) {
      Any.encode(message.extension, writer.uint32(58).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ContractInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseContractInfo } as ContractInfo;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.code_id = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.creator = reader.string();
          break;
        case 3:
          message.admin = reader.string();
          break;
        case 4:
          message.label = reader.string();
          break;
        case 5:
          message.created = AbsoluteTxPosition.decode(reader, reader.uint32());
          break;
        case 6:
          message.ibc_port_id = reader.string();
          break;
        case 7:
          message.extension = Any.decode(reader, reader.uint32());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractInfo {
    const message = { ...baseContractInfo } as ContractInfo;
    if (object.code_id !== undefined && object.code_id !== null) {
      message.code_id = Number(object.code_id);
    } else {
      message.code_id = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = String(object.creator);
    } else {
      message.creator = "";
    }
    if (object.admin !== undefined && object.admin !== null) {
      message.admin = String(object.admin);
    } else {
      message.admin = "";
    }
    if (object.label !== undefined && object.label !== null) {
      message.label = String(object.label);
    } else {
      message.label = "";
    }
    if (object.created !== undefined && object.created !== null) {
      message.created = AbsoluteTxPosition.fromJSON(object.created);
    } else {
      message.created = undefined;
    }
    if (object.ibc_port_id !== undefined && object.ibc_port_id !== null) {
      message.ibc_port_id = String(object.ibc_port_id);
    } else {
      message.ibc_port_id = "";
    }
    if (object.extension !== undefined && object.extension !== null) {
      message.extension = Any.fromJSON(object.extension);
    } else {
      message.extension = undefined;
    }
    return message;
  },

  toJSON(message: ContractInfo): unknown {
    const obj: any = {};
    message.code_id !== undefined && (obj.code_id = message.code_id);
    message.creator !== undefined && (obj.creator = message.creator);
    message.admin !== undefined && (obj.admin = message.admin);
    message.label !== undefined && (obj.label = message.label);
    message.created !== undefined &&
      (obj.created = message.created
        ? AbsoluteTxPosition.toJSON(message.created)
        : undefined);
    message.ibc_port_id !== undefined &&
      (obj.ibc_port_id = message.ibc_port_id);
    message.extension !== undefined &&
      (obj.extension = message.extension
        ? Any.toJSON(message.extension)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<ContractInfo>): ContractInfo {
    const message = { ...baseContractInfo } as ContractInfo;
    if (object.code_id !== undefined && object.code_id !== null) {
      message.code_id = object.code_id;
    } else {
      message.code_id = 0;
    }
    if (object.creator !== undefined && object.creator !== null) {
      message.creator = object.creator;
    } else {
      message.creator = "";
    }
    if (object.admin !== undefined && object.admin !== null) {
      message.admin = object.admin;
    } else {
      message.admin = "";
    }
    if (object.label !== undefined && object.label !== null) {
      message.label = object.label;
    } else {
      message.label = "";
    }
    if (object.created !== undefined && object.created !== null) {
      message.created = AbsoluteTxPosition.fromPartial(object.created);
    } else {
      message.created = undefined;
    }
    if (object.ibc_port_id !== undefined && object.ibc_port_id !== null) {
      message.ibc_port_id = object.ibc_port_id;
    } else {
      message.ibc_port_id = "";
    }
    if (object.extension !== undefined && object.extension !== null) {
      message.extension = Any.fromPartial(object.extension);
    } else {
      message.extension = undefined;
    }
    return message;
  },
};

const baseContractCodeHistoryEntry: object = { operation: 0, code_id: 0 };

export const ContractCodeHistoryEntry = {
  encode(
    message: ContractCodeHistoryEntry,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.operation !== 0) {
      writer.uint32(8).int32(message.operation);
    }
    if (message.code_id !== 0) {
      writer.uint32(16).uint64(message.code_id);
    }
    if (message.updated !== undefined) {
      AbsoluteTxPosition.encode(
        message.updated,
        writer.uint32(26).fork()
      ).ldelim();
    }
    if (message.msg.length !== 0) {
      writer.uint32(34).bytes(message.msg);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): ContractCodeHistoryEntry {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseContractCodeHistoryEntry,
    } as ContractCodeHistoryEntry;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.operation = reader.int32() as any;
          break;
        case 2:
          message.code_id = longToNumber(reader.uint64() as Long);
          break;
        case 3:
          message.updated = AbsoluteTxPosition.decode(reader, reader.uint32());
          break;
        case 4:
          message.msg = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ContractCodeHistoryEntry {
    const message = {
      ...baseContractCodeHistoryEntry,
    } as ContractCodeHistoryEntry;
    if (object.operation !== undefined && object.operation !== null) {
      message.operation = contractCodeHistoryOperationTypeFromJSON(
        object.operation
      );
    } else {
      message.operation = 0;
    }
    if (object.code_id !== undefined && object.code_id !== null) {
      message.code_id = Number(object.code_id);
    } else {
      message.code_id = 0;
    }
    if (object.updated !== undefined && object.updated !== null) {
      message.updated = AbsoluteTxPosition.fromJSON(object.updated);
    } else {
      message.updated = undefined;
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = bytesFromBase64(object.msg);
    }
    return message;
  },

  toJSON(message: ContractCodeHistoryEntry): unknown {
    const obj: any = {};
    message.operation !== undefined &&
      (obj.operation = contractCodeHistoryOperationTypeToJSON(
        message.operation
      ));
    message.code_id !== undefined && (obj.code_id = message.code_id);
    message.updated !== undefined &&
      (obj.updated = message.updated
        ? AbsoluteTxPosition.toJSON(message.updated)
        : undefined);
    message.msg !== undefined &&
      (obj.msg = base64FromBytes(
        message.msg !== undefined ? message.msg : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(
    object: DeepPartial<ContractCodeHistoryEntry>
  ): ContractCodeHistoryEntry {
    const message = {
      ...baseContractCodeHistoryEntry,
    } as ContractCodeHistoryEntry;
    if (object.operation !== undefined && object.operation !== null) {
      message.operation = object.operation;
    } else {
      message.operation = 0;
    }
    if (object.code_id !== undefined && object.code_id !== null) {
      message.code_id = object.code_id;
    } else {
      message.code_id = 0;
    }
    if (object.updated !== undefined && object.updated !== null) {
      message.updated = AbsoluteTxPosition.fromPartial(object.updated);
    } else {
      message.updated = undefined;
    }
    if (object.msg !== undefined && object.msg !== null) {
      message.msg = object.msg;
    } else {
      message.msg = new Uint8Array();
    }
    return message;
  },
};

const baseAbsoluteTxPosition: object = { block_height: 0, tx_index: 0 };

export const AbsoluteTxPosition = {
  encode(
    message: AbsoluteTxPosition,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.block_height !== 0) {
      writer.uint32(8).uint64(message.block_height);
    }
    if (message.tx_index !== 0) {
      writer.uint32(16).uint64(message.tx_index);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): AbsoluteTxPosition {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseAbsoluteTxPosition } as AbsoluteTxPosition;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.block_height = longToNumber(reader.uint64() as Long);
          break;
        case 2:
          message.tx_index = longToNumber(reader.uint64() as Long);
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AbsoluteTxPosition {
    const message = { ...baseAbsoluteTxPosition } as AbsoluteTxPosition;
    if (object.block_height !== undefined && object.block_height !== null) {
      message.block_height = Number(object.block_height);
    } else {
      message.block_height = 0;
    }
    if (object.tx_index !== undefined && object.tx_index !== null) {
      message.tx_index = Number(object.tx_index);
    } else {
      message.tx_index = 0;
    }
    return message;
  },

  toJSON(message: AbsoluteTxPosition): unknown {
    const obj: any = {};
    message.block_height !== undefined &&
      (obj.block_height = message.block_height);
    message.tx_index !== undefined && (obj.tx_index = message.tx_index);
    return obj;
  },

  fromPartial(object: DeepPartial<AbsoluteTxPosition>): AbsoluteTxPosition {
    const message = { ...baseAbsoluteTxPosition } as AbsoluteTxPosition;
    if (object.block_height !== undefined && object.block_height !== null) {
      message.block_height = object.block_height;
    } else {
      message.block_height = 0;
    }
    if (object.tx_index !== undefined && object.tx_index !== null) {
      message.tx_index = object.tx_index;
    } else {
      message.tx_index = 0;
    }
    return message;
  },
};

const baseModel: object = {};

export const Model = {
  encode(message: Model, writer: Writer = Writer.create()): Writer {
    if (message.key.length !== 0) {
      writer.uint32(10).bytes(message.key);
    }
    if (message.value.length !== 0) {
      writer.uint32(18).bytes(message.value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): Model {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseModel } as Model;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.key = reader.bytes();
          break;
        case 2:
          message.value = reader.bytes();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): Model {
    const message = { ...baseModel } as Model;
    if (object.key !== undefined && object.key !== null) {
      message.key = bytesFromBase64(object.key);
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = bytesFromBase64(object.value);
    }
    return message;
  },

  toJSON(message: Model): unknown {
    const obj: any = {};
    message.key !== undefined &&
      (obj.key = base64FromBytes(
        message.key !== undefined ? message.key : new Uint8Array()
      ));
    message.value !== undefined &&
      (obj.value = base64FromBytes(
        message.value !== undefined ? message.value : new Uint8Array()
      ));
    return obj;
  },

  fromPartial(object: DeepPartial<Model>): Model {
    const message = { ...baseModel } as Model;
    if (object.key !== undefined && object.key !== null) {
      message.key = object.key;
    } else {
      message.key = new Uint8Array();
    }
    if (object.value !== undefined && object.value !== null) {
      message.value = object.value;
    } else {
      message.value = new Uint8Array();
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
