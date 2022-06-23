/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";
export const protobufPackage = "google.protobuf";
export var FieldDescriptorProto_Type;
(function (FieldDescriptorProto_Type) {
    /**
     * TYPE_DOUBLE - 0 is reserved for errors.
     * Order is weird for historical reasons.
     */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_DOUBLE"] = 1] = "TYPE_DOUBLE";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_FLOAT"] = 2] = "TYPE_FLOAT";
    /**
     * TYPE_INT64 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT64 if
     * negative values are likely.
     */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_INT64"] = 3] = "TYPE_INT64";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_UINT64"] = 4] = "TYPE_UINT64";
    /**
     * TYPE_INT32 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT32 if
     * negative values are likely.
     */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_INT32"] = 5] = "TYPE_INT32";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_FIXED64"] = 6] = "TYPE_FIXED64";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_FIXED32"] = 7] = "TYPE_FIXED32";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_BOOL"] = 8] = "TYPE_BOOL";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_STRING"] = 9] = "TYPE_STRING";
    /**
     * TYPE_GROUP - Tag-delimited aggregate.
     * Group type is deprecated and not supported in proto3. However, Proto3
     * implementations should still be able to parse the group wire format and
     * treat group fields as unknown fields.
     */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_GROUP"] = 10] = "TYPE_GROUP";
    /** TYPE_MESSAGE - Length-delimited aggregate. */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_MESSAGE"] = 11] = "TYPE_MESSAGE";
    /** TYPE_BYTES - New in version 2. */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_BYTES"] = 12] = "TYPE_BYTES";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_UINT32"] = 13] = "TYPE_UINT32";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_ENUM"] = 14] = "TYPE_ENUM";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_SFIXED32"] = 15] = "TYPE_SFIXED32";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_SFIXED64"] = 16] = "TYPE_SFIXED64";
    /** TYPE_SINT32 - Uses ZigZag encoding. */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_SINT32"] = 17] = "TYPE_SINT32";
    /** TYPE_SINT64 - Uses ZigZag encoding. */
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["TYPE_SINT64"] = 18] = "TYPE_SINT64";
    FieldDescriptorProto_Type[FieldDescriptorProto_Type["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldDescriptorProto_Type || (FieldDescriptorProto_Type = {}));
export function fieldDescriptorProto_TypeFromJSON(object) {
    switch (object) {
        case 1:
        case "TYPE_DOUBLE":
            return FieldDescriptorProto_Type.TYPE_DOUBLE;
        case 2:
        case "TYPE_FLOAT":
            return FieldDescriptorProto_Type.TYPE_FLOAT;
        case 3:
        case "TYPE_INT64":
            return FieldDescriptorProto_Type.TYPE_INT64;
        case 4:
        case "TYPE_UINT64":
            return FieldDescriptorProto_Type.TYPE_UINT64;
        case 5:
        case "TYPE_INT32":
            return FieldDescriptorProto_Type.TYPE_INT32;
        case 6:
        case "TYPE_FIXED64":
            return FieldDescriptorProto_Type.TYPE_FIXED64;
        case 7:
        case "TYPE_FIXED32":
            return FieldDescriptorProto_Type.TYPE_FIXED32;
        case 8:
        case "TYPE_BOOL":
            return FieldDescriptorProto_Type.TYPE_BOOL;
        case 9:
        case "TYPE_STRING":
            return FieldDescriptorProto_Type.TYPE_STRING;
        case 10:
        case "TYPE_GROUP":
            return FieldDescriptorProto_Type.TYPE_GROUP;
        case 11:
        case "TYPE_MESSAGE":
            return FieldDescriptorProto_Type.TYPE_MESSAGE;
        case 12:
        case "TYPE_BYTES":
            return FieldDescriptorProto_Type.TYPE_BYTES;
        case 13:
        case "TYPE_UINT32":
            return FieldDescriptorProto_Type.TYPE_UINT32;
        case 14:
        case "TYPE_ENUM":
            return FieldDescriptorProto_Type.TYPE_ENUM;
        case 15:
        case "TYPE_SFIXED32":
            return FieldDescriptorProto_Type.TYPE_SFIXED32;
        case 16:
        case "TYPE_SFIXED64":
            return FieldDescriptorProto_Type.TYPE_SFIXED64;
        case 17:
        case "TYPE_SINT32":
            return FieldDescriptorProto_Type.TYPE_SINT32;
        case 18:
        case "TYPE_SINT64":
            return FieldDescriptorProto_Type.TYPE_SINT64;
        case -1:
        case "UNRECOGNIZED":
        default:
            return FieldDescriptorProto_Type.UNRECOGNIZED;
    }
}
export function fieldDescriptorProto_TypeToJSON(object) {
    switch (object) {
        case FieldDescriptorProto_Type.TYPE_DOUBLE:
            return "TYPE_DOUBLE";
        case FieldDescriptorProto_Type.TYPE_FLOAT:
            return "TYPE_FLOAT";
        case FieldDescriptorProto_Type.TYPE_INT64:
            return "TYPE_INT64";
        case FieldDescriptorProto_Type.TYPE_UINT64:
            return "TYPE_UINT64";
        case FieldDescriptorProto_Type.TYPE_INT32:
            return "TYPE_INT32";
        case FieldDescriptorProto_Type.TYPE_FIXED64:
            return "TYPE_FIXED64";
        case FieldDescriptorProto_Type.TYPE_FIXED32:
            return "TYPE_FIXED32";
        case FieldDescriptorProto_Type.TYPE_BOOL:
            return "TYPE_BOOL";
        case FieldDescriptorProto_Type.TYPE_STRING:
            return "TYPE_STRING";
        case FieldDescriptorProto_Type.TYPE_GROUP:
            return "TYPE_GROUP";
        case FieldDescriptorProto_Type.TYPE_MESSAGE:
            return "TYPE_MESSAGE";
        case FieldDescriptorProto_Type.TYPE_BYTES:
            return "TYPE_BYTES";
        case FieldDescriptorProto_Type.TYPE_UINT32:
            return "TYPE_UINT32";
        case FieldDescriptorProto_Type.TYPE_ENUM:
            return "TYPE_ENUM";
        case FieldDescriptorProto_Type.TYPE_SFIXED32:
            return "TYPE_SFIXED32";
        case FieldDescriptorProto_Type.TYPE_SFIXED64:
            return "TYPE_SFIXED64";
        case FieldDescriptorProto_Type.TYPE_SINT32:
            return "TYPE_SINT32";
        case FieldDescriptorProto_Type.TYPE_SINT64:
            return "TYPE_SINT64";
        default:
            return "UNKNOWN";
    }
}
export var FieldDescriptorProto_Label;
(function (FieldDescriptorProto_Label) {
    /** LABEL_OPTIONAL - 0 is reserved for errors */
    FieldDescriptorProto_Label[FieldDescriptorProto_Label["LABEL_OPTIONAL"] = 1] = "LABEL_OPTIONAL";
    FieldDescriptorProto_Label[FieldDescriptorProto_Label["LABEL_REQUIRED"] = 2] = "LABEL_REQUIRED";
    FieldDescriptorProto_Label[FieldDescriptorProto_Label["LABEL_REPEATED"] = 3] = "LABEL_REPEATED";
    FieldDescriptorProto_Label[FieldDescriptorProto_Label["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldDescriptorProto_Label || (FieldDescriptorProto_Label = {}));
export function fieldDescriptorProto_LabelFromJSON(object) {
    switch (object) {
        case 1:
        case "LABEL_OPTIONAL":
            return FieldDescriptorProto_Label.LABEL_OPTIONAL;
        case 2:
        case "LABEL_REQUIRED":
            return FieldDescriptorProto_Label.LABEL_REQUIRED;
        case 3:
        case "LABEL_REPEATED":
            return FieldDescriptorProto_Label.LABEL_REPEATED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return FieldDescriptorProto_Label.UNRECOGNIZED;
    }
}
export function fieldDescriptorProto_LabelToJSON(object) {
    switch (object) {
        case FieldDescriptorProto_Label.LABEL_OPTIONAL:
            return "LABEL_OPTIONAL";
        case FieldDescriptorProto_Label.LABEL_REQUIRED:
            return "LABEL_REQUIRED";
        case FieldDescriptorProto_Label.LABEL_REPEATED:
            return "LABEL_REPEATED";
        default:
            return "UNKNOWN";
    }
}
/** Generated classes can be optimized for speed or code size. */
export var FileOptions_OptimizeMode;
(function (FileOptions_OptimizeMode) {
    /** SPEED - Generate complete code for parsing, serialization, */
    FileOptions_OptimizeMode[FileOptions_OptimizeMode["SPEED"] = 1] = "SPEED";
    /** CODE_SIZE - etc. */
    FileOptions_OptimizeMode[FileOptions_OptimizeMode["CODE_SIZE"] = 2] = "CODE_SIZE";
    /** LITE_RUNTIME - Generate code using MessageLite and the lite runtime. */
    FileOptions_OptimizeMode[FileOptions_OptimizeMode["LITE_RUNTIME"] = 3] = "LITE_RUNTIME";
    FileOptions_OptimizeMode[FileOptions_OptimizeMode["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FileOptions_OptimizeMode || (FileOptions_OptimizeMode = {}));
export function fileOptions_OptimizeModeFromJSON(object) {
    switch (object) {
        case 1:
        case "SPEED":
            return FileOptions_OptimizeMode.SPEED;
        case 2:
        case "CODE_SIZE":
            return FileOptions_OptimizeMode.CODE_SIZE;
        case 3:
        case "LITE_RUNTIME":
            return FileOptions_OptimizeMode.LITE_RUNTIME;
        case -1:
        case "UNRECOGNIZED":
        default:
            return FileOptions_OptimizeMode.UNRECOGNIZED;
    }
}
export function fileOptions_OptimizeModeToJSON(object) {
    switch (object) {
        case FileOptions_OptimizeMode.SPEED:
            return "SPEED";
        case FileOptions_OptimizeMode.CODE_SIZE:
            return "CODE_SIZE";
        case FileOptions_OptimizeMode.LITE_RUNTIME:
            return "LITE_RUNTIME";
        default:
            return "UNKNOWN";
    }
}
export var FieldOptions_CType;
(function (FieldOptions_CType) {
    /** STRING - Default mode. */
    FieldOptions_CType[FieldOptions_CType["STRING"] = 0] = "STRING";
    FieldOptions_CType[FieldOptions_CType["CORD"] = 1] = "CORD";
    FieldOptions_CType[FieldOptions_CType["STRING_PIECE"] = 2] = "STRING_PIECE";
    FieldOptions_CType[FieldOptions_CType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldOptions_CType || (FieldOptions_CType = {}));
export function fieldOptions_CTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "STRING":
            return FieldOptions_CType.STRING;
        case 1:
        case "CORD":
            return FieldOptions_CType.CORD;
        case 2:
        case "STRING_PIECE":
            return FieldOptions_CType.STRING_PIECE;
        case -1:
        case "UNRECOGNIZED":
        default:
            return FieldOptions_CType.UNRECOGNIZED;
    }
}
export function fieldOptions_CTypeToJSON(object) {
    switch (object) {
        case FieldOptions_CType.STRING:
            return "STRING";
        case FieldOptions_CType.CORD:
            return "CORD";
        case FieldOptions_CType.STRING_PIECE:
            return "STRING_PIECE";
        default:
            return "UNKNOWN";
    }
}
export var FieldOptions_JSType;
(function (FieldOptions_JSType) {
    /** JS_NORMAL - Use the default type. */
    FieldOptions_JSType[FieldOptions_JSType["JS_NORMAL"] = 0] = "JS_NORMAL";
    /** JS_STRING - Use JavaScript strings. */
    FieldOptions_JSType[FieldOptions_JSType["JS_STRING"] = 1] = "JS_STRING";
    /** JS_NUMBER - Use JavaScript numbers. */
    FieldOptions_JSType[FieldOptions_JSType["JS_NUMBER"] = 2] = "JS_NUMBER";
    FieldOptions_JSType[FieldOptions_JSType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(FieldOptions_JSType || (FieldOptions_JSType = {}));
export function fieldOptions_JSTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "JS_NORMAL":
            return FieldOptions_JSType.JS_NORMAL;
        case 1:
        case "JS_STRING":
            return FieldOptions_JSType.JS_STRING;
        case 2:
        case "JS_NUMBER":
            return FieldOptions_JSType.JS_NUMBER;
        case -1:
        case "UNRECOGNIZED":
        default:
            return FieldOptions_JSType.UNRECOGNIZED;
    }
}
export function fieldOptions_JSTypeToJSON(object) {
    switch (object) {
        case FieldOptions_JSType.JS_NORMAL:
            return "JS_NORMAL";
        case FieldOptions_JSType.JS_STRING:
            return "JS_STRING";
        case FieldOptions_JSType.JS_NUMBER:
            return "JS_NUMBER";
        default:
            return "UNKNOWN";
    }
}
/**
 * Is this method side-effect-free (or safe in HTTP parlance), or idempotent,
 * or neither? HTTP based RPC implementation may choose GET verb for safe
 * methods, and PUT verb for idempotent methods instead of the default POST.
 */
export var MethodOptions_IdempotencyLevel;
(function (MethodOptions_IdempotencyLevel) {
    MethodOptions_IdempotencyLevel[MethodOptions_IdempotencyLevel["IDEMPOTENCY_UNKNOWN"] = 0] = "IDEMPOTENCY_UNKNOWN";
    /** NO_SIDE_EFFECTS - implies idempotent */
    MethodOptions_IdempotencyLevel[MethodOptions_IdempotencyLevel["NO_SIDE_EFFECTS"] = 1] = "NO_SIDE_EFFECTS";
    /** IDEMPOTENT - idempotent, but may have side effects */
    MethodOptions_IdempotencyLevel[MethodOptions_IdempotencyLevel["IDEMPOTENT"] = 2] = "IDEMPOTENT";
    MethodOptions_IdempotencyLevel[MethodOptions_IdempotencyLevel["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(MethodOptions_IdempotencyLevel || (MethodOptions_IdempotencyLevel = {}));
export function methodOptions_IdempotencyLevelFromJSON(object) {
    switch (object) {
        case 0:
        case "IDEMPOTENCY_UNKNOWN":
            return MethodOptions_IdempotencyLevel.IDEMPOTENCY_UNKNOWN;
        case 1:
        case "NO_SIDE_EFFECTS":
            return MethodOptions_IdempotencyLevel.NO_SIDE_EFFECTS;
        case 2:
        case "IDEMPOTENT":
            return MethodOptions_IdempotencyLevel.IDEMPOTENT;
        case -1:
        case "UNRECOGNIZED":
        default:
            return MethodOptions_IdempotencyLevel.UNRECOGNIZED;
    }
}
export function methodOptions_IdempotencyLevelToJSON(object) {
    switch (object) {
        case MethodOptions_IdempotencyLevel.IDEMPOTENCY_UNKNOWN:
            return "IDEMPOTENCY_UNKNOWN";
        case MethodOptions_IdempotencyLevel.NO_SIDE_EFFECTS:
            return "NO_SIDE_EFFECTS";
        case MethodOptions_IdempotencyLevel.IDEMPOTENT:
            return "IDEMPOTENT";
        default:
            return "UNKNOWN";
    }
}
const baseFileDescriptorSet = {};
export const FileDescriptorSet = {
    encode(message, writer = Writer.create()) {
        for (const v of message.file) {
            FileDescriptorProto.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFileDescriptorSet };
        message.file = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.file.push(FileDescriptorProto.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseFileDescriptorSet };
        message.file = [];
        if (object.file !== undefined && object.file !== null) {
            for (const e of object.file) {
                message.file.push(FileDescriptorProto.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.file) {
            obj.file = message.file.map((e) => e ? FileDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.file = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFileDescriptorSet };
        message.file = [];
        if (object.file !== undefined && object.file !== null) {
            for (const e of object.file) {
                message.file.push(FileDescriptorProto.fromPartial(e));
            }
        }
        return message;
    },
};
const baseFileDescriptorProto = {
    name: "",
    package: "",
    dependency: "",
    publicDependency: 0,
    weakDependency: 0,
    syntax: "",
};
export const FileDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.package !== "") {
            writer.uint32(18).string(message.package);
        }
        for (const v of message.dependency) {
            writer.uint32(26).string(v);
        }
        writer.uint32(82).fork();
        for (const v of message.publicDependency) {
            writer.int32(v);
        }
        writer.ldelim();
        writer.uint32(90).fork();
        for (const v of message.weakDependency) {
            writer.int32(v);
        }
        writer.ldelim();
        for (const v of message.messageType) {
            DescriptorProto.encode(v, writer.uint32(34).fork()).ldelim();
        }
        for (const v of message.enumType) {
            EnumDescriptorProto.encode(v, writer.uint32(42).fork()).ldelim();
        }
        for (const v of message.service) {
            ServiceDescriptorProto.encode(v, writer.uint32(50).fork()).ldelim();
        }
        for (const v of message.extension) {
            FieldDescriptorProto.encode(v, writer.uint32(58).fork()).ldelim();
        }
        if (message.options !== undefined) {
            FileOptions.encode(message.options, writer.uint32(66).fork()).ldelim();
        }
        if (message.sourceCodeInfo !== undefined) {
            SourceCodeInfo.encode(message.sourceCodeInfo, writer.uint32(74).fork()).ldelim();
        }
        if (message.syntax !== "") {
            writer.uint32(98).string(message.syntax);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFileDescriptorProto };
        message.dependency = [];
        message.publicDependency = [];
        message.weakDependency = [];
        message.messageType = [];
        message.enumType = [];
        message.service = [];
        message.extension = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.package = reader.string();
                    break;
                case 3:
                    message.dependency.push(reader.string());
                    break;
                case 10:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.publicDependency.push(reader.int32());
                        }
                    }
                    else {
                        message.publicDependency.push(reader.int32());
                    }
                    break;
                case 11:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.weakDependency.push(reader.int32());
                        }
                    }
                    else {
                        message.weakDependency.push(reader.int32());
                    }
                    break;
                case 4:
                    message.messageType.push(DescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.enumType.push(EnumDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 6:
                    message.service.push(ServiceDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 7:
                    message.extension.push(FieldDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 8:
                    message.options = FileOptions.decode(reader, reader.uint32());
                    break;
                case 9:
                    message.sourceCodeInfo = SourceCodeInfo.decode(reader, reader.uint32());
                    break;
                case 12:
                    message.syntax = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseFileDescriptorProto };
        message.dependency = [];
        message.publicDependency = [];
        message.weakDependency = [];
        message.messageType = [];
        message.enumType = [];
        message.service = [];
        message.extension = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.package !== undefined && object.package !== null) {
            message.package = String(object.package);
        }
        else {
            message.package = "";
        }
        if (object.dependency !== undefined && object.dependency !== null) {
            for (const e of object.dependency) {
                message.dependency.push(String(e));
            }
        }
        if (object.publicDependency !== undefined &&
            object.publicDependency !== null) {
            for (const e of object.publicDependency) {
                message.publicDependency.push(Number(e));
            }
        }
        if (object.weakDependency !== undefined && object.weakDependency !== null) {
            for (const e of object.weakDependency) {
                message.weakDependency.push(Number(e));
            }
        }
        if (object.messageType !== undefined && object.messageType !== null) {
            for (const e of object.messageType) {
                message.messageType.push(DescriptorProto.fromJSON(e));
            }
        }
        if (object.enumType !== undefined && object.enumType !== null) {
            for (const e of object.enumType) {
                message.enumType.push(EnumDescriptorProto.fromJSON(e));
            }
        }
        if (object.service !== undefined && object.service !== null) {
            for (const e of object.service) {
                message.service.push(ServiceDescriptorProto.fromJSON(e));
            }
        }
        if (object.extension !== undefined && object.extension !== null) {
            for (const e of object.extension) {
                message.extension.push(FieldDescriptorProto.fromJSON(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = FileOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.sourceCodeInfo !== undefined && object.sourceCodeInfo !== null) {
            message.sourceCodeInfo = SourceCodeInfo.fromJSON(object.sourceCodeInfo);
        }
        else {
            message.sourceCodeInfo = undefined;
        }
        if (object.syntax !== undefined && object.syntax !== null) {
            message.syntax = String(object.syntax);
        }
        else {
            message.syntax = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        message.package !== undefined && (obj.package = message.package);
        if (message.dependency) {
            obj.dependency = message.dependency.map((e) => e);
        }
        else {
            obj.dependency = [];
        }
        if (message.publicDependency) {
            obj.publicDependency = message.publicDependency.map((e) => e);
        }
        else {
            obj.publicDependency = [];
        }
        if (message.weakDependency) {
            obj.weakDependency = message.weakDependency.map((e) => e);
        }
        else {
            obj.weakDependency = [];
        }
        if (message.messageType) {
            obj.messageType = message.messageType.map((e) => e ? DescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.messageType = [];
        }
        if (message.enumType) {
            obj.enumType = message.enumType.map((e) => e ? EnumDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.enumType = [];
        }
        if (message.service) {
            obj.service = message.service.map((e) => e ? ServiceDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.service = [];
        }
        if (message.extension) {
            obj.extension = message.extension.map((e) => e ? FieldDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.extension = [];
        }
        message.options !== undefined &&
            (obj.options = message.options
                ? FileOptions.toJSON(message.options)
                : undefined);
        message.sourceCodeInfo !== undefined &&
            (obj.sourceCodeInfo = message.sourceCodeInfo
                ? SourceCodeInfo.toJSON(message.sourceCodeInfo)
                : undefined);
        message.syntax !== undefined && (obj.syntax = message.syntax);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFileDescriptorProto };
        message.dependency = [];
        message.publicDependency = [];
        message.weakDependency = [];
        message.messageType = [];
        message.enumType = [];
        message.service = [];
        message.extension = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.package !== undefined && object.package !== null) {
            message.package = object.package;
        }
        else {
            message.package = "";
        }
        if (object.dependency !== undefined && object.dependency !== null) {
            for (const e of object.dependency) {
                message.dependency.push(e);
            }
        }
        if (object.publicDependency !== undefined &&
            object.publicDependency !== null) {
            for (const e of object.publicDependency) {
                message.publicDependency.push(e);
            }
        }
        if (object.weakDependency !== undefined && object.weakDependency !== null) {
            for (const e of object.weakDependency) {
                message.weakDependency.push(e);
            }
        }
        if (object.messageType !== undefined && object.messageType !== null) {
            for (const e of object.messageType) {
                message.messageType.push(DescriptorProto.fromPartial(e));
            }
        }
        if (object.enumType !== undefined && object.enumType !== null) {
            for (const e of object.enumType) {
                message.enumType.push(EnumDescriptorProto.fromPartial(e));
            }
        }
        if (object.service !== undefined && object.service !== null) {
            for (const e of object.service) {
                message.service.push(ServiceDescriptorProto.fromPartial(e));
            }
        }
        if (object.extension !== undefined && object.extension !== null) {
            for (const e of object.extension) {
                message.extension.push(FieldDescriptorProto.fromPartial(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = FileOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.sourceCodeInfo !== undefined && object.sourceCodeInfo !== null) {
            message.sourceCodeInfo = SourceCodeInfo.fromPartial(object.sourceCodeInfo);
        }
        else {
            message.sourceCodeInfo = undefined;
        }
        if (object.syntax !== undefined && object.syntax !== null) {
            message.syntax = object.syntax;
        }
        else {
            message.syntax = "";
        }
        return message;
    },
};
const baseDescriptorProto = { name: "", reservedName: "" };
export const DescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        for (const v of message.field) {
            FieldDescriptorProto.encode(v, writer.uint32(18).fork()).ldelim();
        }
        for (const v of message.extension) {
            FieldDescriptorProto.encode(v, writer.uint32(50).fork()).ldelim();
        }
        for (const v of message.nestedType) {
            DescriptorProto.encode(v, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.enumType) {
            EnumDescriptorProto.encode(v, writer.uint32(34).fork()).ldelim();
        }
        for (const v of message.extensionRange) {
            DescriptorProto_ExtensionRange.encode(v, writer.uint32(42).fork()).ldelim();
        }
        for (const v of message.oneofDecl) {
            OneofDescriptorProto.encode(v, writer.uint32(66).fork()).ldelim();
        }
        if (message.options !== undefined) {
            MessageOptions.encode(message.options, writer.uint32(58).fork()).ldelim();
        }
        for (const v of message.reservedRange) {
            DescriptorProto_ReservedRange.encode(v, writer.uint32(74).fork()).ldelim();
        }
        for (const v of message.reservedName) {
            writer.uint32(82).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseDescriptorProto };
        message.field = [];
        message.extension = [];
        message.nestedType = [];
        message.enumType = [];
        message.extensionRange = [];
        message.oneofDecl = [];
        message.reservedRange = [];
        message.reservedName = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.field.push(FieldDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 6:
                    message.extension.push(FieldDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.nestedType.push(DescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 4:
                    message.enumType.push(EnumDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.extensionRange.push(DescriptorProto_ExtensionRange.decode(reader, reader.uint32()));
                    break;
                case 8:
                    message.oneofDecl.push(OneofDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 7:
                    message.options = MessageOptions.decode(reader, reader.uint32());
                    break;
                case 9:
                    message.reservedRange.push(DescriptorProto_ReservedRange.decode(reader, reader.uint32()));
                    break;
                case 10:
                    message.reservedName.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseDescriptorProto };
        message.field = [];
        message.extension = [];
        message.nestedType = [];
        message.enumType = [];
        message.extensionRange = [];
        message.oneofDecl = [];
        message.reservedRange = [];
        message.reservedName = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.field !== undefined && object.field !== null) {
            for (const e of object.field) {
                message.field.push(FieldDescriptorProto.fromJSON(e));
            }
        }
        if (object.extension !== undefined && object.extension !== null) {
            for (const e of object.extension) {
                message.extension.push(FieldDescriptorProto.fromJSON(e));
            }
        }
        if (object.nestedType !== undefined && object.nestedType !== null) {
            for (const e of object.nestedType) {
                message.nestedType.push(DescriptorProto.fromJSON(e));
            }
        }
        if (object.enumType !== undefined && object.enumType !== null) {
            for (const e of object.enumType) {
                message.enumType.push(EnumDescriptorProto.fromJSON(e));
            }
        }
        if (object.extensionRange !== undefined && object.extensionRange !== null) {
            for (const e of object.extensionRange) {
                message.extensionRange.push(DescriptorProto_ExtensionRange.fromJSON(e));
            }
        }
        if (object.oneofDecl !== undefined && object.oneofDecl !== null) {
            for (const e of object.oneofDecl) {
                message.oneofDecl.push(OneofDescriptorProto.fromJSON(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = MessageOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.reservedRange !== undefined && object.reservedRange !== null) {
            for (const e of object.reservedRange) {
                message.reservedRange.push(DescriptorProto_ReservedRange.fromJSON(e));
            }
        }
        if (object.reservedName !== undefined && object.reservedName !== null) {
            for (const e of object.reservedName) {
                message.reservedName.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        if (message.field) {
            obj.field = message.field.map((e) => e ? FieldDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.field = [];
        }
        if (message.extension) {
            obj.extension = message.extension.map((e) => e ? FieldDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.extension = [];
        }
        if (message.nestedType) {
            obj.nestedType = message.nestedType.map((e) => e ? DescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.nestedType = [];
        }
        if (message.enumType) {
            obj.enumType = message.enumType.map((e) => e ? EnumDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.enumType = [];
        }
        if (message.extensionRange) {
            obj.extensionRange = message.extensionRange.map((e) => e ? DescriptorProto_ExtensionRange.toJSON(e) : undefined);
        }
        else {
            obj.extensionRange = [];
        }
        if (message.oneofDecl) {
            obj.oneofDecl = message.oneofDecl.map((e) => e ? OneofDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.oneofDecl = [];
        }
        message.options !== undefined &&
            (obj.options = message.options
                ? MessageOptions.toJSON(message.options)
                : undefined);
        if (message.reservedRange) {
            obj.reservedRange = message.reservedRange.map((e) => e ? DescriptorProto_ReservedRange.toJSON(e) : undefined);
        }
        else {
            obj.reservedRange = [];
        }
        if (message.reservedName) {
            obj.reservedName = message.reservedName.map((e) => e);
        }
        else {
            obj.reservedName = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseDescriptorProto };
        message.field = [];
        message.extension = [];
        message.nestedType = [];
        message.enumType = [];
        message.extensionRange = [];
        message.oneofDecl = [];
        message.reservedRange = [];
        message.reservedName = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.field !== undefined && object.field !== null) {
            for (const e of object.field) {
                message.field.push(FieldDescriptorProto.fromPartial(e));
            }
        }
        if (object.extension !== undefined && object.extension !== null) {
            for (const e of object.extension) {
                message.extension.push(FieldDescriptorProto.fromPartial(e));
            }
        }
        if (object.nestedType !== undefined && object.nestedType !== null) {
            for (const e of object.nestedType) {
                message.nestedType.push(DescriptorProto.fromPartial(e));
            }
        }
        if (object.enumType !== undefined && object.enumType !== null) {
            for (const e of object.enumType) {
                message.enumType.push(EnumDescriptorProto.fromPartial(e));
            }
        }
        if (object.extensionRange !== undefined && object.extensionRange !== null) {
            for (const e of object.extensionRange) {
                message.extensionRange.push(DescriptorProto_ExtensionRange.fromPartial(e));
            }
        }
        if (object.oneofDecl !== undefined && object.oneofDecl !== null) {
            for (const e of object.oneofDecl) {
                message.oneofDecl.push(OneofDescriptorProto.fromPartial(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = MessageOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.reservedRange !== undefined && object.reservedRange !== null) {
            for (const e of object.reservedRange) {
                message.reservedRange.push(DescriptorProto_ReservedRange.fromPartial(e));
            }
        }
        if (object.reservedName !== undefined && object.reservedName !== null) {
            for (const e of object.reservedName) {
                message.reservedName.push(e);
            }
        }
        return message;
    },
};
const baseDescriptorProto_ExtensionRange = { start: 0, end: 0 };
export const DescriptorProto_ExtensionRange = {
    encode(message, writer = Writer.create()) {
        if (message.start !== 0) {
            writer.uint32(8).int32(message.start);
        }
        if (message.end !== 0) {
            writer.uint32(16).int32(message.end);
        }
        if (message.options !== undefined) {
            ExtensionRangeOptions.encode(message.options, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseDescriptorProto_ExtensionRange,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.start = reader.int32();
                    break;
                case 2:
                    message.end = reader.int32();
                    break;
                case 3:
                    message.options = ExtensionRangeOptions.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseDescriptorProto_ExtensionRange,
        };
        if (object.start !== undefined && object.start !== null) {
            message.start = Number(object.start);
        }
        else {
            message.start = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = Number(object.end);
        }
        else {
            message.end = 0;
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = ExtensionRangeOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.start !== undefined && (obj.start = message.start);
        message.end !== undefined && (obj.end = message.end);
        message.options !== undefined &&
            (obj.options = message.options
                ? ExtensionRangeOptions.toJSON(message.options)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseDescriptorProto_ExtensionRange,
        };
        if (object.start !== undefined && object.start !== null) {
            message.start = object.start;
        }
        else {
            message.start = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = object.end;
        }
        else {
            message.end = 0;
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = ExtensionRangeOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
};
const baseDescriptorProto_ReservedRange = { start: 0, end: 0 };
export const DescriptorProto_ReservedRange = {
    encode(message, writer = Writer.create()) {
        if (message.start !== 0) {
            writer.uint32(8).int32(message.start);
        }
        if (message.end !== 0) {
            writer.uint32(16).int32(message.end);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseDescriptorProto_ReservedRange,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.start = reader.int32();
                    break;
                case 2:
                    message.end = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseDescriptorProto_ReservedRange,
        };
        if (object.start !== undefined && object.start !== null) {
            message.start = Number(object.start);
        }
        else {
            message.start = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = Number(object.end);
        }
        else {
            message.end = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.start !== undefined && (obj.start = message.start);
        message.end !== undefined && (obj.end = message.end);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseDescriptorProto_ReservedRange,
        };
        if (object.start !== undefined && object.start !== null) {
            message.start = object.start;
        }
        else {
            message.start = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = object.end;
        }
        else {
            message.end = 0;
        }
        return message;
    },
};
const baseExtensionRangeOptions = {};
export const ExtensionRangeOptions = {
    encode(message, writer = Writer.create()) {
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseExtensionRangeOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseExtensionRangeOptions };
        message.uninterpretedOption = [];
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseExtensionRangeOptions };
        message.uninterpretedOption = [];
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseFieldDescriptorProto = {
    name: "",
    number: 0,
    label: 1,
    type: 1,
    typeName: "",
    extendee: "",
    defaultValue: "",
    oneofIndex: 0,
    jsonName: "",
    proto3Optional: false,
};
export const FieldDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.number !== 0) {
            writer.uint32(24).int32(message.number);
        }
        if (message.label !== 1) {
            writer.uint32(32).int32(message.label);
        }
        if (message.type !== 1) {
            writer.uint32(40).int32(message.type);
        }
        if (message.typeName !== "") {
            writer.uint32(50).string(message.typeName);
        }
        if (message.extendee !== "") {
            writer.uint32(18).string(message.extendee);
        }
        if (message.defaultValue !== "") {
            writer.uint32(58).string(message.defaultValue);
        }
        if (message.oneofIndex !== 0) {
            writer.uint32(72).int32(message.oneofIndex);
        }
        if (message.jsonName !== "") {
            writer.uint32(82).string(message.jsonName);
        }
        if (message.options !== undefined) {
            FieldOptions.encode(message.options, writer.uint32(66).fork()).ldelim();
        }
        if (message.proto3Optional === true) {
            writer.uint32(136).bool(message.proto3Optional);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFieldDescriptorProto };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 3:
                    message.number = reader.int32();
                    break;
                case 4:
                    message.label = reader.int32();
                    break;
                case 5:
                    message.type = reader.int32();
                    break;
                case 6:
                    message.typeName = reader.string();
                    break;
                case 2:
                    message.extendee = reader.string();
                    break;
                case 7:
                    message.defaultValue = reader.string();
                    break;
                case 9:
                    message.oneofIndex = reader.int32();
                    break;
                case 10:
                    message.jsonName = reader.string();
                    break;
                case 8:
                    message.options = FieldOptions.decode(reader, reader.uint32());
                    break;
                case 17:
                    message.proto3Optional = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseFieldDescriptorProto };
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.number !== undefined && object.number !== null) {
            message.number = Number(object.number);
        }
        else {
            message.number = 0;
        }
        if (object.label !== undefined && object.label !== null) {
            message.label = fieldDescriptorProto_LabelFromJSON(object.label);
        }
        else {
            message.label = 1;
        }
        if (object.type !== undefined && object.type !== null) {
            message.type = fieldDescriptorProto_TypeFromJSON(object.type);
        }
        else {
            message.type = 1;
        }
        if (object.typeName !== undefined && object.typeName !== null) {
            message.typeName = String(object.typeName);
        }
        else {
            message.typeName = "";
        }
        if (object.extendee !== undefined && object.extendee !== null) {
            message.extendee = String(object.extendee);
        }
        else {
            message.extendee = "";
        }
        if (object.defaultValue !== undefined && object.defaultValue !== null) {
            message.defaultValue = String(object.defaultValue);
        }
        else {
            message.defaultValue = "";
        }
        if (object.oneofIndex !== undefined && object.oneofIndex !== null) {
            message.oneofIndex = Number(object.oneofIndex);
        }
        else {
            message.oneofIndex = 0;
        }
        if (object.jsonName !== undefined && object.jsonName !== null) {
            message.jsonName = String(object.jsonName);
        }
        else {
            message.jsonName = "";
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = FieldOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.proto3Optional !== undefined && object.proto3Optional !== null) {
            message.proto3Optional = Boolean(object.proto3Optional);
        }
        else {
            message.proto3Optional = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        message.number !== undefined && (obj.number = message.number);
        message.label !== undefined &&
            (obj.label = fieldDescriptorProto_LabelToJSON(message.label));
        message.type !== undefined &&
            (obj.type = fieldDescriptorProto_TypeToJSON(message.type));
        message.typeName !== undefined && (obj.typeName = message.typeName);
        message.extendee !== undefined && (obj.extendee = message.extendee);
        message.defaultValue !== undefined &&
            (obj.defaultValue = message.defaultValue);
        message.oneofIndex !== undefined && (obj.oneofIndex = message.oneofIndex);
        message.jsonName !== undefined && (obj.jsonName = message.jsonName);
        message.options !== undefined &&
            (obj.options = message.options
                ? FieldOptions.toJSON(message.options)
                : undefined);
        message.proto3Optional !== undefined &&
            (obj.proto3Optional = message.proto3Optional);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFieldDescriptorProto };
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.number !== undefined && object.number !== null) {
            message.number = object.number;
        }
        else {
            message.number = 0;
        }
        if (object.label !== undefined && object.label !== null) {
            message.label = object.label;
        }
        else {
            message.label = 1;
        }
        if (object.type !== undefined && object.type !== null) {
            message.type = object.type;
        }
        else {
            message.type = 1;
        }
        if (object.typeName !== undefined && object.typeName !== null) {
            message.typeName = object.typeName;
        }
        else {
            message.typeName = "";
        }
        if (object.extendee !== undefined && object.extendee !== null) {
            message.extendee = object.extendee;
        }
        else {
            message.extendee = "";
        }
        if (object.defaultValue !== undefined && object.defaultValue !== null) {
            message.defaultValue = object.defaultValue;
        }
        else {
            message.defaultValue = "";
        }
        if (object.oneofIndex !== undefined && object.oneofIndex !== null) {
            message.oneofIndex = object.oneofIndex;
        }
        else {
            message.oneofIndex = 0;
        }
        if (object.jsonName !== undefined && object.jsonName !== null) {
            message.jsonName = object.jsonName;
        }
        else {
            message.jsonName = "";
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = FieldOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.proto3Optional !== undefined && object.proto3Optional !== null) {
            message.proto3Optional = object.proto3Optional;
        }
        else {
            message.proto3Optional = false;
        }
        return message;
    },
};
const baseOneofDescriptorProto = { name: "" };
export const OneofDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.options !== undefined) {
            OneofOptions.encode(message.options, writer.uint32(18).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseOneofDescriptorProto };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.options = OneofOptions.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseOneofDescriptorProto };
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = OneofOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        message.options !== undefined &&
            (obj.options = message.options
                ? OneofOptions.toJSON(message.options)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseOneofDescriptorProto };
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = OneofOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
};
const baseEnumDescriptorProto = { name: "", reservedName: "" };
export const EnumDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        for (const v of message.value) {
            EnumValueDescriptorProto.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.options !== undefined) {
            EnumOptions.encode(message.options, writer.uint32(26).fork()).ldelim();
        }
        for (const v of message.reservedRange) {
            EnumDescriptorProto_EnumReservedRange.encode(v, writer.uint32(34).fork()).ldelim();
        }
        for (const v of message.reservedName) {
            writer.uint32(42).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEnumDescriptorProto };
        message.value = [];
        message.reservedRange = [];
        message.reservedName = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.value.push(EnumValueDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.options = EnumOptions.decode(reader, reader.uint32());
                    break;
                case 4:
                    message.reservedRange.push(EnumDescriptorProto_EnumReservedRange.decode(reader, reader.uint32()));
                    break;
                case 5:
                    message.reservedName.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEnumDescriptorProto };
        message.value = [];
        message.reservedRange = [];
        message.reservedName = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.value !== undefined && object.value !== null) {
            for (const e of object.value) {
                message.value.push(EnumValueDescriptorProto.fromJSON(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = EnumOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.reservedRange !== undefined && object.reservedRange !== null) {
            for (const e of object.reservedRange) {
                message.reservedRange.push(EnumDescriptorProto_EnumReservedRange.fromJSON(e));
            }
        }
        if (object.reservedName !== undefined && object.reservedName !== null) {
            for (const e of object.reservedName) {
                message.reservedName.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        if (message.value) {
            obj.value = message.value.map((e) => e ? EnumValueDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.value = [];
        }
        message.options !== undefined &&
            (obj.options = message.options
                ? EnumOptions.toJSON(message.options)
                : undefined);
        if (message.reservedRange) {
            obj.reservedRange = message.reservedRange.map((e) => e ? EnumDescriptorProto_EnumReservedRange.toJSON(e) : undefined);
        }
        else {
            obj.reservedRange = [];
        }
        if (message.reservedName) {
            obj.reservedName = message.reservedName.map((e) => e);
        }
        else {
            obj.reservedName = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEnumDescriptorProto };
        message.value = [];
        message.reservedRange = [];
        message.reservedName = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.value !== undefined && object.value !== null) {
            for (const e of object.value) {
                message.value.push(EnumValueDescriptorProto.fromPartial(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = EnumOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.reservedRange !== undefined && object.reservedRange !== null) {
            for (const e of object.reservedRange) {
                message.reservedRange.push(EnumDescriptorProto_EnumReservedRange.fromPartial(e));
            }
        }
        if (object.reservedName !== undefined && object.reservedName !== null) {
            for (const e of object.reservedName) {
                message.reservedName.push(e);
            }
        }
        return message;
    },
};
const baseEnumDescriptorProto_EnumReservedRange = { start: 0, end: 0 };
export const EnumDescriptorProto_EnumReservedRange = {
    encode(message, writer = Writer.create()) {
        if (message.start !== 0) {
            writer.uint32(8).int32(message.start);
        }
        if (message.end !== 0) {
            writer.uint32(16).int32(message.end);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseEnumDescriptorProto_EnumReservedRange,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.start = reader.int32();
                    break;
                case 2:
                    message.end = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseEnumDescriptorProto_EnumReservedRange,
        };
        if (object.start !== undefined && object.start !== null) {
            message.start = Number(object.start);
        }
        else {
            message.start = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = Number(object.end);
        }
        else {
            message.end = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.start !== undefined && (obj.start = message.start);
        message.end !== undefined && (obj.end = message.end);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseEnumDescriptorProto_EnumReservedRange,
        };
        if (object.start !== undefined && object.start !== null) {
            message.start = object.start;
        }
        else {
            message.start = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = object.end;
        }
        else {
            message.end = 0;
        }
        return message;
    },
};
const baseEnumValueDescriptorProto = { name: "", number: 0 };
export const EnumValueDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.number !== 0) {
            writer.uint32(16).int32(message.number);
        }
        if (message.options !== undefined) {
            EnumValueOptions.encode(message.options, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseEnumValueDescriptorProto,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.number = reader.int32();
                    break;
                case 3:
                    message.options = EnumValueOptions.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseEnumValueDescriptorProto,
        };
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.number !== undefined && object.number !== null) {
            message.number = Number(object.number);
        }
        else {
            message.number = 0;
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = EnumValueOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        message.number !== undefined && (obj.number = message.number);
        message.options !== undefined &&
            (obj.options = message.options
                ? EnumValueOptions.toJSON(message.options)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseEnumValueDescriptorProto,
        };
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.number !== undefined && object.number !== null) {
            message.number = object.number;
        }
        else {
            message.number = 0;
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = EnumValueOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
};
const baseServiceDescriptorProto = { name: "" };
export const ServiceDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        for (const v of message.method) {
            MethodDescriptorProto.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.options !== undefined) {
            ServiceOptions.encode(message.options, writer.uint32(26).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseServiceDescriptorProto };
        message.method = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.method.push(MethodDescriptorProto.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.options = ServiceOptions.decode(reader, reader.uint32());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseServiceDescriptorProto };
        message.method = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.method !== undefined && object.method !== null) {
            for (const e of object.method) {
                message.method.push(MethodDescriptorProto.fromJSON(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = ServiceOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        if (message.method) {
            obj.method = message.method.map((e) => e ? MethodDescriptorProto.toJSON(e) : undefined);
        }
        else {
            obj.method = [];
        }
        message.options !== undefined &&
            (obj.options = message.options
                ? ServiceOptions.toJSON(message.options)
                : undefined);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseServiceDescriptorProto };
        message.method = [];
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.method !== undefined && object.method !== null) {
            for (const e of object.method) {
                message.method.push(MethodDescriptorProto.fromPartial(e));
            }
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = ServiceOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        return message;
    },
};
const baseMethodDescriptorProto = {
    name: "",
    inputType: "",
    outputType: "",
    clientStreaming: false,
    serverStreaming: false,
};
export const MethodDescriptorProto = {
    encode(message, writer = Writer.create()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.inputType !== "") {
            writer.uint32(18).string(message.inputType);
        }
        if (message.outputType !== "") {
            writer.uint32(26).string(message.outputType);
        }
        if (message.options !== undefined) {
            MethodOptions.encode(message.options, writer.uint32(34).fork()).ldelim();
        }
        if (message.clientStreaming === true) {
            writer.uint32(40).bool(message.clientStreaming);
        }
        if (message.serverStreaming === true) {
            writer.uint32(48).bool(message.serverStreaming);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMethodDescriptorProto };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.name = reader.string();
                    break;
                case 2:
                    message.inputType = reader.string();
                    break;
                case 3:
                    message.outputType = reader.string();
                    break;
                case 4:
                    message.options = MethodOptions.decode(reader, reader.uint32());
                    break;
                case 5:
                    message.clientStreaming = reader.bool();
                    break;
                case 6:
                    message.serverStreaming = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMethodDescriptorProto };
        if (object.name !== undefined && object.name !== null) {
            message.name = String(object.name);
        }
        else {
            message.name = "";
        }
        if (object.inputType !== undefined && object.inputType !== null) {
            message.inputType = String(object.inputType);
        }
        else {
            message.inputType = "";
        }
        if (object.outputType !== undefined && object.outputType !== null) {
            message.outputType = String(object.outputType);
        }
        else {
            message.outputType = "";
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = MethodOptions.fromJSON(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.clientStreaming !== undefined &&
            object.clientStreaming !== null) {
            message.clientStreaming = Boolean(object.clientStreaming);
        }
        else {
            message.clientStreaming = false;
        }
        if (object.serverStreaming !== undefined &&
            object.serverStreaming !== null) {
            message.serverStreaming = Boolean(object.serverStreaming);
        }
        else {
            message.serverStreaming = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.name !== undefined && (obj.name = message.name);
        message.inputType !== undefined && (obj.inputType = message.inputType);
        message.outputType !== undefined && (obj.outputType = message.outputType);
        message.options !== undefined &&
            (obj.options = message.options
                ? MethodOptions.toJSON(message.options)
                : undefined);
        message.clientStreaming !== undefined &&
            (obj.clientStreaming = message.clientStreaming);
        message.serverStreaming !== undefined &&
            (obj.serverStreaming = message.serverStreaming);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMethodDescriptorProto };
        if (object.name !== undefined && object.name !== null) {
            message.name = object.name;
        }
        else {
            message.name = "";
        }
        if (object.inputType !== undefined && object.inputType !== null) {
            message.inputType = object.inputType;
        }
        else {
            message.inputType = "";
        }
        if (object.outputType !== undefined && object.outputType !== null) {
            message.outputType = object.outputType;
        }
        else {
            message.outputType = "";
        }
        if (object.options !== undefined && object.options !== null) {
            message.options = MethodOptions.fromPartial(object.options);
        }
        else {
            message.options = undefined;
        }
        if (object.clientStreaming !== undefined &&
            object.clientStreaming !== null) {
            message.clientStreaming = object.clientStreaming;
        }
        else {
            message.clientStreaming = false;
        }
        if (object.serverStreaming !== undefined &&
            object.serverStreaming !== null) {
            message.serverStreaming = object.serverStreaming;
        }
        else {
            message.serverStreaming = false;
        }
        return message;
    },
};
const baseFileOptions = {
    javaPackage: "",
    javaOuterClassname: "",
    javaMultipleFiles: false,
    javaGenerateEqualsAndHash: false,
    javaStringCheckUtf8: false,
    optimizeFor: 1,
    goPackage: "",
    ccGenericServices: false,
    javaGenericServices: false,
    pyGenericServices: false,
    phpGenericServices: false,
    deprecated: false,
    ccEnableArenas: false,
    objcClassPrefix: "",
    csharpNamespace: "",
    swiftPrefix: "",
    phpClassPrefix: "",
    phpNamespace: "",
    phpMetadataNamespace: "",
    rubyPackage: "",
};
export const FileOptions = {
    encode(message, writer = Writer.create()) {
        if (message.javaPackage !== "") {
            writer.uint32(10).string(message.javaPackage);
        }
        if (message.javaOuterClassname !== "") {
            writer.uint32(66).string(message.javaOuterClassname);
        }
        if (message.javaMultipleFiles === true) {
            writer.uint32(80).bool(message.javaMultipleFiles);
        }
        if (message.javaGenerateEqualsAndHash === true) {
            writer.uint32(160).bool(message.javaGenerateEqualsAndHash);
        }
        if (message.javaStringCheckUtf8 === true) {
            writer.uint32(216).bool(message.javaStringCheckUtf8);
        }
        if (message.optimizeFor !== 1) {
            writer.uint32(72).int32(message.optimizeFor);
        }
        if (message.goPackage !== "") {
            writer.uint32(90).string(message.goPackage);
        }
        if (message.ccGenericServices === true) {
            writer.uint32(128).bool(message.ccGenericServices);
        }
        if (message.javaGenericServices === true) {
            writer.uint32(136).bool(message.javaGenericServices);
        }
        if (message.pyGenericServices === true) {
            writer.uint32(144).bool(message.pyGenericServices);
        }
        if (message.phpGenericServices === true) {
            writer.uint32(336).bool(message.phpGenericServices);
        }
        if (message.deprecated === true) {
            writer.uint32(184).bool(message.deprecated);
        }
        if (message.ccEnableArenas === true) {
            writer.uint32(248).bool(message.ccEnableArenas);
        }
        if (message.objcClassPrefix !== "") {
            writer.uint32(290).string(message.objcClassPrefix);
        }
        if (message.csharpNamespace !== "") {
            writer.uint32(298).string(message.csharpNamespace);
        }
        if (message.swiftPrefix !== "") {
            writer.uint32(314).string(message.swiftPrefix);
        }
        if (message.phpClassPrefix !== "") {
            writer.uint32(322).string(message.phpClassPrefix);
        }
        if (message.phpNamespace !== "") {
            writer.uint32(330).string(message.phpNamespace);
        }
        if (message.phpMetadataNamespace !== "") {
            writer.uint32(354).string(message.phpMetadataNamespace);
        }
        if (message.rubyPackage !== "") {
            writer.uint32(362).string(message.rubyPackage);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFileOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.javaPackage = reader.string();
                    break;
                case 8:
                    message.javaOuterClassname = reader.string();
                    break;
                case 10:
                    message.javaMultipleFiles = reader.bool();
                    break;
                case 20:
                    message.javaGenerateEqualsAndHash = reader.bool();
                    break;
                case 27:
                    message.javaStringCheckUtf8 = reader.bool();
                    break;
                case 9:
                    message.optimizeFor = reader.int32();
                    break;
                case 11:
                    message.goPackage = reader.string();
                    break;
                case 16:
                    message.ccGenericServices = reader.bool();
                    break;
                case 17:
                    message.javaGenericServices = reader.bool();
                    break;
                case 18:
                    message.pyGenericServices = reader.bool();
                    break;
                case 42:
                    message.phpGenericServices = reader.bool();
                    break;
                case 23:
                    message.deprecated = reader.bool();
                    break;
                case 31:
                    message.ccEnableArenas = reader.bool();
                    break;
                case 36:
                    message.objcClassPrefix = reader.string();
                    break;
                case 37:
                    message.csharpNamespace = reader.string();
                    break;
                case 39:
                    message.swiftPrefix = reader.string();
                    break;
                case 40:
                    message.phpClassPrefix = reader.string();
                    break;
                case 41:
                    message.phpNamespace = reader.string();
                    break;
                case 44:
                    message.phpMetadataNamespace = reader.string();
                    break;
                case 45:
                    message.rubyPackage = reader.string();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseFileOptions };
        message.uninterpretedOption = [];
        if (object.javaPackage !== undefined && object.javaPackage !== null) {
            message.javaPackage = String(object.javaPackage);
        }
        else {
            message.javaPackage = "";
        }
        if (object.javaOuterClassname !== undefined &&
            object.javaOuterClassname !== null) {
            message.javaOuterClassname = String(object.javaOuterClassname);
        }
        else {
            message.javaOuterClassname = "";
        }
        if (object.javaMultipleFiles !== undefined &&
            object.javaMultipleFiles !== null) {
            message.javaMultipleFiles = Boolean(object.javaMultipleFiles);
        }
        else {
            message.javaMultipleFiles = false;
        }
        if (object.javaGenerateEqualsAndHash !== undefined &&
            object.javaGenerateEqualsAndHash !== null) {
            message.javaGenerateEqualsAndHash = Boolean(object.javaGenerateEqualsAndHash);
        }
        else {
            message.javaGenerateEqualsAndHash = false;
        }
        if (object.javaStringCheckUtf8 !== undefined &&
            object.javaStringCheckUtf8 !== null) {
            message.javaStringCheckUtf8 = Boolean(object.javaStringCheckUtf8);
        }
        else {
            message.javaStringCheckUtf8 = false;
        }
        if (object.optimizeFor !== undefined && object.optimizeFor !== null) {
            message.optimizeFor = fileOptions_OptimizeModeFromJSON(object.optimizeFor);
        }
        else {
            message.optimizeFor = 1;
        }
        if (object.goPackage !== undefined && object.goPackage !== null) {
            message.goPackage = String(object.goPackage);
        }
        else {
            message.goPackage = "";
        }
        if (object.ccGenericServices !== undefined &&
            object.ccGenericServices !== null) {
            message.ccGenericServices = Boolean(object.ccGenericServices);
        }
        else {
            message.ccGenericServices = false;
        }
        if (object.javaGenericServices !== undefined &&
            object.javaGenericServices !== null) {
            message.javaGenericServices = Boolean(object.javaGenericServices);
        }
        else {
            message.javaGenericServices = false;
        }
        if (object.pyGenericServices !== undefined &&
            object.pyGenericServices !== null) {
            message.pyGenericServices = Boolean(object.pyGenericServices);
        }
        else {
            message.pyGenericServices = false;
        }
        if (object.phpGenericServices !== undefined &&
            object.phpGenericServices !== null) {
            message.phpGenericServices = Boolean(object.phpGenericServices);
        }
        else {
            message.phpGenericServices = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.ccEnableArenas !== undefined && object.ccEnableArenas !== null) {
            message.ccEnableArenas = Boolean(object.ccEnableArenas);
        }
        else {
            message.ccEnableArenas = false;
        }
        if (object.objcClassPrefix !== undefined &&
            object.objcClassPrefix !== null) {
            message.objcClassPrefix = String(object.objcClassPrefix);
        }
        else {
            message.objcClassPrefix = "";
        }
        if (object.csharpNamespace !== undefined &&
            object.csharpNamespace !== null) {
            message.csharpNamespace = String(object.csharpNamespace);
        }
        else {
            message.csharpNamespace = "";
        }
        if (object.swiftPrefix !== undefined && object.swiftPrefix !== null) {
            message.swiftPrefix = String(object.swiftPrefix);
        }
        else {
            message.swiftPrefix = "";
        }
        if (object.phpClassPrefix !== undefined && object.phpClassPrefix !== null) {
            message.phpClassPrefix = String(object.phpClassPrefix);
        }
        else {
            message.phpClassPrefix = "";
        }
        if (object.phpNamespace !== undefined && object.phpNamespace !== null) {
            message.phpNamespace = String(object.phpNamespace);
        }
        else {
            message.phpNamespace = "";
        }
        if (object.phpMetadataNamespace !== undefined &&
            object.phpMetadataNamespace !== null) {
            message.phpMetadataNamespace = String(object.phpMetadataNamespace);
        }
        else {
            message.phpMetadataNamespace = "";
        }
        if (object.rubyPackage !== undefined && object.rubyPackage !== null) {
            message.rubyPackage = String(object.rubyPackage);
        }
        else {
            message.rubyPackage = "";
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.javaPackage !== undefined &&
            (obj.javaPackage = message.javaPackage);
        message.javaOuterClassname !== undefined &&
            (obj.javaOuterClassname = message.javaOuterClassname);
        message.javaMultipleFiles !== undefined &&
            (obj.javaMultipleFiles = message.javaMultipleFiles);
        message.javaGenerateEqualsAndHash !== undefined &&
            (obj.javaGenerateEqualsAndHash = message.javaGenerateEqualsAndHash);
        message.javaStringCheckUtf8 !== undefined &&
            (obj.javaStringCheckUtf8 = message.javaStringCheckUtf8);
        message.optimizeFor !== undefined &&
            (obj.optimizeFor = fileOptions_OptimizeModeToJSON(message.optimizeFor));
        message.goPackage !== undefined && (obj.goPackage = message.goPackage);
        message.ccGenericServices !== undefined &&
            (obj.ccGenericServices = message.ccGenericServices);
        message.javaGenericServices !== undefined &&
            (obj.javaGenericServices = message.javaGenericServices);
        message.pyGenericServices !== undefined &&
            (obj.pyGenericServices = message.pyGenericServices);
        message.phpGenericServices !== undefined &&
            (obj.phpGenericServices = message.phpGenericServices);
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        message.ccEnableArenas !== undefined &&
            (obj.ccEnableArenas = message.ccEnableArenas);
        message.objcClassPrefix !== undefined &&
            (obj.objcClassPrefix = message.objcClassPrefix);
        message.csharpNamespace !== undefined &&
            (obj.csharpNamespace = message.csharpNamespace);
        message.swiftPrefix !== undefined &&
            (obj.swiftPrefix = message.swiftPrefix);
        message.phpClassPrefix !== undefined &&
            (obj.phpClassPrefix = message.phpClassPrefix);
        message.phpNamespace !== undefined &&
            (obj.phpNamespace = message.phpNamespace);
        message.phpMetadataNamespace !== undefined &&
            (obj.phpMetadataNamespace = message.phpMetadataNamespace);
        message.rubyPackage !== undefined &&
            (obj.rubyPackage = message.rubyPackage);
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFileOptions };
        message.uninterpretedOption = [];
        if (object.javaPackage !== undefined && object.javaPackage !== null) {
            message.javaPackage = object.javaPackage;
        }
        else {
            message.javaPackage = "";
        }
        if (object.javaOuterClassname !== undefined &&
            object.javaOuterClassname !== null) {
            message.javaOuterClassname = object.javaOuterClassname;
        }
        else {
            message.javaOuterClassname = "";
        }
        if (object.javaMultipleFiles !== undefined &&
            object.javaMultipleFiles !== null) {
            message.javaMultipleFiles = object.javaMultipleFiles;
        }
        else {
            message.javaMultipleFiles = false;
        }
        if (object.javaGenerateEqualsAndHash !== undefined &&
            object.javaGenerateEqualsAndHash !== null) {
            message.javaGenerateEqualsAndHash = object.javaGenerateEqualsAndHash;
        }
        else {
            message.javaGenerateEqualsAndHash = false;
        }
        if (object.javaStringCheckUtf8 !== undefined &&
            object.javaStringCheckUtf8 !== null) {
            message.javaStringCheckUtf8 = object.javaStringCheckUtf8;
        }
        else {
            message.javaStringCheckUtf8 = false;
        }
        if (object.optimizeFor !== undefined && object.optimizeFor !== null) {
            message.optimizeFor = object.optimizeFor;
        }
        else {
            message.optimizeFor = 1;
        }
        if (object.goPackage !== undefined && object.goPackage !== null) {
            message.goPackage = object.goPackage;
        }
        else {
            message.goPackage = "";
        }
        if (object.ccGenericServices !== undefined &&
            object.ccGenericServices !== null) {
            message.ccGenericServices = object.ccGenericServices;
        }
        else {
            message.ccGenericServices = false;
        }
        if (object.javaGenericServices !== undefined &&
            object.javaGenericServices !== null) {
            message.javaGenericServices = object.javaGenericServices;
        }
        else {
            message.javaGenericServices = false;
        }
        if (object.pyGenericServices !== undefined &&
            object.pyGenericServices !== null) {
            message.pyGenericServices = object.pyGenericServices;
        }
        else {
            message.pyGenericServices = false;
        }
        if (object.phpGenericServices !== undefined &&
            object.phpGenericServices !== null) {
            message.phpGenericServices = object.phpGenericServices;
        }
        else {
            message.phpGenericServices = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.ccEnableArenas !== undefined && object.ccEnableArenas !== null) {
            message.ccEnableArenas = object.ccEnableArenas;
        }
        else {
            message.ccEnableArenas = false;
        }
        if (object.objcClassPrefix !== undefined &&
            object.objcClassPrefix !== null) {
            message.objcClassPrefix = object.objcClassPrefix;
        }
        else {
            message.objcClassPrefix = "";
        }
        if (object.csharpNamespace !== undefined &&
            object.csharpNamespace !== null) {
            message.csharpNamespace = object.csharpNamespace;
        }
        else {
            message.csharpNamespace = "";
        }
        if (object.swiftPrefix !== undefined && object.swiftPrefix !== null) {
            message.swiftPrefix = object.swiftPrefix;
        }
        else {
            message.swiftPrefix = "";
        }
        if (object.phpClassPrefix !== undefined && object.phpClassPrefix !== null) {
            message.phpClassPrefix = object.phpClassPrefix;
        }
        else {
            message.phpClassPrefix = "";
        }
        if (object.phpNamespace !== undefined && object.phpNamespace !== null) {
            message.phpNamespace = object.phpNamespace;
        }
        else {
            message.phpNamespace = "";
        }
        if (object.phpMetadataNamespace !== undefined &&
            object.phpMetadataNamespace !== null) {
            message.phpMetadataNamespace = object.phpMetadataNamespace;
        }
        else {
            message.phpMetadataNamespace = "";
        }
        if (object.rubyPackage !== undefined && object.rubyPackage !== null) {
            message.rubyPackage = object.rubyPackage;
        }
        else {
            message.rubyPackage = "";
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseMessageOptions = {
    messageSetWireFormat: false,
    noStandardDescriptorAccessor: false,
    deprecated: false,
    mapEntry: false,
};
export const MessageOptions = {
    encode(message, writer = Writer.create()) {
        if (message.messageSetWireFormat === true) {
            writer.uint32(8).bool(message.messageSetWireFormat);
        }
        if (message.noStandardDescriptorAccessor === true) {
            writer.uint32(16).bool(message.noStandardDescriptorAccessor);
        }
        if (message.deprecated === true) {
            writer.uint32(24).bool(message.deprecated);
        }
        if (message.mapEntry === true) {
            writer.uint32(56).bool(message.mapEntry);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMessageOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.messageSetWireFormat = reader.bool();
                    break;
                case 2:
                    message.noStandardDescriptorAccessor = reader.bool();
                    break;
                case 3:
                    message.deprecated = reader.bool();
                    break;
                case 7:
                    message.mapEntry = reader.bool();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMessageOptions };
        message.uninterpretedOption = [];
        if (object.messageSetWireFormat !== undefined &&
            object.messageSetWireFormat !== null) {
            message.messageSetWireFormat = Boolean(object.messageSetWireFormat);
        }
        else {
            message.messageSetWireFormat = false;
        }
        if (object.noStandardDescriptorAccessor !== undefined &&
            object.noStandardDescriptorAccessor !== null) {
            message.noStandardDescriptorAccessor = Boolean(object.noStandardDescriptorAccessor);
        }
        else {
            message.noStandardDescriptorAccessor = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.mapEntry !== undefined && object.mapEntry !== null) {
            message.mapEntry = Boolean(object.mapEntry);
        }
        else {
            message.mapEntry = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.messageSetWireFormat !== undefined &&
            (obj.messageSetWireFormat = message.messageSetWireFormat);
        message.noStandardDescriptorAccessor !== undefined &&
            (obj.noStandardDescriptorAccessor = message.noStandardDescriptorAccessor);
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        message.mapEntry !== undefined && (obj.mapEntry = message.mapEntry);
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMessageOptions };
        message.uninterpretedOption = [];
        if (object.messageSetWireFormat !== undefined &&
            object.messageSetWireFormat !== null) {
            message.messageSetWireFormat = object.messageSetWireFormat;
        }
        else {
            message.messageSetWireFormat = false;
        }
        if (object.noStandardDescriptorAccessor !== undefined &&
            object.noStandardDescriptorAccessor !== null) {
            message.noStandardDescriptorAccessor =
                object.noStandardDescriptorAccessor;
        }
        else {
            message.noStandardDescriptorAccessor = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.mapEntry !== undefined && object.mapEntry !== null) {
            message.mapEntry = object.mapEntry;
        }
        else {
            message.mapEntry = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseFieldOptions = {
    ctype: 0,
    packed: false,
    jstype: 0,
    lazy: false,
    deprecated: false,
    weak: false,
};
export const FieldOptions = {
    encode(message, writer = Writer.create()) {
        if (message.ctype !== 0) {
            writer.uint32(8).int32(message.ctype);
        }
        if (message.packed === true) {
            writer.uint32(16).bool(message.packed);
        }
        if (message.jstype !== 0) {
            writer.uint32(48).int32(message.jstype);
        }
        if (message.lazy === true) {
            writer.uint32(40).bool(message.lazy);
        }
        if (message.deprecated === true) {
            writer.uint32(24).bool(message.deprecated);
        }
        if (message.weak === true) {
            writer.uint32(80).bool(message.weak);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseFieldOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.ctype = reader.int32();
                    break;
                case 2:
                    message.packed = reader.bool();
                    break;
                case 6:
                    message.jstype = reader.int32();
                    break;
                case 5:
                    message.lazy = reader.bool();
                    break;
                case 3:
                    message.deprecated = reader.bool();
                    break;
                case 10:
                    message.weak = reader.bool();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseFieldOptions };
        message.uninterpretedOption = [];
        if (object.ctype !== undefined && object.ctype !== null) {
            message.ctype = fieldOptions_CTypeFromJSON(object.ctype);
        }
        else {
            message.ctype = 0;
        }
        if (object.packed !== undefined && object.packed !== null) {
            message.packed = Boolean(object.packed);
        }
        else {
            message.packed = false;
        }
        if (object.jstype !== undefined && object.jstype !== null) {
            message.jstype = fieldOptions_JSTypeFromJSON(object.jstype);
        }
        else {
            message.jstype = 0;
        }
        if (object.lazy !== undefined && object.lazy !== null) {
            message.lazy = Boolean(object.lazy);
        }
        else {
            message.lazy = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.weak !== undefined && object.weak !== null) {
            message.weak = Boolean(object.weak);
        }
        else {
            message.weak = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.ctype !== undefined &&
            (obj.ctype = fieldOptions_CTypeToJSON(message.ctype));
        message.packed !== undefined && (obj.packed = message.packed);
        message.jstype !== undefined &&
            (obj.jstype = fieldOptions_JSTypeToJSON(message.jstype));
        message.lazy !== undefined && (obj.lazy = message.lazy);
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        message.weak !== undefined && (obj.weak = message.weak);
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseFieldOptions };
        message.uninterpretedOption = [];
        if (object.ctype !== undefined && object.ctype !== null) {
            message.ctype = object.ctype;
        }
        else {
            message.ctype = 0;
        }
        if (object.packed !== undefined && object.packed !== null) {
            message.packed = object.packed;
        }
        else {
            message.packed = false;
        }
        if (object.jstype !== undefined && object.jstype !== null) {
            message.jstype = object.jstype;
        }
        else {
            message.jstype = 0;
        }
        if (object.lazy !== undefined && object.lazy !== null) {
            message.lazy = object.lazy;
        }
        else {
            message.lazy = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.weak !== undefined && object.weak !== null) {
            message.weak = object.weak;
        }
        else {
            message.weak = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseOneofOptions = {};
export const OneofOptions = {
    encode(message, writer = Writer.create()) {
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseOneofOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseOneofOptions };
        message.uninterpretedOption = [];
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseOneofOptions };
        message.uninterpretedOption = [];
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseEnumOptions = { allowAlias: false, deprecated: false };
export const EnumOptions = {
    encode(message, writer = Writer.create()) {
        if (message.allowAlias === true) {
            writer.uint32(16).bool(message.allowAlias);
        }
        if (message.deprecated === true) {
            writer.uint32(24).bool(message.deprecated);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEnumOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    message.allowAlias = reader.bool();
                    break;
                case 3:
                    message.deprecated = reader.bool();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEnumOptions };
        message.uninterpretedOption = [];
        if (object.allowAlias !== undefined && object.allowAlias !== null) {
            message.allowAlias = Boolean(object.allowAlias);
        }
        else {
            message.allowAlias = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.allowAlias !== undefined && (obj.allowAlias = message.allowAlias);
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEnumOptions };
        message.uninterpretedOption = [];
        if (object.allowAlias !== undefined && object.allowAlias !== null) {
            message.allowAlias = object.allowAlias;
        }
        else {
            message.allowAlias = false;
        }
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseEnumValueOptions = { deprecated: false };
export const EnumValueOptions = {
    encode(message, writer = Writer.create()) {
        if (message.deprecated === true) {
            writer.uint32(8).bool(message.deprecated);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseEnumValueOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.deprecated = reader.bool();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseEnumValueOptions };
        message.uninterpretedOption = [];
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseEnumValueOptions };
        message.uninterpretedOption = [];
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseServiceOptions = { deprecated: false };
export const ServiceOptions = {
    encode(message, writer = Writer.create()) {
        if (message.deprecated === true) {
            writer.uint32(264).bool(message.deprecated);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseServiceOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 33:
                    message.deprecated = reader.bool();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseServiceOptions };
        message.uninterpretedOption = [];
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseServiceOptions };
        message.uninterpretedOption = [];
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseMethodOptions = { deprecated: false, idempotencyLevel: 0 };
export const MethodOptions = {
    encode(message, writer = Writer.create()) {
        if (message.deprecated === true) {
            writer.uint32(264).bool(message.deprecated);
        }
        if (message.idempotencyLevel !== 0) {
            writer.uint32(272).int32(message.idempotencyLevel);
        }
        for (const v of message.uninterpretedOption) {
            UninterpretedOption.encode(v, writer.uint32(7994).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseMethodOptions };
        message.uninterpretedOption = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 33:
                    message.deprecated = reader.bool();
                    break;
                case 34:
                    message.idempotencyLevel = reader.int32();
                    break;
                case 999:
                    message.uninterpretedOption.push(UninterpretedOption.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseMethodOptions };
        message.uninterpretedOption = [];
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = Boolean(object.deprecated);
        }
        else {
            message.deprecated = false;
        }
        if (object.idempotencyLevel !== undefined &&
            object.idempotencyLevel !== null) {
            message.idempotencyLevel = methodOptions_IdempotencyLevelFromJSON(object.idempotencyLevel);
        }
        else {
            message.idempotencyLevel = 0;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.deprecated !== undefined && (obj.deprecated = message.deprecated);
        message.idempotencyLevel !== undefined &&
            (obj.idempotencyLevel = methodOptions_IdempotencyLevelToJSON(message.idempotencyLevel));
        if (message.uninterpretedOption) {
            obj.uninterpretedOption = message.uninterpretedOption.map((e) => e ? UninterpretedOption.toJSON(e) : undefined);
        }
        else {
            obj.uninterpretedOption = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseMethodOptions };
        message.uninterpretedOption = [];
        if (object.deprecated !== undefined && object.deprecated !== null) {
            message.deprecated = object.deprecated;
        }
        else {
            message.deprecated = false;
        }
        if (object.idempotencyLevel !== undefined &&
            object.idempotencyLevel !== null) {
            message.idempotencyLevel = object.idempotencyLevel;
        }
        else {
            message.idempotencyLevel = 0;
        }
        if (object.uninterpretedOption !== undefined &&
            object.uninterpretedOption !== null) {
            for (const e of object.uninterpretedOption) {
                message.uninterpretedOption.push(UninterpretedOption.fromPartial(e));
            }
        }
        return message;
    },
};
const baseUninterpretedOption = {
    identifierValue: "",
    positiveIntValue: 0,
    negativeIntValue: 0,
    doubleValue: 0,
    aggregateValue: "",
};
export const UninterpretedOption = {
    encode(message, writer = Writer.create()) {
        for (const v of message.name) {
            UninterpretedOption_NamePart.encode(v, writer.uint32(18).fork()).ldelim();
        }
        if (message.identifierValue !== "") {
            writer.uint32(26).string(message.identifierValue);
        }
        if (message.positiveIntValue !== 0) {
            writer.uint32(32).uint64(message.positiveIntValue);
        }
        if (message.negativeIntValue !== 0) {
            writer.uint32(40).int64(message.negativeIntValue);
        }
        if (message.doubleValue !== 0) {
            writer.uint32(49).double(message.doubleValue);
        }
        if (message.stringValue.length !== 0) {
            writer.uint32(58).bytes(message.stringValue);
        }
        if (message.aggregateValue !== "") {
            writer.uint32(66).string(message.aggregateValue);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseUninterpretedOption };
        message.name = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    message.name.push(UninterpretedOption_NamePart.decode(reader, reader.uint32()));
                    break;
                case 3:
                    message.identifierValue = reader.string();
                    break;
                case 4:
                    message.positiveIntValue = longToNumber(reader.uint64());
                    break;
                case 5:
                    message.negativeIntValue = longToNumber(reader.int64());
                    break;
                case 6:
                    message.doubleValue = reader.double();
                    break;
                case 7:
                    message.stringValue = reader.bytes();
                    break;
                case 8:
                    message.aggregateValue = reader.string();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseUninterpretedOption };
        message.name = [];
        if (object.name !== undefined && object.name !== null) {
            for (const e of object.name) {
                message.name.push(UninterpretedOption_NamePart.fromJSON(e));
            }
        }
        if (object.identifierValue !== undefined &&
            object.identifierValue !== null) {
            message.identifierValue = String(object.identifierValue);
        }
        else {
            message.identifierValue = "";
        }
        if (object.positiveIntValue !== undefined &&
            object.positiveIntValue !== null) {
            message.positiveIntValue = Number(object.positiveIntValue);
        }
        else {
            message.positiveIntValue = 0;
        }
        if (object.negativeIntValue !== undefined &&
            object.negativeIntValue !== null) {
            message.negativeIntValue = Number(object.negativeIntValue);
        }
        else {
            message.negativeIntValue = 0;
        }
        if (object.doubleValue !== undefined && object.doubleValue !== null) {
            message.doubleValue = Number(object.doubleValue);
        }
        else {
            message.doubleValue = 0;
        }
        if (object.stringValue !== undefined && object.stringValue !== null) {
            message.stringValue = bytesFromBase64(object.stringValue);
        }
        if (object.aggregateValue !== undefined && object.aggregateValue !== null) {
            message.aggregateValue = String(object.aggregateValue);
        }
        else {
            message.aggregateValue = "";
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.name) {
            obj.name = message.name.map((e) => e ? UninterpretedOption_NamePart.toJSON(e) : undefined);
        }
        else {
            obj.name = [];
        }
        message.identifierValue !== undefined &&
            (obj.identifierValue = message.identifierValue);
        message.positiveIntValue !== undefined &&
            (obj.positiveIntValue = message.positiveIntValue);
        message.negativeIntValue !== undefined &&
            (obj.negativeIntValue = message.negativeIntValue);
        message.doubleValue !== undefined &&
            (obj.doubleValue = message.doubleValue);
        message.stringValue !== undefined &&
            (obj.stringValue = base64FromBytes(message.stringValue !== undefined
                ? message.stringValue
                : new Uint8Array()));
        message.aggregateValue !== undefined &&
            (obj.aggregateValue = message.aggregateValue);
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseUninterpretedOption };
        message.name = [];
        if (object.name !== undefined && object.name !== null) {
            for (const e of object.name) {
                message.name.push(UninterpretedOption_NamePart.fromPartial(e));
            }
        }
        if (object.identifierValue !== undefined &&
            object.identifierValue !== null) {
            message.identifierValue = object.identifierValue;
        }
        else {
            message.identifierValue = "";
        }
        if (object.positiveIntValue !== undefined &&
            object.positiveIntValue !== null) {
            message.positiveIntValue = object.positiveIntValue;
        }
        else {
            message.positiveIntValue = 0;
        }
        if (object.negativeIntValue !== undefined &&
            object.negativeIntValue !== null) {
            message.negativeIntValue = object.negativeIntValue;
        }
        else {
            message.negativeIntValue = 0;
        }
        if (object.doubleValue !== undefined && object.doubleValue !== null) {
            message.doubleValue = object.doubleValue;
        }
        else {
            message.doubleValue = 0;
        }
        if (object.stringValue !== undefined && object.stringValue !== null) {
            message.stringValue = object.stringValue;
        }
        else {
            message.stringValue = new Uint8Array();
        }
        if (object.aggregateValue !== undefined && object.aggregateValue !== null) {
            message.aggregateValue = object.aggregateValue;
        }
        else {
            message.aggregateValue = "";
        }
        return message;
    },
};
const baseUninterpretedOption_NamePart = {
    namePart: "",
    isExtension: false,
};
export const UninterpretedOption_NamePart = {
    encode(message, writer = Writer.create()) {
        if (message.namePart !== "") {
            writer.uint32(10).string(message.namePart);
        }
        if (message.isExtension === true) {
            writer.uint32(16).bool(message.isExtension);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseUninterpretedOption_NamePart,
        };
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.namePart = reader.string();
                    break;
                case 2:
                    message.isExtension = reader.bool();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseUninterpretedOption_NamePart,
        };
        if (object.namePart !== undefined && object.namePart !== null) {
            message.namePart = String(object.namePart);
        }
        else {
            message.namePart = "";
        }
        if (object.isExtension !== undefined && object.isExtension !== null) {
            message.isExtension = Boolean(object.isExtension);
        }
        else {
            message.isExtension = false;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        message.namePart !== undefined && (obj.namePart = message.namePart);
        message.isExtension !== undefined &&
            (obj.isExtension = message.isExtension);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseUninterpretedOption_NamePart,
        };
        if (object.namePart !== undefined && object.namePart !== null) {
            message.namePart = object.namePart;
        }
        else {
            message.namePart = "";
        }
        if (object.isExtension !== undefined && object.isExtension !== null) {
            message.isExtension = object.isExtension;
        }
        else {
            message.isExtension = false;
        }
        return message;
    },
};
const baseSourceCodeInfo = {};
export const SourceCodeInfo = {
    encode(message, writer = Writer.create()) {
        for (const v of message.location) {
            SourceCodeInfo_Location.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseSourceCodeInfo };
        message.location = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.location.push(SourceCodeInfo_Location.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseSourceCodeInfo };
        message.location = [];
        if (object.location !== undefined && object.location !== null) {
            for (const e of object.location) {
                message.location.push(SourceCodeInfo_Location.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.location) {
            obj.location = message.location.map((e) => e ? SourceCodeInfo_Location.toJSON(e) : undefined);
        }
        else {
            obj.location = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseSourceCodeInfo };
        message.location = [];
        if (object.location !== undefined && object.location !== null) {
            for (const e of object.location) {
                message.location.push(SourceCodeInfo_Location.fromPartial(e));
            }
        }
        return message;
    },
};
const baseSourceCodeInfo_Location = {
    path: 0,
    span: 0,
    leadingComments: "",
    trailingComments: "",
    leadingDetachedComments: "",
};
export const SourceCodeInfo_Location = {
    encode(message, writer = Writer.create()) {
        writer.uint32(10).fork();
        for (const v of message.path) {
            writer.int32(v);
        }
        writer.ldelim();
        writer.uint32(18).fork();
        for (const v of message.span) {
            writer.int32(v);
        }
        writer.ldelim();
        if (message.leadingComments !== "") {
            writer.uint32(26).string(message.leadingComments);
        }
        if (message.trailingComments !== "") {
            writer.uint32(34).string(message.trailingComments);
        }
        for (const v of message.leadingDetachedComments) {
            writer.uint32(50).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseSourceCodeInfo_Location,
        };
        message.path = [];
        message.span = [];
        message.leadingDetachedComments = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.path.push(reader.int32());
                        }
                    }
                    else {
                        message.path.push(reader.int32());
                    }
                    break;
                case 2:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.span.push(reader.int32());
                        }
                    }
                    else {
                        message.span.push(reader.int32());
                    }
                    break;
                case 3:
                    message.leadingComments = reader.string();
                    break;
                case 4:
                    message.trailingComments = reader.string();
                    break;
                case 6:
                    message.leadingDetachedComments.push(reader.string());
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseSourceCodeInfo_Location,
        };
        message.path = [];
        message.span = [];
        message.leadingDetachedComments = [];
        if (object.path !== undefined && object.path !== null) {
            for (const e of object.path) {
                message.path.push(Number(e));
            }
        }
        if (object.span !== undefined && object.span !== null) {
            for (const e of object.span) {
                message.span.push(Number(e));
            }
        }
        if (object.leadingComments !== undefined &&
            object.leadingComments !== null) {
            message.leadingComments = String(object.leadingComments);
        }
        else {
            message.leadingComments = "";
        }
        if (object.trailingComments !== undefined &&
            object.trailingComments !== null) {
            message.trailingComments = String(object.trailingComments);
        }
        else {
            message.trailingComments = "";
        }
        if (object.leadingDetachedComments !== undefined &&
            object.leadingDetachedComments !== null) {
            for (const e of object.leadingDetachedComments) {
                message.leadingDetachedComments.push(String(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.path) {
            obj.path = message.path.map((e) => e);
        }
        else {
            obj.path = [];
        }
        if (message.span) {
            obj.span = message.span.map((e) => e);
        }
        else {
            obj.span = [];
        }
        message.leadingComments !== undefined &&
            (obj.leadingComments = message.leadingComments);
        message.trailingComments !== undefined &&
            (obj.trailingComments = message.trailingComments);
        if (message.leadingDetachedComments) {
            obj.leadingDetachedComments = message.leadingDetachedComments.map((e) => e);
        }
        else {
            obj.leadingDetachedComments = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseSourceCodeInfo_Location,
        };
        message.path = [];
        message.span = [];
        message.leadingDetachedComments = [];
        if (object.path !== undefined && object.path !== null) {
            for (const e of object.path) {
                message.path.push(e);
            }
        }
        if (object.span !== undefined && object.span !== null) {
            for (const e of object.span) {
                message.span.push(e);
            }
        }
        if (object.leadingComments !== undefined &&
            object.leadingComments !== null) {
            message.leadingComments = object.leadingComments;
        }
        else {
            message.leadingComments = "";
        }
        if (object.trailingComments !== undefined &&
            object.trailingComments !== null) {
            message.trailingComments = object.trailingComments;
        }
        else {
            message.trailingComments = "";
        }
        if (object.leadingDetachedComments !== undefined &&
            object.leadingDetachedComments !== null) {
            for (const e of object.leadingDetachedComments) {
                message.leadingDetachedComments.push(e);
            }
        }
        return message;
    },
};
const baseGeneratedCodeInfo = {};
export const GeneratedCodeInfo = {
    encode(message, writer = Writer.create()) {
        for (const v of message.annotation) {
            GeneratedCodeInfo_Annotation.encode(v, writer.uint32(10).fork()).ldelim();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = { ...baseGeneratedCodeInfo };
        message.annotation = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    message.annotation.push(GeneratedCodeInfo_Annotation.decode(reader, reader.uint32()));
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = { ...baseGeneratedCodeInfo };
        message.annotation = [];
        if (object.annotation !== undefined && object.annotation !== null) {
            for (const e of object.annotation) {
                message.annotation.push(GeneratedCodeInfo_Annotation.fromJSON(e));
            }
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.annotation) {
            obj.annotation = message.annotation.map((e) => e ? GeneratedCodeInfo_Annotation.toJSON(e) : undefined);
        }
        else {
            obj.annotation = [];
        }
        return obj;
    },
    fromPartial(object) {
        const message = { ...baseGeneratedCodeInfo };
        message.annotation = [];
        if (object.annotation !== undefined && object.annotation !== null) {
            for (const e of object.annotation) {
                message.annotation.push(GeneratedCodeInfo_Annotation.fromPartial(e));
            }
        }
        return message;
    },
};
const baseGeneratedCodeInfo_Annotation = {
    path: 0,
    sourceFile: "",
    begin: 0,
    end: 0,
};
export const GeneratedCodeInfo_Annotation = {
    encode(message, writer = Writer.create()) {
        writer.uint32(10).fork();
        for (const v of message.path) {
            writer.int32(v);
        }
        writer.ldelim();
        if (message.sourceFile !== "") {
            writer.uint32(18).string(message.sourceFile);
        }
        if (message.begin !== 0) {
            writer.uint32(24).int32(message.begin);
        }
        if (message.end !== 0) {
            writer.uint32(32).int32(message.end);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof Uint8Array ? new Reader(input) : input;
        let end = length === undefined ? reader.len : reader.pos + length;
        const message = {
            ...baseGeneratedCodeInfo_Annotation,
        };
        message.path = [];
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if ((tag & 7) === 2) {
                        const end2 = reader.uint32() + reader.pos;
                        while (reader.pos < end2) {
                            message.path.push(reader.int32());
                        }
                    }
                    else {
                        message.path.push(reader.int32());
                    }
                    break;
                case 2:
                    message.sourceFile = reader.string();
                    break;
                case 3:
                    message.begin = reader.int32();
                    break;
                case 4:
                    message.end = reader.int32();
                    break;
                default:
                    reader.skipType(tag & 7);
                    break;
            }
        }
        return message;
    },
    fromJSON(object) {
        const message = {
            ...baseGeneratedCodeInfo_Annotation,
        };
        message.path = [];
        if (object.path !== undefined && object.path !== null) {
            for (const e of object.path) {
                message.path.push(Number(e));
            }
        }
        if (object.sourceFile !== undefined && object.sourceFile !== null) {
            message.sourceFile = String(object.sourceFile);
        }
        else {
            message.sourceFile = "";
        }
        if (object.begin !== undefined && object.begin !== null) {
            message.begin = Number(object.begin);
        }
        else {
            message.begin = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = Number(object.end);
        }
        else {
            message.end = 0;
        }
        return message;
    },
    toJSON(message) {
        const obj = {};
        if (message.path) {
            obj.path = message.path.map((e) => e);
        }
        else {
            obj.path = [];
        }
        message.sourceFile !== undefined && (obj.sourceFile = message.sourceFile);
        message.begin !== undefined && (obj.begin = message.begin);
        message.end !== undefined && (obj.end = message.end);
        return obj;
    },
    fromPartial(object) {
        const message = {
            ...baseGeneratedCodeInfo_Annotation,
        };
        message.path = [];
        if (object.path !== undefined && object.path !== null) {
            for (const e of object.path) {
                message.path.push(e);
            }
        }
        if (object.sourceFile !== undefined && object.sourceFile !== null) {
            message.sourceFile = object.sourceFile;
        }
        else {
            message.sourceFile = "";
        }
        if (object.begin !== undefined && object.begin !== null) {
            message.begin = object.begin;
        }
        else {
            message.begin = 0;
        }
        if (object.end !== undefined && object.end !== null) {
            message.end = object.end;
        }
        else {
            message.end = 0;
        }
        return message;
    },
};
var globalThis = (() => {
    if (typeof globalThis !== "undefined")
        return globalThis;
    if (typeof self !== "undefined")
        return self;
    if (typeof window !== "undefined")
        return window;
    if (typeof global !== "undefined")
        return global;
    throw "Unable to locate global object";
})();
const atob = globalThis.atob ||
    ((b64) => globalThis.Buffer.from(b64, "base64").toString("binary"));
function bytesFromBase64(b64) {
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; ++i) {
        arr[i] = bin.charCodeAt(i);
    }
    return arr;
}
const btoa = globalThis.btoa ||
    ((bin) => globalThis.Buffer.from(bin, "binary").toString("base64"));
function base64FromBytes(arr) {
    const bin = [];
    for (let i = 0; i < arr.byteLength; ++i) {
        bin.push(String.fromCharCode(arr[i]));
    }
    return btoa(bin.join(""));
}
function longToNumber(long) {
    if (long.gt(Number.MAX_SAFE_INTEGER)) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    return long.toNumber();
}
if (util.Long !== Long) {
    util.Long = Long;
    configure();
}
