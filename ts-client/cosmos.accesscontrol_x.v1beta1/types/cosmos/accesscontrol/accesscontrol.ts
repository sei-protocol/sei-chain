/* eslint-disable */
import {
  AccessType,
  ResourceType,
  AccessOperationSelectorType,
  WasmMessageSubtype,
  accessTypeFromJSON,
  resourceTypeFromJSON,
  accessTypeToJSON,
  resourceTypeToJSON,
  accessOperationSelectorTypeFromJSON,
  accessOperationSelectorTypeToJSON,
  wasmMessageSubtypeFromJSON,
  wasmMessageSubtypeToJSON,
} from "../../cosmos/accesscontrol/constants";
import { Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "cosmos.accesscontrol.v1beta1";

export interface AccessOperation {
  accessType: AccessType;
  resourceType: ResourceType;
  identifierTemplate: string;
}

export interface WasmAccessOperation {
  operation: AccessOperation | undefined;
  selectorType: AccessOperationSelectorType;
  selector: string;
}

export interface WasmContractReference {
  contractAddress: string;
  messageType: WasmMessageSubtype;
  messageName: string;
  jsonTranslationTemplate: string;
}

export interface WasmContractReferences {
  messageName: string;
  contractReferences: WasmContractReference[];
}

export interface WasmAccessOperations {
  messageName: string;
  wasmOperations: WasmAccessOperation[];
}

export interface MessageDependencyMapping {
  messageKey: string;
  accessOps: AccessOperation[];
  dynamicEnabled: boolean;
}

export interface WasmDependencyMapping {
  baseAccessOps: WasmAccessOperation[];
  queryAccessOps: WasmAccessOperations[];
  executeAccessOps: WasmAccessOperations[];
  baseContractReferences: WasmContractReference[];
  queryContractReferences: WasmContractReferences[];
  executeContractReferences: WasmContractReferences[];
  resetReason: string;
  contractAddress: string;
}

const baseAccessOperation: object = {
  accessType: 0,
  resourceType: 0,
  identifierTemplate: "",
};

export const AccessOperation = {
  encode(message: AccessOperation, writer: Writer = Writer.create()): Writer {
    if (message.accessType !== 0) {
      writer.uint32(8).int32(message.accessType);
    }
    if (message.resourceType !== 0) {
      writer.uint32(16).int32(message.resourceType);
    }
    if (message.identifierTemplate !== "") {
      writer.uint32(26).string(message.identifierTemplate);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): AccessOperation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseAccessOperation } as AccessOperation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.accessType = reader.int32() as any;
          break;
        case 2:
          message.resourceType = reader.int32() as any;
          break;
        case 3:
          message.identifierTemplate = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): AccessOperation {
    const message = { ...baseAccessOperation } as AccessOperation;
    if (object.accessType !== undefined && object.accessType !== null) {
      message.accessType = accessTypeFromJSON(object.accessType);
    } else {
      message.accessType = 0;
    }
    if (object.resourceType !== undefined && object.resourceType !== null) {
      message.resourceType = resourceTypeFromJSON(object.resourceType);
    } else {
      message.resourceType = 0;
    }
    if (
      object.identifierTemplate !== undefined &&
      object.identifierTemplate !== null
    ) {
      message.identifierTemplate = String(object.identifierTemplate);
    } else {
      message.identifierTemplate = "";
    }
    return message;
  },

  toJSON(message: AccessOperation): unknown {
    const obj: any = {};
    message.accessType !== undefined &&
      (obj.accessType = accessTypeToJSON(message.accessType));
    message.resourceType !== undefined &&
      (obj.resourceType = resourceTypeToJSON(message.resourceType));
    message.identifierTemplate !== undefined &&
      (obj.identifierTemplate = message.identifierTemplate);
    return obj;
  },

  fromPartial(object: DeepPartial<AccessOperation>): AccessOperation {
    const message = { ...baseAccessOperation } as AccessOperation;
    if (object.accessType !== undefined && object.accessType !== null) {
      message.accessType = object.accessType;
    } else {
      message.accessType = 0;
    }
    if (object.resourceType !== undefined && object.resourceType !== null) {
      message.resourceType = object.resourceType;
    } else {
      message.resourceType = 0;
    }
    if (
      object.identifierTemplate !== undefined &&
      object.identifierTemplate !== null
    ) {
      message.identifierTemplate = object.identifierTemplate;
    } else {
      message.identifierTemplate = "";
    }
    return message;
  },
};

const baseWasmAccessOperation: object = { selectorType: 0, selector: "" };

export const WasmAccessOperation = {
  encode(
    message: WasmAccessOperation,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.operation !== undefined) {
      AccessOperation.encode(
        message.operation,
        writer.uint32(10).fork()
      ).ldelim();
    }
    if (message.selectorType !== 0) {
      writer.uint32(16).int32(message.selectorType);
    }
    if (message.selector !== "") {
      writer.uint32(26).string(message.selector);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): WasmAccessOperation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWasmAccessOperation } as WasmAccessOperation;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.operation = AccessOperation.decode(reader, reader.uint32());
          break;
        case 2:
          message.selectorType = reader.int32() as any;
          break;
        case 3:
          message.selector = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WasmAccessOperation {
    const message = { ...baseWasmAccessOperation } as WasmAccessOperation;
    if (object.operation !== undefined && object.operation !== null) {
      message.operation = AccessOperation.fromJSON(object.operation);
    } else {
      message.operation = undefined;
    }
    if (object.selectorType !== undefined && object.selectorType !== null) {
      message.selectorType = accessOperationSelectorTypeFromJSON(
        object.selectorType
      );
    } else {
      message.selectorType = 0;
    }
    if (object.selector !== undefined && object.selector !== null) {
      message.selector = String(object.selector);
    } else {
      message.selector = "";
    }
    return message;
  },

  toJSON(message: WasmAccessOperation): unknown {
    const obj: any = {};
    message.operation !== undefined &&
      (obj.operation = message.operation
        ? AccessOperation.toJSON(message.operation)
        : undefined);
    message.selectorType !== undefined &&
      (obj.selectorType = accessOperationSelectorTypeToJSON(
        message.selectorType
      ));
    message.selector !== undefined && (obj.selector = message.selector);
    return obj;
  },

  fromPartial(object: DeepPartial<WasmAccessOperation>): WasmAccessOperation {
    const message = { ...baseWasmAccessOperation } as WasmAccessOperation;
    if (object.operation !== undefined && object.operation !== null) {
      message.operation = AccessOperation.fromPartial(object.operation);
    } else {
      message.operation = undefined;
    }
    if (object.selectorType !== undefined && object.selectorType !== null) {
      message.selectorType = object.selectorType;
    } else {
      message.selectorType = 0;
    }
    if (object.selector !== undefined && object.selector !== null) {
      message.selector = object.selector;
    } else {
      message.selector = "";
    }
    return message;
  },
};

const baseWasmContractReference: object = {
  contractAddress: "",
  messageType: 0,
  messageName: "",
  jsonTranslationTemplate: "",
};

export const WasmContractReference = {
  encode(
    message: WasmContractReference,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.contractAddress !== "") {
      writer.uint32(10).string(message.contractAddress);
    }
    if (message.messageType !== 0) {
      writer.uint32(16).int32(message.messageType);
    }
    if (message.messageName !== "") {
      writer.uint32(26).string(message.messageName);
    }
    if (message.jsonTranslationTemplate !== "") {
      writer.uint32(34).string(message.jsonTranslationTemplate);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): WasmContractReference {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWasmContractReference } as WasmContractReference;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.contractAddress = reader.string();
          break;
        case 2:
          message.messageType = reader.int32() as any;
          break;
        case 3:
          message.messageName = reader.string();
          break;
        case 4:
          message.jsonTranslationTemplate = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WasmContractReference {
    const message = { ...baseWasmContractReference } as WasmContractReference;
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
    }
    if (object.messageType !== undefined && object.messageType !== null) {
      message.messageType = wasmMessageSubtypeFromJSON(object.messageType);
    } else {
      message.messageType = 0;
    }
    if (object.messageName !== undefined && object.messageName !== null) {
      message.messageName = String(object.messageName);
    } else {
      message.messageName = "";
    }
    if (
      object.jsonTranslationTemplate !== undefined &&
      object.jsonTranslationTemplate !== null
    ) {
      message.jsonTranslationTemplate = String(object.jsonTranslationTemplate);
    } else {
      message.jsonTranslationTemplate = "";
    }
    return message;
  },

  toJSON(message: WasmContractReference): unknown {
    const obj: any = {};
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    message.messageType !== undefined &&
      (obj.messageType = wasmMessageSubtypeToJSON(message.messageType));
    message.messageName !== undefined &&
      (obj.messageName = message.messageName);
    message.jsonTranslationTemplate !== undefined &&
      (obj.jsonTranslationTemplate = message.jsonTranslationTemplate);
    return obj;
  },

  fromPartial(
    object: DeepPartial<WasmContractReference>
  ): WasmContractReference {
    const message = { ...baseWasmContractReference } as WasmContractReference;
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
    }
    if (object.messageType !== undefined && object.messageType !== null) {
      message.messageType = object.messageType;
    } else {
      message.messageType = 0;
    }
    if (object.messageName !== undefined && object.messageName !== null) {
      message.messageName = object.messageName;
    } else {
      message.messageName = "";
    }
    if (
      object.jsonTranslationTemplate !== undefined &&
      object.jsonTranslationTemplate !== null
    ) {
      message.jsonTranslationTemplate = object.jsonTranslationTemplate;
    } else {
      message.jsonTranslationTemplate = "";
    }
    return message;
  },
};

const baseWasmContractReferences: object = { messageName: "" };

export const WasmContractReferences = {
  encode(
    message: WasmContractReferences,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.messageName !== "") {
      writer.uint32(10).string(message.messageName);
    }
    for (const v of message.contractReferences) {
      WasmContractReference.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): WasmContractReferences {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWasmContractReferences } as WasmContractReferences;
    message.contractReferences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageName = reader.string();
          break;
        case 2:
          message.contractReferences.push(
            WasmContractReference.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WasmContractReferences {
    const message = { ...baseWasmContractReferences } as WasmContractReferences;
    message.contractReferences = [];
    if (object.messageName !== undefined && object.messageName !== null) {
      message.messageName = String(object.messageName);
    } else {
      message.messageName = "";
    }
    if (
      object.contractReferences !== undefined &&
      object.contractReferences !== null
    ) {
      for (const e of object.contractReferences) {
        message.contractReferences.push(WasmContractReference.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: WasmContractReferences): unknown {
    const obj: any = {};
    message.messageName !== undefined &&
      (obj.messageName = message.messageName);
    if (message.contractReferences) {
      obj.contractReferences = message.contractReferences.map((e) =>
        e ? WasmContractReference.toJSON(e) : undefined
      );
    } else {
      obj.contractReferences = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<WasmContractReferences>
  ): WasmContractReferences {
    const message = { ...baseWasmContractReferences } as WasmContractReferences;
    message.contractReferences = [];
    if (object.messageName !== undefined && object.messageName !== null) {
      message.messageName = object.messageName;
    } else {
      message.messageName = "";
    }
    if (
      object.contractReferences !== undefined &&
      object.contractReferences !== null
    ) {
      for (const e of object.contractReferences) {
        message.contractReferences.push(WasmContractReference.fromPartial(e));
      }
    }
    return message;
  },
};

const baseWasmAccessOperations: object = { messageName: "" };

export const WasmAccessOperations = {
  encode(
    message: WasmAccessOperations,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.messageName !== "") {
      writer.uint32(10).string(message.messageName);
    }
    for (const v of message.wasmOperations) {
      WasmAccessOperation.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): WasmAccessOperations {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWasmAccessOperations } as WasmAccessOperations;
    message.wasmOperations = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageName = reader.string();
          break;
        case 2:
          message.wasmOperations.push(
            WasmAccessOperation.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WasmAccessOperations {
    const message = { ...baseWasmAccessOperations } as WasmAccessOperations;
    message.wasmOperations = [];
    if (object.messageName !== undefined && object.messageName !== null) {
      message.messageName = String(object.messageName);
    } else {
      message.messageName = "";
    }
    if (object.wasmOperations !== undefined && object.wasmOperations !== null) {
      for (const e of object.wasmOperations) {
        message.wasmOperations.push(WasmAccessOperation.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: WasmAccessOperations): unknown {
    const obj: any = {};
    message.messageName !== undefined &&
      (obj.messageName = message.messageName);
    if (message.wasmOperations) {
      obj.wasmOperations = message.wasmOperations.map((e) =>
        e ? WasmAccessOperation.toJSON(e) : undefined
      );
    } else {
      obj.wasmOperations = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<WasmAccessOperations>): WasmAccessOperations {
    const message = { ...baseWasmAccessOperations } as WasmAccessOperations;
    message.wasmOperations = [];
    if (object.messageName !== undefined && object.messageName !== null) {
      message.messageName = object.messageName;
    } else {
      message.messageName = "";
    }
    if (object.wasmOperations !== undefined && object.wasmOperations !== null) {
      for (const e of object.wasmOperations) {
        message.wasmOperations.push(WasmAccessOperation.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMessageDependencyMapping: object = {
  messageKey: "",
  dynamicEnabled: false,
};

export const MessageDependencyMapping = {
  encode(
    message: MessageDependencyMapping,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.messageKey !== "") {
      writer.uint32(10).string(message.messageKey);
    }
    for (const v of message.accessOps) {
      AccessOperation.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.dynamicEnabled === true) {
      writer.uint32(24).bool(message.dynamicEnabled);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): MessageDependencyMapping {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseMessageDependencyMapping,
    } as MessageDependencyMapping;
    message.accessOps = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.messageKey = reader.string();
          break;
        case 2:
          message.accessOps.push(
            AccessOperation.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.dynamicEnabled = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MessageDependencyMapping {
    const message = {
      ...baseMessageDependencyMapping,
    } as MessageDependencyMapping;
    message.accessOps = [];
    if (object.messageKey !== undefined && object.messageKey !== null) {
      message.messageKey = String(object.messageKey);
    } else {
      message.messageKey = "";
    }
    if (object.accessOps !== undefined && object.accessOps !== null) {
      for (const e of object.accessOps) {
        message.accessOps.push(AccessOperation.fromJSON(e));
      }
    }
    if (object.dynamicEnabled !== undefined && object.dynamicEnabled !== null) {
      message.dynamicEnabled = Boolean(object.dynamicEnabled);
    } else {
      message.dynamicEnabled = false;
    }
    return message;
  },

  toJSON(message: MessageDependencyMapping): unknown {
    const obj: any = {};
    message.messageKey !== undefined && (obj.messageKey = message.messageKey);
    if (message.accessOps) {
      obj.accessOps = message.accessOps.map((e) =>
        e ? AccessOperation.toJSON(e) : undefined
      );
    } else {
      obj.accessOps = [];
    }
    message.dynamicEnabled !== undefined &&
      (obj.dynamicEnabled = message.dynamicEnabled);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MessageDependencyMapping>
  ): MessageDependencyMapping {
    const message = {
      ...baseMessageDependencyMapping,
    } as MessageDependencyMapping;
    message.accessOps = [];
    if (object.messageKey !== undefined && object.messageKey !== null) {
      message.messageKey = object.messageKey;
    } else {
      message.messageKey = "";
    }
    if (object.accessOps !== undefined && object.accessOps !== null) {
      for (const e of object.accessOps) {
        message.accessOps.push(AccessOperation.fromPartial(e));
      }
    }
    if (object.dynamicEnabled !== undefined && object.dynamicEnabled !== null) {
      message.dynamicEnabled = object.dynamicEnabled;
    } else {
      message.dynamicEnabled = false;
    }
    return message;
  },
};

const baseWasmDependencyMapping: object = {
  resetReason: "",
  contractAddress: "",
};

export const WasmDependencyMapping = {
  encode(
    message: WasmDependencyMapping,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.baseAccessOps) {
      WasmAccessOperation.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    for (const v of message.queryAccessOps) {
      WasmAccessOperations.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.executeAccessOps) {
      WasmAccessOperations.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.baseContractReferences) {
      WasmContractReference.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.queryContractReferences) {
      WasmContractReferences.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    for (const v of message.executeContractReferences) {
      WasmContractReferences.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    if (message.resetReason !== "") {
      writer.uint32(58).string(message.resetReason);
    }
    if (message.contractAddress !== "") {
      writer.uint32(66).string(message.contractAddress);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): WasmDependencyMapping {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseWasmDependencyMapping } as WasmDependencyMapping;
    message.baseAccessOps = [];
    message.queryAccessOps = [];
    message.executeAccessOps = [];
    message.baseContractReferences = [];
    message.queryContractReferences = [];
    message.executeContractReferences = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.baseAccessOps.push(
            WasmAccessOperation.decode(reader, reader.uint32())
          );
          break;
        case 2:
          message.queryAccessOps.push(
            WasmAccessOperations.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.executeAccessOps.push(
            WasmAccessOperations.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.baseContractReferences.push(
            WasmContractReference.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.queryContractReferences.push(
            WasmContractReferences.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.executeContractReferences.push(
            WasmContractReferences.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.resetReason = reader.string();
          break;
        case 8:
          message.contractAddress = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): WasmDependencyMapping {
    const message = { ...baseWasmDependencyMapping } as WasmDependencyMapping;
    message.baseAccessOps = [];
    message.queryAccessOps = [];
    message.executeAccessOps = [];
    message.baseContractReferences = [];
    message.queryContractReferences = [];
    message.executeContractReferences = [];
    if (object.baseAccessOps !== undefined && object.baseAccessOps !== null) {
      for (const e of object.baseAccessOps) {
        message.baseAccessOps.push(WasmAccessOperation.fromJSON(e));
      }
    }
    if (object.queryAccessOps !== undefined && object.queryAccessOps !== null) {
      for (const e of object.queryAccessOps) {
        message.queryAccessOps.push(WasmAccessOperations.fromJSON(e));
      }
    }
    if (
      object.executeAccessOps !== undefined &&
      object.executeAccessOps !== null
    ) {
      for (const e of object.executeAccessOps) {
        message.executeAccessOps.push(WasmAccessOperations.fromJSON(e));
      }
    }
    if (
      object.baseContractReferences !== undefined &&
      object.baseContractReferences !== null
    ) {
      for (const e of object.baseContractReferences) {
        message.baseContractReferences.push(WasmContractReference.fromJSON(e));
      }
    }
    if (
      object.queryContractReferences !== undefined &&
      object.queryContractReferences !== null
    ) {
      for (const e of object.queryContractReferences) {
        message.queryContractReferences.push(
          WasmContractReferences.fromJSON(e)
        );
      }
    }
    if (
      object.executeContractReferences !== undefined &&
      object.executeContractReferences !== null
    ) {
      for (const e of object.executeContractReferences) {
        message.executeContractReferences.push(
          WasmContractReferences.fromJSON(e)
        );
      }
    }
    if (object.resetReason !== undefined && object.resetReason !== null) {
      message.resetReason = String(object.resetReason);
    } else {
      message.resetReason = "";
    }
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = String(object.contractAddress);
    } else {
      message.contractAddress = "";
    }
    return message;
  },

  toJSON(message: WasmDependencyMapping): unknown {
    const obj: any = {};
    if (message.baseAccessOps) {
      obj.baseAccessOps = message.baseAccessOps.map((e) =>
        e ? WasmAccessOperation.toJSON(e) : undefined
      );
    } else {
      obj.baseAccessOps = [];
    }
    if (message.queryAccessOps) {
      obj.queryAccessOps = message.queryAccessOps.map((e) =>
        e ? WasmAccessOperations.toJSON(e) : undefined
      );
    } else {
      obj.queryAccessOps = [];
    }
    if (message.executeAccessOps) {
      obj.executeAccessOps = message.executeAccessOps.map((e) =>
        e ? WasmAccessOperations.toJSON(e) : undefined
      );
    } else {
      obj.executeAccessOps = [];
    }
    if (message.baseContractReferences) {
      obj.baseContractReferences = message.baseContractReferences.map((e) =>
        e ? WasmContractReference.toJSON(e) : undefined
      );
    } else {
      obj.baseContractReferences = [];
    }
    if (message.queryContractReferences) {
      obj.queryContractReferences = message.queryContractReferences.map((e) =>
        e ? WasmContractReferences.toJSON(e) : undefined
      );
    } else {
      obj.queryContractReferences = [];
    }
    if (message.executeContractReferences) {
      obj.executeContractReferences = message.executeContractReferences.map(
        (e) => (e ? WasmContractReferences.toJSON(e) : undefined)
      );
    } else {
      obj.executeContractReferences = [];
    }
    message.resetReason !== undefined &&
      (obj.resetReason = message.resetReason);
    message.contractAddress !== undefined &&
      (obj.contractAddress = message.contractAddress);
    return obj;
  },

  fromPartial(
    object: DeepPartial<WasmDependencyMapping>
  ): WasmDependencyMapping {
    const message = { ...baseWasmDependencyMapping } as WasmDependencyMapping;
    message.baseAccessOps = [];
    message.queryAccessOps = [];
    message.executeAccessOps = [];
    message.baseContractReferences = [];
    message.queryContractReferences = [];
    message.executeContractReferences = [];
    if (object.baseAccessOps !== undefined && object.baseAccessOps !== null) {
      for (const e of object.baseAccessOps) {
        message.baseAccessOps.push(WasmAccessOperation.fromPartial(e));
      }
    }
    if (object.queryAccessOps !== undefined && object.queryAccessOps !== null) {
      for (const e of object.queryAccessOps) {
        message.queryAccessOps.push(WasmAccessOperations.fromPartial(e));
      }
    }
    if (
      object.executeAccessOps !== undefined &&
      object.executeAccessOps !== null
    ) {
      for (const e of object.executeAccessOps) {
        message.executeAccessOps.push(WasmAccessOperations.fromPartial(e));
      }
    }
    if (
      object.baseContractReferences !== undefined &&
      object.baseContractReferences !== null
    ) {
      for (const e of object.baseContractReferences) {
        message.baseContractReferences.push(
          WasmContractReference.fromPartial(e)
        );
      }
    }
    if (
      object.queryContractReferences !== undefined &&
      object.queryContractReferences !== null
    ) {
      for (const e of object.queryContractReferences) {
        message.queryContractReferences.push(
          WasmContractReferences.fromPartial(e)
        );
      }
    }
    if (
      object.executeContractReferences !== undefined &&
      object.executeContractReferences !== null
    ) {
      for (const e of object.executeContractReferences) {
        message.executeContractReferences.push(
          WasmContractReferences.fromPartial(e)
        );
      }
    }
    if (object.resetReason !== undefined && object.resetReason !== null) {
      message.resetReason = object.resetReason;
    } else {
      message.resetReason = "";
    }
    if (
      object.contractAddress !== undefined &&
      object.contractAddress !== null
    ) {
      message.contractAddress = object.contractAddress;
    } else {
      message.contractAddress = "";
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
