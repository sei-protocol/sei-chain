/* eslint-disable */
import * as Long from "long";
import { util, configure, Writer, Reader } from "protobufjs/minimal";

export const protobufPackage = "google.protobuf";

/**
 * The protocol compiler can output a FileDescriptorSet containing the .proto
 * files it parses.
 */
export interface FileDescriptorSet {
  file: FileDescriptorProto[];
}

/** Describes a complete .proto file. */
export interface FileDescriptorProto {
  /** file name, relative to root of source tree */
  name: string;
  /** e.g. "foo", "foo.bar", etc. */
  package: string;
  /** Names of files imported by this file. */
  dependency: string[];
  /** Indexes of the public imported files in the dependency list above. */
  public_dependency: number[];
  /**
   * Indexes of the weak imported files in the dependency list.
   * For Google-internal migration only. Do not use.
   */
  weak_dependency: number[];
  /** All top-level definitions in this file. */
  message_type: DescriptorProto[];
  enum_type: EnumDescriptorProto[];
  service: ServiceDescriptorProto[];
  extension: FieldDescriptorProto[];
  options: FileOptions | undefined;
  /**
   * This field contains optional information about the original source code.
   * You may safely remove this entire field without harming runtime
   * functionality of the descriptors -- the information is needed only by
   * development tools.
   */
  source_code_info: SourceCodeInfo | undefined;
  /**
   * The syntax of the proto file.
   * The supported values are "proto2" and "proto3".
   */
  syntax: string;
}

/** Describes a message type. */
export interface DescriptorProto {
  name: string;
  field: FieldDescriptorProto[];
  extension: FieldDescriptorProto[];
  nested_type: DescriptorProto[];
  enum_type: EnumDescriptorProto[];
  extension_range: DescriptorProto_ExtensionRange[];
  oneof_decl: OneofDescriptorProto[];
  options: MessageOptions | undefined;
  reserved_range: DescriptorProto_ReservedRange[];
  /**
   * Reserved field names, which may not be used by fields in the same message.
   * A given name may only be reserved once.
   */
  reserved_name: string[];
}

export interface DescriptorProto_ExtensionRange {
  /** Inclusive. */
  start: number;
  /** Exclusive. */
  end: number;
  options: ExtensionRangeOptions | undefined;
}

/**
 * Range of reserved tag numbers. Reserved tag numbers may not be used by
 * fields or extension ranges in the same message. Reserved ranges may
 * not overlap.
 */
export interface DescriptorProto_ReservedRange {
  /** Inclusive. */
  start: number;
  /** Exclusive. */
  end: number;
}

export interface ExtensionRangeOptions {
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

/** Describes a field within a message. */
export interface FieldDescriptorProto {
  name: string;
  number: number;
  label: FieldDescriptorProto_Label;
  /**
   * If type_name is set, this need not be set.  If both this and type_name
   * are set, this must be one of TYPE_ENUM, TYPE_MESSAGE or TYPE_GROUP.
   */
  type: FieldDescriptorProto_Type;
  /**
   * For message and enum types, this is the name of the type.  If the name
   * starts with a '.', it is fully-qualified.  Otherwise, C++-like scoping
   * rules are used to find the type (i.e. first the nested types within this
   * message are searched, then within the parent, on up to the root
   * namespace).
   */
  type_name: string;
  /**
   * For extensions, this is the name of the type being extended.  It is
   * resolved in the same manner as type_name.
   */
  extendee: string;
  /**
   * For numeric types, contains the original text representation of the value.
   * For booleans, "true" or "false".
   * For strings, contains the default text contents (not escaped in any way).
   * For bytes, contains the C escaped value.  All bytes >= 128 are escaped.
   * TODO(kenton):  Base-64 encode?
   */
  default_value: string;
  /**
   * If set, gives the index of a oneof in the containing type's oneof_decl
   * list.  This field is a member of that oneof.
   */
  oneof_index: number;
  /**
   * JSON name of this field. The value is set by protocol compiler. If the
   * user has set a "json_name" option on this field, that option's value
   * will be used. Otherwise, it's deduced from the field's name by converting
   * it to camelCase.
   */
  json_name: string;
  options: FieldOptions | undefined;
  /**
   * If true, this is a proto3 "optional". When a proto3 field is optional, it
   * tracks presence regardless of field type.
   *
   * When proto3_optional is true, this field must be belong to a oneof to
   * signal to old proto3 clients that presence is tracked for this field. This
   * oneof is known as a "synthetic" oneof, and this field must be its sole
   * member (each proto3 optional field gets its own synthetic oneof). Synthetic
   * oneofs exist in the descriptor only, and do not generate any API. Synthetic
   * oneofs must be ordered after all "real" oneofs.
   *
   * For message fields, proto3_optional doesn't create any semantic change,
   * since non-repeated message fields always track presence. However it still
   * indicates the semantic detail of whether the user wrote "optional" or not.
   * This can be useful for round-tripping the .proto file. For consistency we
   * give message fields a synthetic oneof also, even though it is not required
   * to track presence. This is especially important because the parser can't
   * tell if a field is a message or an enum, so it must always create a
   * synthetic oneof.
   *
   * Proto2 optional fields do not set this flag, because they already indicate
   * optional with `LABEL_OPTIONAL`.
   */
  proto3_optional: boolean;
}

export enum FieldDescriptorProto_Type {
  /**
   * TYPE_DOUBLE - 0 is reserved for errors.
   * Order is weird for historical reasons.
   */
  TYPE_DOUBLE = 1,
  TYPE_FLOAT = 2,
  /**
   * TYPE_INT64 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT64 if
   * negative values are likely.
   */
  TYPE_INT64 = 3,
  TYPE_UINT64 = 4,
  /**
   * TYPE_INT32 - Not ZigZag encoded.  Negative numbers take 10 bytes.  Use TYPE_SINT32 if
   * negative values are likely.
   */
  TYPE_INT32 = 5,
  TYPE_FIXED64 = 6,
  TYPE_FIXED32 = 7,
  TYPE_BOOL = 8,
  TYPE_STRING = 9,
  /**
   * TYPE_GROUP - Tag-delimited aggregate.
   * Group type is deprecated and not supported in proto3. However, Proto3
   * implementations should still be able to parse the group wire format and
   * treat group fields as unknown fields.
   */
  TYPE_GROUP = 10,
  /** TYPE_MESSAGE - Length-delimited aggregate. */
  TYPE_MESSAGE = 11,
  /** TYPE_BYTES - New in version 2. */
  TYPE_BYTES = 12,
  TYPE_UINT32 = 13,
  TYPE_ENUM = 14,
  TYPE_SFIXED32 = 15,
  TYPE_SFIXED64 = 16,
  /** TYPE_SINT32 - Uses ZigZag encoding. */
  TYPE_SINT32 = 17,
  /** TYPE_SINT64 - Uses ZigZag encoding. */
  TYPE_SINT64 = 18,
  UNRECOGNIZED = -1,
}

export function fieldDescriptorProto_TypeFromJSON(
  object: any
): FieldDescriptorProto_Type {
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

export function fieldDescriptorProto_TypeToJSON(
  object: FieldDescriptorProto_Type
): string {
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

export enum FieldDescriptorProto_Label {
  /** LABEL_OPTIONAL - 0 is reserved for errors */
  LABEL_OPTIONAL = 1,
  LABEL_REQUIRED = 2,
  LABEL_REPEATED = 3,
  UNRECOGNIZED = -1,
}

export function fieldDescriptorProto_LabelFromJSON(
  object: any
): FieldDescriptorProto_Label {
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

export function fieldDescriptorProto_LabelToJSON(
  object: FieldDescriptorProto_Label
): string {
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

/** Describes a oneof. */
export interface OneofDescriptorProto {
  name: string;
  options: OneofOptions | undefined;
}

/** Describes an enum type. */
export interface EnumDescriptorProto {
  name: string;
  value: EnumValueDescriptorProto[];
  options: EnumOptions | undefined;
  /**
   * Range of reserved numeric values. Reserved numeric values may not be used
   * by enum values in the same enum declaration. Reserved ranges may not
   * overlap.
   */
  reserved_range: EnumDescriptorProto_EnumReservedRange[];
  /**
   * Reserved enum value names, which may not be reused. A given name may only
   * be reserved once.
   */
  reserved_name: string[];
}

/**
 * Range of reserved numeric values. Reserved values may not be used by
 * entries in the same enum. Reserved ranges may not overlap.
 *
 * Note that this is distinct from DescriptorProto.ReservedRange in that it
 * is inclusive such that it can appropriately represent the entire int32
 * domain.
 */
export interface EnumDescriptorProto_EnumReservedRange {
  /** Inclusive. */
  start: number;
  /** Inclusive. */
  end: number;
}

/** Describes a value within an enum. */
export interface EnumValueDescriptorProto {
  name: string;
  number: number;
  options: EnumValueOptions | undefined;
}

/** Describes a service. */
export interface ServiceDescriptorProto {
  name: string;
  method: MethodDescriptorProto[];
  options: ServiceOptions | undefined;
}

/** Describes a method of a service. */
export interface MethodDescriptorProto {
  name: string;
  /**
   * Input and output type names.  These are resolved in the same way as
   * FieldDescriptorProto.type_name, but must refer to a message type.
   */
  input_type: string;
  output_type: string;
  options: MethodOptions | undefined;
  /** Identifies if client streams multiple client messages */
  client_streaming: boolean;
  /** Identifies if server streams multiple server messages */
  server_streaming: boolean;
}

export interface FileOptions {
  /**
   * Sets the Java package where classes generated from this .proto will be
   * placed.  By default, the proto package is used, but this is often
   * inappropriate because proto packages do not normally start with backwards
   * domain names.
   */
  java_package: string;
  /**
   * Controls the name of the wrapper Java class generated for the .proto file.
   * That class will always contain the .proto file's getDescriptor() method as
   * well as any top-level extensions defined in the .proto file.
   * If java_multiple_files is disabled, then all the other classes from the
   * .proto file will be nested inside the single wrapper outer class.
   */
  java_outer_classname: string;
  /**
   * If enabled, then the Java code generator will generate a separate .java
   * file for each top-level message, enum, and service defined in the .proto
   * file.  Thus, these types will *not* be nested inside the wrapper class
   * named by java_outer_classname.  However, the wrapper class will still be
   * generated to contain the file's getDescriptor() method as well as any
   * top-level extensions defined in the file.
   */
  java_multiple_files: boolean;
  /**
   * This option does nothing.
   *
   * @deprecated
   */
  java_generate_equals_and_hash: boolean;
  /**
   * If set true, then the Java2 code generator will generate code that
   * throws an exception whenever an attempt is made to assign a non-UTF-8
   * byte sequence to a string field.
   * Message reflection will do the same.
   * However, an extension field still accepts non-UTF-8 byte sequences.
   * This option has no effect on when used with the lite runtime.
   */
  java_string_check_utf8: boolean;
  optimize_for: FileOptions_OptimizeMode;
  /**
   * Sets the Go package where structs generated from this .proto will be
   * placed. If omitted, the Go package will be derived from the following:
   *   - The basename of the package import path, if provided.
   *   - Otherwise, the package statement in the .proto file, if present.
   *   - Otherwise, the basename of the .proto file, without extension.
   */
  go_package: string;
  /**
   * Should generic services be generated in each language?  "Generic" services
   * are not specific to any particular RPC system.  They are generated by the
   * main code generators in each language (without additional plugins).
   * Generic services were the only kind of service generation supported by
   * early versions of google.protobuf.
   *
   * Generic services are now considered deprecated in favor of using plugins
   * that generate code specific to your particular RPC system.  Therefore,
   * these default to false.  Old code which depends on generic services should
   * explicitly set them to true.
   */
  cc_generic_services: boolean;
  java_generic_services: boolean;
  py_generic_services: boolean;
  php_generic_services: boolean;
  /**
   * Is this file deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for everything in the file, or it will be completely ignored; in the very
   * least, this is a formalization for deprecating files.
   */
  deprecated: boolean;
  /**
   * Enables the use of arenas for the proto messages in this file. This applies
   * only to generated classes for C++.
   */
  cc_enable_arenas: boolean;
  /**
   * Sets the objective c class prefix which is prepended to all objective c
   * generated classes from this .proto. There is no default.
   */
  objc_class_prefix: string;
  /** Namespace for generated classes; defaults to the package. */
  csharp_namespace: string;
  /**
   * By default Swift generators will take the proto package and CamelCase it
   * replacing '.' with underscore and use that to prefix the types/symbols
   * defined. When this options is provided, they will use this value instead
   * to prefix the types/symbols defined.
   */
  swift_prefix: string;
  /**
   * Sets the php class prefix which is prepended to all php generated classes
   * from this .proto. Default is empty.
   */
  php_class_prefix: string;
  /**
   * Use this option to change the namespace of php generated classes. Default
   * is empty. When this option is empty, the package name will be used for
   * determining the namespace.
   */
  php_namespace: string;
  /**
   * Use this option to change the namespace of php generated metadata classes.
   * Default is empty. When this option is empty, the proto file name will be
   * used for determining the namespace.
   */
  php_metadata_namespace: string;
  /**
   * Use this option to change the package of ruby generated classes. Default
   * is empty. When this option is not set, the package name will be used for
   * determining the ruby package.
   */
  ruby_package: string;
  /**
   * The parser stores options it doesn't recognize here.
   * See the documentation for the "Options" section above.
   */
  uninterpreted_option: UninterpretedOption[];
}

/** Generated classes can be optimized for speed or code size. */
export enum FileOptions_OptimizeMode {
  /** SPEED - Generate complete code for parsing, serialization, */
  SPEED = 1,
  /** CODE_SIZE - etc. */
  CODE_SIZE = 2,
  /** LITE_RUNTIME - Generate code using MessageLite and the lite runtime. */
  LITE_RUNTIME = 3,
  UNRECOGNIZED = -1,
}

export function fileOptions_OptimizeModeFromJSON(
  object: any
): FileOptions_OptimizeMode {
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

export function fileOptions_OptimizeModeToJSON(
  object: FileOptions_OptimizeMode
): string {
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

export interface MessageOptions {
  /**
   * Set true to use the old proto1 MessageSet wire format for extensions.
   * This is provided for backwards-compatibility with the MessageSet wire
   * format.  You should not use this for any other reason:  It's less
   * efficient, has fewer features, and is more complicated.
   *
   * The message must be defined exactly as follows:
   *   message Foo {
   *     option message_set_wire_format = true;
   *     extensions 4 to max;
   *   }
   * Note that the message cannot have any defined fields; MessageSets only
   * have extensions.
   *
   * All extensions of your type must be singular messages; e.g. they cannot
   * be int32s, enums, or repeated messages.
   *
   * Because this is an option, the above two restrictions are not enforced by
   * the protocol compiler.
   */
  message_set_wire_format: boolean;
  /**
   * Disables the generation of the standard "descriptor()" accessor, which can
   * conflict with a field of the same name.  This is meant to make migration
   * from proto1 easier; new code should avoid fields named "descriptor".
   */
  no_standard_descriptor_accessor: boolean;
  /**
   * Is this message deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for the message, or it will be completely ignored; in the very least,
   * this is a formalization for deprecating messages.
   */
  deprecated: boolean;
  /**
   * Whether the message is an automatically generated map entry type for the
   * maps field.
   *
   * For maps fields:
   *     map<KeyType, ValueType> map_field = 1;
   * The parsed descriptor looks like:
   *     message MapFieldEntry {
   *         option map_entry = true;
   *         optional KeyType key = 1;
   *         optional ValueType value = 2;
   *     }
   *     repeated MapFieldEntry map_field = 1;
   *
   * Implementations may choose not to generate the map_entry=true message, but
   * use a native map in the target language to hold the keys and values.
   * The reflection APIs in such implementations still need to work as
   * if the field is a repeated message field.
   *
   * NOTE: Do not set the option in .proto files. Always use the maps syntax
   * instead. The option should only be implicitly set by the proto compiler
   * parser.
   */
  map_entry: boolean;
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

export interface FieldOptions {
  /**
   * The ctype option instructs the C++ code generator to use a different
   * representation of the field than it normally would.  See the specific
   * options below.  This option is not yet implemented in the open source
   * release -- sorry, we'll try to include it in a future version!
   */
  ctype: FieldOptions_CType;
  /**
   * The packed option can be enabled for repeated primitive fields to enable
   * a more efficient representation on the wire. Rather than repeatedly
   * writing the tag and type for each element, the entire array is encoded as
   * a single length-delimited blob. In proto3, only explicit setting it to
   * false will avoid using packed encoding.
   */
  packed: boolean;
  /**
   * The jstype option determines the JavaScript type used for values of the
   * field.  The option is permitted only for 64 bit integral and fixed types
   * (int64, uint64, sint64, fixed64, sfixed64).  A field with jstype JS_STRING
   * is represented as JavaScript string, which avoids loss of precision that
   * can happen when a large value is converted to a floating point JavaScript.
   * Specifying JS_NUMBER for the jstype causes the generated JavaScript code to
   * use the JavaScript "number" type.  The behavior of the default option
   * JS_NORMAL is implementation dependent.
   *
   * This option is an enum to permit additional types to be added, e.g.
   * goog.math.Integer.
   */
  jstype: FieldOptions_JSType;
  /**
   * Should this field be parsed lazily?  Lazy applies only to message-type
   * fields.  It means that when the outer message is initially parsed, the
   * inner message's contents will not be parsed but instead stored in encoded
   * form.  The inner message will actually be parsed when it is first accessed.
   *
   * This is only a hint.  Implementations are free to choose whether to use
   * eager or lazy parsing regardless of the value of this option.  However,
   * setting this option true suggests that the protocol author believes that
   * using lazy parsing on this field is worth the additional bookkeeping
   * overhead typically needed to implement it.
   *
   * This option does not affect the public interface of any generated code;
   * all method signatures remain the same.  Furthermore, thread-safety of the
   * interface is not affected by this option; const methods remain safe to
   * call from multiple threads concurrently, while non-const methods continue
   * to require exclusive access.
   *
   *
   * Note that implementations may choose not to check required fields within
   * a lazy sub-message.  That is, calling IsInitialized() on the outer message
   * may return true even if the inner message has missing required fields.
   * This is necessary because otherwise the inner message would have to be
   * parsed in order to perform the check, defeating the purpose of lazy
   * parsing.  An implementation which chooses not to check required fields
   * must be consistent about it.  That is, for any particular sub-message, the
   * implementation must either *always* check its required fields, or *never*
   * check its required fields, regardless of whether or not the message has
   * been parsed.
   */
  lazy: boolean;
  /**
   * Is this field deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for accessors, or it will be completely ignored; in the very least, this
   * is a formalization for deprecating fields.
   */
  deprecated: boolean;
  /** For Google-internal migration only. Do not use. */
  weak: boolean;
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

export enum FieldOptions_CType {
  /** STRING - Default mode. */
  STRING = 0,
  CORD = 1,
  STRING_PIECE = 2,
  UNRECOGNIZED = -1,
}

export function fieldOptions_CTypeFromJSON(object: any): FieldOptions_CType {
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

export function fieldOptions_CTypeToJSON(object: FieldOptions_CType): string {
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

export enum FieldOptions_JSType {
  /** JS_NORMAL - Use the default type. */
  JS_NORMAL = 0,
  /** JS_STRING - Use JavaScript strings. */
  JS_STRING = 1,
  /** JS_NUMBER - Use JavaScript numbers. */
  JS_NUMBER = 2,
  UNRECOGNIZED = -1,
}

export function fieldOptions_JSTypeFromJSON(object: any): FieldOptions_JSType {
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

export function fieldOptions_JSTypeToJSON(object: FieldOptions_JSType): string {
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

export interface OneofOptions {
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

export interface EnumOptions {
  /**
   * Set this option to true to allow mapping different tag names to the same
   * value.
   */
  allow_alias: boolean;
  /**
   * Is this enum deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for the enum, or it will be completely ignored; in the very least, this
   * is a formalization for deprecating enums.
   */
  deprecated: boolean;
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

export interface EnumValueOptions {
  /**
   * Is this enum value deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for the enum value, or it will be completely ignored; in the very least,
   * this is a formalization for deprecating enum values.
   */
  deprecated: boolean;
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

export interface ServiceOptions {
  /**
   * Is this service deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for the service, or it will be completely ignored; in the very least,
   * this is a formalization for deprecating services.
   */
  deprecated: boolean;
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

export interface MethodOptions {
  /**
   * Is this method deprecated?
   * Depending on the target platform, this can emit Deprecated annotations
   * for the method, or it will be completely ignored; in the very least,
   * this is a formalization for deprecating methods.
   */
  deprecated: boolean;
  idempotency_level: MethodOptions_IdempotencyLevel;
  /** The parser stores options it doesn't recognize here. See above. */
  uninterpreted_option: UninterpretedOption[];
}

/**
 * Is this method side-effect-free (or safe in HTTP parlance), or idempotent,
 * or neither? HTTP based RPC implementation may choose GET verb for safe
 * methods, and PUT verb for idempotent methods instead of the default POST.
 */
export enum MethodOptions_IdempotencyLevel {
  IDEMPOTENCY_UNKNOWN = 0,
  /** NO_SIDE_EFFECTS - implies idempotent */
  NO_SIDE_EFFECTS = 1,
  /** IDEMPOTENT - idempotent, but may have side effects */
  IDEMPOTENT = 2,
  UNRECOGNIZED = -1,
}

export function methodOptions_IdempotencyLevelFromJSON(
  object: any
): MethodOptions_IdempotencyLevel {
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

export function methodOptions_IdempotencyLevelToJSON(
  object: MethodOptions_IdempotencyLevel
): string {
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

/**
 * A message representing a option the parser does not recognize. This only
 * appears in options protos created by the compiler::Parser class.
 * DescriptorPool resolves these when building Descriptor objects. Therefore,
 * options protos in descriptor objects (e.g. returned by Descriptor::options(),
 * or produced by Descriptor::CopyTo()) will never have UninterpretedOptions
 * in them.
 */
export interface UninterpretedOption {
  name: UninterpretedOption_NamePart[];
  /**
   * The value of the uninterpreted option, in whatever type the tokenizer
   * identified it as during parsing. Exactly one of these should be set.
   */
  identifier_value: string;
  positive_int_value: number;
  negative_int_value: number;
  double_value: number;
  string_value: Uint8Array;
  aggregate_value: string;
}

/**
 * The name of the uninterpreted option.  Each string represents a segment in
 * a dot-separated name.  is_extension is true iff a segment represents an
 * extension (denoted with parentheses in options specs in .proto files).
 * E.g.,{ ["foo", false], ["bar.baz", true], ["qux", false] } represents
 * "foo.(bar.baz).qux".
 */
export interface UninterpretedOption_NamePart {
  name_part: string;
  is_extension: boolean;
}

/**
 * Encapsulates information about the original source file from which a
 * FileDescriptorProto was generated.
 */
export interface SourceCodeInfo {
  /**
   * A Location identifies a piece of source code in a .proto file which
   * corresponds to a particular definition.  This information is intended
   * to be useful to IDEs, code indexers, documentation generators, and similar
   * tools.
   *
   * For example, say we have a file like:
   *   message Foo {
   *     optional string foo = 1;
   *   }
   * Let's look at just the field definition:
   *   optional string foo = 1;
   *   ^       ^^     ^^  ^  ^^^
   *   a       bc     de  f  ghi
   * We have the following locations:
   *   span   path               represents
   *   [a,i)  [ 4, 0, 2, 0 ]     The whole field definition.
   *   [a,b)  [ 4, 0, 2, 0, 4 ]  The label (optional).
   *   [c,d)  [ 4, 0, 2, 0, 5 ]  The type (string).
   *   [e,f)  [ 4, 0, 2, 0, 1 ]  The name (foo).
   *   [g,h)  [ 4, 0, 2, 0, 3 ]  The number (1).
   *
   * Notes:
   * - A location may refer to a repeated field itself (i.e. not to any
   *   particular index within it).  This is used whenever a set of elements are
   *   logically enclosed in a single code segment.  For example, an entire
   *   extend block (possibly containing multiple extension definitions) will
   *   have an outer location whose path refers to the "extensions" repeated
   *   field without an index.
   * - Multiple locations may have the same path.  This happens when a single
   *   logical declaration is spread out across multiple places.  The most
   *   obvious example is the "extend" block again -- there may be multiple
   *   extend blocks in the same scope, each of which will have the same path.
   * - A location's span is not always a subset of its parent's span.  For
   *   example, the "extendee" of an extension declaration appears at the
   *   beginning of the "extend" block and is shared by all extensions within
   *   the block.
   * - Just because a location's span is a subset of some other location's span
   *   does not mean that it is a descendant.  For example, a "group" defines
   *   both a type and a field in a single declaration.  Thus, the locations
   *   corresponding to the type and field and their components will overlap.
   * - Code which tries to interpret locations should probably be designed to
   *   ignore those that it doesn't understand, as more types of locations could
   *   be recorded in the future.
   */
  location: SourceCodeInfo_Location[];
}

export interface SourceCodeInfo_Location {
  /**
   * Identifies which part of the FileDescriptorProto was defined at this
   * location.
   *
   * Each element is a field number or an index.  They form a path from
   * the root FileDescriptorProto to the place where the definition.  For
   * example, this path:
   *   [ 4, 3, 2, 7, 1 ]
   * refers to:
   *   file.message_type(3)  // 4, 3
   *       .field(7)         // 2, 7
   *       .name()           // 1
   * This is because FileDescriptorProto.message_type has field number 4:
   *   repeated DescriptorProto message_type = 4;
   * and DescriptorProto.field has field number 2:
   *   repeated FieldDescriptorProto field = 2;
   * and FieldDescriptorProto.name has field number 1:
   *   optional string name = 1;
   *
   * Thus, the above path gives the location of a field name.  If we removed
   * the last element:
   *   [ 4, 3, 2, 7 ]
   * this path refers to the whole field declaration (from the beginning
   * of the label to the terminating semicolon).
   */
  path: number[];
  /**
   * Always has exactly three or four elements: start line, start column,
   * end line (optional, otherwise assumed same as start line), end column.
   * These are packed into a single field for efficiency.  Note that line
   * and column numbers are zero-based -- typically you will want to add
   * 1 to each before displaying to a user.
   */
  span: number[];
  /**
   * If this SourceCodeInfo represents a complete declaration, these are any
   * comments appearing before and after the declaration which appear to be
   * attached to the declaration.
   *
   * A series of line comments appearing on consecutive lines, with no other
   * tokens appearing on those lines, will be treated as a single comment.
   *
   * leading_detached_comments will keep paragraphs of comments that appear
   * before (but not connected to) the current element. Each paragraph,
   * separated by empty lines, will be one comment element in the repeated
   * field.
   *
   * Only the comment content is provided; comment markers (e.g. //) are
   * stripped out.  For block comments, leading whitespace and an asterisk
   * will be stripped from the beginning of each line other than the first.
   * Newlines are included in the output.
   *
   * Examples:
   *
   *   optional int32 foo = 1;  // Comment attached to foo.
   *   // Comment attached to bar.
   *   optional int32 bar = 2;
   *
   *   optional string baz = 3;
   *   // Comment attached to baz.
   *   // Another line attached to baz.
   *
   *   // Comment attached to qux.
   *   //
   *   // Another line attached to qux.
   *   optional double qux = 4;
   *
   *   // Detached comment for corge. This is not leading or trailing comments
   *   // to qux or corge because there are blank lines separating it from
   *   // both.
   *
   *   // Detached comment for corge paragraph 2.
   *
   *   optional string corge = 5;
   *   /* Block comment attached
   *    * to corge.  Leading asterisks
   *    * will be removed. * /
   *   /* Block comment attached to
   *    * grault. * /
   *   optional int32 grault = 6;
   *
   *   // ignored detached comments.
   */
  leading_comments: string;
  trailing_comments: string;
  leading_detached_comments: string[];
}

/**
 * Describes the relationship between generated code and its original source
 * file. A GeneratedCodeInfo message is associated with only one generated
 * source file, but may contain references to different source .proto files.
 */
export interface GeneratedCodeInfo {
  /**
   * An Annotation connects some span of text in generated code to an element
   * of its generating .proto file.
   */
  annotation: GeneratedCodeInfo_Annotation[];
}

export interface GeneratedCodeInfo_Annotation {
  /**
   * Identifies the element in the original source .proto file. This field
   * is formatted the same as SourceCodeInfo.Location.path.
   */
  path: number[];
  /** Identifies the filesystem path to the original source .proto. */
  source_file: string;
  /**
   * Identifies the starting offset in bytes in the generated code
   * that relates to the identified object.
   */
  begin: number;
  /**
   * Identifies the ending offset in bytes in the generated code that
   * relates to the identified offset. The end offset should be one past
   * the last relevant byte (so the length of the text = end - begin).
   */
  end: number;
}

const baseFileDescriptorSet: object = {};

export const FileDescriptorSet = {
  encode(message: FileDescriptorSet, writer: Writer = Writer.create()): Writer {
    for (const v of message.file) {
      FileDescriptorProto.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FileDescriptorSet {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFileDescriptorSet } as FileDescriptorSet;
    message.file = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.file.push(
            FileDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FileDescriptorSet {
    const message = { ...baseFileDescriptorSet } as FileDescriptorSet;
    message.file = [];
    if (object.file !== undefined && object.file !== null) {
      for (const e of object.file) {
        message.file.push(FileDescriptorProto.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: FileDescriptorSet): unknown {
    const obj: any = {};
    if (message.file) {
      obj.file = message.file.map((e) =>
        e ? FileDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.file = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<FileDescriptorSet>): FileDescriptorSet {
    const message = { ...baseFileDescriptorSet } as FileDescriptorSet;
    message.file = [];
    if (object.file !== undefined && object.file !== null) {
      for (const e of object.file) {
        message.file.push(FileDescriptorProto.fromPartial(e));
      }
    }
    return message;
  },
};

const baseFileDescriptorProto: object = {
  name: "",
  package: "",
  dependency: "",
  public_dependency: 0,
  weak_dependency: 0,
  syntax: "",
};

export const FileDescriptorProto = {
  encode(
    message: FileDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    if (message.package !== "") {
      writer.uint32(18).string(message.package);
    }
    for (const v of message.dependency) {
      writer.uint32(26).string(v!);
    }
    writer.uint32(82).fork();
    for (const v of message.public_dependency) {
      writer.int32(v);
    }
    writer.ldelim();
    writer.uint32(90).fork();
    for (const v of message.weak_dependency) {
      writer.int32(v);
    }
    writer.ldelim();
    for (const v of message.message_type) {
      DescriptorProto.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.enum_type) {
      EnumDescriptorProto.encode(v!, writer.uint32(42).fork()).ldelim();
    }
    for (const v of message.service) {
      ServiceDescriptorProto.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    for (const v of message.extension) {
      FieldDescriptorProto.encode(v!, writer.uint32(58).fork()).ldelim();
    }
    if (message.options !== undefined) {
      FileOptions.encode(message.options, writer.uint32(66).fork()).ldelim();
    }
    if (message.source_code_info !== undefined) {
      SourceCodeInfo.encode(
        message.source_code_info,
        writer.uint32(74).fork()
      ).ldelim();
    }
    if (message.syntax !== "") {
      writer.uint32(98).string(message.syntax);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FileDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFileDescriptorProto } as FileDescriptorProto;
    message.dependency = [];
    message.public_dependency = [];
    message.weak_dependency = [];
    message.message_type = [];
    message.enum_type = [];
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
              message.public_dependency.push(reader.int32());
            }
          } else {
            message.public_dependency.push(reader.int32());
          }
          break;
        case 11:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.weak_dependency.push(reader.int32());
            }
          } else {
            message.weak_dependency.push(reader.int32());
          }
          break;
        case 4:
          message.message_type.push(
            DescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.enum_type.push(
            EnumDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.service.push(
            ServiceDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.extension.push(
            FieldDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 8:
          message.options = FileOptions.decode(reader, reader.uint32());
          break;
        case 9:
          message.source_code_info = SourceCodeInfo.decode(
            reader,
            reader.uint32()
          );
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

  fromJSON(object: any): FileDescriptorProto {
    const message = { ...baseFileDescriptorProto } as FileDescriptorProto;
    message.dependency = [];
    message.public_dependency = [];
    message.weak_dependency = [];
    message.message_type = [];
    message.enum_type = [];
    message.service = [];
    message.extension = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.package !== undefined && object.package !== null) {
      message.package = String(object.package);
    } else {
      message.package = "";
    }
    if (object.dependency !== undefined && object.dependency !== null) {
      for (const e of object.dependency) {
        message.dependency.push(String(e));
      }
    }
    if (
      object.public_dependency !== undefined &&
      object.public_dependency !== null
    ) {
      for (const e of object.public_dependency) {
        message.public_dependency.push(Number(e));
      }
    }
    if (
      object.weak_dependency !== undefined &&
      object.weak_dependency !== null
    ) {
      for (const e of object.weak_dependency) {
        message.weak_dependency.push(Number(e));
      }
    }
    if (object.message_type !== undefined && object.message_type !== null) {
      for (const e of object.message_type) {
        message.message_type.push(DescriptorProto.fromJSON(e));
      }
    }
    if (object.enum_type !== undefined && object.enum_type !== null) {
      for (const e of object.enum_type) {
        message.enum_type.push(EnumDescriptorProto.fromJSON(e));
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
    } else {
      message.options = undefined;
    }
    if (
      object.source_code_info !== undefined &&
      object.source_code_info !== null
    ) {
      message.source_code_info = SourceCodeInfo.fromJSON(
        object.source_code_info
      );
    } else {
      message.source_code_info = undefined;
    }
    if (object.syntax !== undefined && object.syntax !== null) {
      message.syntax = String(object.syntax);
    } else {
      message.syntax = "";
    }
    return message;
  },

  toJSON(message: FileDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    message.package !== undefined && (obj.package = message.package);
    if (message.dependency) {
      obj.dependency = message.dependency.map((e) => e);
    } else {
      obj.dependency = [];
    }
    if (message.public_dependency) {
      obj.public_dependency = message.public_dependency.map((e) => e);
    } else {
      obj.public_dependency = [];
    }
    if (message.weak_dependency) {
      obj.weak_dependency = message.weak_dependency.map((e) => e);
    } else {
      obj.weak_dependency = [];
    }
    if (message.message_type) {
      obj.message_type = message.message_type.map((e) =>
        e ? DescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.message_type = [];
    }
    if (message.enum_type) {
      obj.enum_type = message.enum_type.map((e) =>
        e ? EnumDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.enum_type = [];
    }
    if (message.service) {
      obj.service = message.service.map((e) =>
        e ? ServiceDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.service = [];
    }
    if (message.extension) {
      obj.extension = message.extension.map((e) =>
        e ? FieldDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.extension = [];
    }
    message.options !== undefined &&
      (obj.options = message.options
        ? FileOptions.toJSON(message.options)
        : undefined);
    message.source_code_info !== undefined &&
      (obj.source_code_info = message.source_code_info
        ? SourceCodeInfo.toJSON(message.source_code_info)
        : undefined);
    message.syntax !== undefined && (obj.syntax = message.syntax);
    return obj;
  },

  fromPartial(object: DeepPartial<FileDescriptorProto>): FileDescriptorProto {
    const message = { ...baseFileDescriptorProto } as FileDescriptorProto;
    message.dependency = [];
    message.public_dependency = [];
    message.weak_dependency = [];
    message.message_type = [];
    message.enum_type = [];
    message.service = [];
    message.extension = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.package !== undefined && object.package !== null) {
      message.package = object.package;
    } else {
      message.package = "";
    }
    if (object.dependency !== undefined && object.dependency !== null) {
      for (const e of object.dependency) {
        message.dependency.push(e);
      }
    }
    if (
      object.public_dependency !== undefined &&
      object.public_dependency !== null
    ) {
      for (const e of object.public_dependency) {
        message.public_dependency.push(e);
      }
    }
    if (
      object.weak_dependency !== undefined &&
      object.weak_dependency !== null
    ) {
      for (const e of object.weak_dependency) {
        message.weak_dependency.push(e);
      }
    }
    if (object.message_type !== undefined && object.message_type !== null) {
      for (const e of object.message_type) {
        message.message_type.push(DescriptorProto.fromPartial(e));
      }
    }
    if (object.enum_type !== undefined && object.enum_type !== null) {
      for (const e of object.enum_type) {
        message.enum_type.push(EnumDescriptorProto.fromPartial(e));
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
    } else {
      message.options = undefined;
    }
    if (
      object.source_code_info !== undefined &&
      object.source_code_info !== null
    ) {
      message.source_code_info = SourceCodeInfo.fromPartial(
        object.source_code_info
      );
    } else {
      message.source_code_info = undefined;
    }
    if (object.syntax !== undefined && object.syntax !== null) {
      message.syntax = object.syntax;
    } else {
      message.syntax = "";
    }
    return message;
  },
};

const baseDescriptorProto: object = { name: "", reserved_name: "" };

export const DescriptorProto = {
  encode(message: DescriptorProto, writer: Writer = Writer.create()): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    for (const v of message.field) {
      FieldDescriptorProto.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    for (const v of message.extension) {
      FieldDescriptorProto.encode(v!, writer.uint32(50).fork()).ldelim();
    }
    for (const v of message.nested_type) {
      DescriptorProto.encode(v!, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.enum_type) {
      EnumDescriptorProto.encode(v!, writer.uint32(34).fork()).ldelim();
    }
    for (const v of message.extension_range) {
      DescriptorProto_ExtensionRange.encode(
        v!,
        writer.uint32(42).fork()
      ).ldelim();
    }
    for (const v of message.oneof_decl) {
      OneofDescriptorProto.encode(v!, writer.uint32(66).fork()).ldelim();
    }
    if (message.options !== undefined) {
      MessageOptions.encode(message.options, writer.uint32(58).fork()).ldelim();
    }
    for (const v of message.reserved_range) {
      DescriptorProto_ReservedRange.encode(
        v!,
        writer.uint32(74).fork()
      ).ldelim();
    }
    for (const v of message.reserved_name) {
      writer.uint32(82).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): DescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseDescriptorProto } as DescriptorProto;
    message.field = [];
    message.extension = [];
    message.nested_type = [];
    message.enum_type = [];
    message.extension_range = [];
    message.oneof_decl = [];
    message.reserved_range = [];
    message.reserved_name = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.name = reader.string();
          break;
        case 2:
          message.field.push(
            FieldDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 6:
          message.extension.push(
            FieldDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.nested_type.push(
            DescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 4:
          message.enum_type.push(
            EnumDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 5:
          message.extension_range.push(
            DescriptorProto_ExtensionRange.decode(reader, reader.uint32())
          );
          break;
        case 8:
          message.oneof_decl.push(
            OneofDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 7:
          message.options = MessageOptions.decode(reader, reader.uint32());
          break;
        case 9:
          message.reserved_range.push(
            DescriptorProto_ReservedRange.decode(reader, reader.uint32())
          );
          break;
        case 10:
          message.reserved_name.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): DescriptorProto {
    const message = { ...baseDescriptorProto } as DescriptorProto;
    message.field = [];
    message.extension = [];
    message.nested_type = [];
    message.enum_type = [];
    message.extension_range = [];
    message.oneof_decl = [];
    message.reserved_range = [];
    message.reserved_name = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
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
    if (object.nested_type !== undefined && object.nested_type !== null) {
      for (const e of object.nested_type) {
        message.nested_type.push(DescriptorProto.fromJSON(e));
      }
    }
    if (object.enum_type !== undefined && object.enum_type !== null) {
      for (const e of object.enum_type) {
        message.enum_type.push(EnumDescriptorProto.fromJSON(e));
      }
    }
    if (
      object.extension_range !== undefined &&
      object.extension_range !== null
    ) {
      for (const e of object.extension_range) {
        message.extension_range.push(
          DescriptorProto_ExtensionRange.fromJSON(e)
        );
      }
    }
    if (object.oneof_decl !== undefined && object.oneof_decl !== null) {
      for (const e of object.oneof_decl) {
        message.oneof_decl.push(OneofDescriptorProto.fromJSON(e));
      }
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = MessageOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    if (object.reserved_range !== undefined && object.reserved_range !== null) {
      for (const e of object.reserved_range) {
        message.reserved_range.push(DescriptorProto_ReservedRange.fromJSON(e));
      }
    }
    if (object.reserved_name !== undefined && object.reserved_name !== null) {
      for (const e of object.reserved_name) {
        message.reserved_name.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: DescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    if (message.field) {
      obj.field = message.field.map((e) =>
        e ? FieldDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.field = [];
    }
    if (message.extension) {
      obj.extension = message.extension.map((e) =>
        e ? FieldDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.extension = [];
    }
    if (message.nested_type) {
      obj.nested_type = message.nested_type.map((e) =>
        e ? DescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.nested_type = [];
    }
    if (message.enum_type) {
      obj.enum_type = message.enum_type.map((e) =>
        e ? EnumDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.enum_type = [];
    }
    if (message.extension_range) {
      obj.extension_range = message.extension_range.map((e) =>
        e ? DescriptorProto_ExtensionRange.toJSON(e) : undefined
      );
    } else {
      obj.extension_range = [];
    }
    if (message.oneof_decl) {
      obj.oneof_decl = message.oneof_decl.map((e) =>
        e ? OneofDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.oneof_decl = [];
    }
    message.options !== undefined &&
      (obj.options = message.options
        ? MessageOptions.toJSON(message.options)
        : undefined);
    if (message.reserved_range) {
      obj.reserved_range = message.reserved_range.map((e) =>
        e ? DescriptorProto_ReservedRange.toJSON(e) : undefined
      );
    } else {
      obj.reserved_range = [];
    }
    if (message.reserved_name) {
      obj.reserved_name = message.reserved_name.map((e) => e);
    } else {
      obj.reserved_name = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<DescriptorProto>): DescriptorProto {
    const message = { ...baseDescriptorProto } as DescriptorProto;
    message.field = [];
    message.extension = [];
    message.nested_type = [];
    message.enum_type = [];
    message.extension_range = [];
    message.oneof_decl = [];
    message.reserved_range = [];
    message.reserved_name = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
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
    if (object.nested_type !== undefined && object.nested_type !== null) {
      for (const e of object.nested_type) {
        message.nested_type.push(DescriptorProto.fromPartial(e));
      }
    }
    if (object.enum_type !== undefined && object.enum_type !== null) {
      for (const e of object.enum_type) {
        message.enum_type.push(EnumDescriptorProto.fromPartial(e));
      }
    }
    if (
      object.extension_range !== undefined &&
      object.extension_range !== null
    ) {
      for (const e of object.extension_range) {
        message.extension_range.push(
          DescriptorProto_ExtensionRange.fromPartial(e)
        );
      }
    }
    if (object.oneof_decl !== undefined && object.oneof_decl !== null) {
      for (const e of object.oneof_decl) {
        message.oneof_decl.push(OneofDescriptorProto.fromPartial(e));
      }
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = MessageOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    if (object.reserved_range !== undefined && object.reserved_range !== null) {
      for (const e of object.reserved_range) {
        message.reserved_range.push(
          DescriptorProto_ReservedRange.fromPartial(e)
        );
      }
    }
    if (object.reserved_name !== undefined && object.reserved_name !== null) {
      for (const e of object.reserved_name) {
        message.reserved_name.push(e);
      }
    }
    return message;
  },
};

const baseDescriptorProto_ExtensionRange: object = { start: 0, end: 0 };

export const DescriptorProto_ExtensionRange = {
  encode(
    message: DescriptorProto_ExtensionRange,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.start !== 0) {
      writer.uint32(8).int32(message.start);
    }
    if (message.end !== 0) {
      writer.uint32(16).int32(message.end);
    }
    if (message.options !== undefined) {
      ExtensionRangeOptions.encode(
        message.options,
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): DescriptorProto_ExtensionRange {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseDescriptorProto_ExtensionRange,
    } as DescriptorProto_ExtensionRange;
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
          message.options = ExtensionRangeOptions.decode(
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

  fromJSON(object: any): DescriptorProto_ExtensionRange {
    const message = {
      ...baseDescriptorProto_ExtensionRange,
    } as DescriptorProto_ExtensionRange;
    if (object.start !== undefined && object.start !== null) {
      message.start = Number(object.start);
    } else {
      message.start = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = Number(object.end);
    } else {
      message.end = 0;
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = ExtensionRangeOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },

  toJSON(message: DescriptorProto_ExtensionRange): unknown {
    const obj: any = {};
    message.start !== undefined && (obj.start = message.start);
    message.end !== undefined && (obj.end = message.end);
    message.options !== undefined &&
      (obj.options = message.options
        ? ExtensionRangeOptions.toJSON(message.options)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DescriptorProto_ExtensionRange>
  ): DescriptorProto_ExtensionRange {
    const message = {
      ...baseDescriptorProto_ExtensionRange,
    } as DescriptorProto_ExtensionRange;
    if (object.start !== undefined && object.start !== null) {
      message.start = object.start;
    } else {
      message.start = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = object.end;
    } else {
      message.end = 0;
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = ExtensionRangeOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },
};

const baseDescriptorProto_ReservedRange: object = { start: 0, end: 0 };

export const DescriptorProto_ReservedRange = {
  encode(
    message: DescriptorProto_ReservedRange,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.start !== 0) {
      writer.uint32(8).int32(message.start);
    }
    if (message.end !== 0) {
      writer.uint32(16).int32(message.end);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): DescriptorProto_ReservedRange {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseDescriptorProto_ReservedRange,
    } as DescriptorProto_ReservedRange;
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

  fromJSON(object: any): DescriptorProto_ReservedRange {
    const message = {
      ...baseDescriptorProto_ReservedRange,
    } as DescriptorProto_ReservedRange;
    if (object.start !== undefined && object.start !== null) {
      message.start = Number(object.start);
    } else {
      message.start = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = Number(object.end);
    } else {
      message.end = 0;
    }
    return message;
  },

  toJSON(message: DescriptorProto_ReservedRange): unknown {
    const obj: any = {};
    message.start !== undefined && (obj.start = message.start);
    message.end !== undefined && (obj.end = message.end);
    return obj;
  },

  fromPartial(
    object: DeepPartial<DescriptorProto_ReservedRange>
  ): DescriptorProto_ReservedRange {
    const message = {
      ...baseDescriptorProto_ReservedRange,
    } as DescriptorProto_ReservedRange;
    if (object.start !== undefined && object.start !== null) {
      message.start = object.start;
    } else {
      message.start = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = object.end;
    } else {
      message.end = 0;
    }
    return message;
  },
};

const baseExtensionRangeOptions: object = {};

export const ExtensionRangeOptions = {
  encode(
    message: ExtensionRangeOptions,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ExtensionRangeOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseExtensionRangeOptions } as ExtensionRangeOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ExtensionRangeOptions {
    const message = { ...baseExtensionRangeOptions } as ExtensionRangeOptions;
    message.uninterpreted_option = [];
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ExtensionRangeOptions): unknown {
    const obj: any = {};
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<ExtensionRangeOptions>
  ): ExtensionRangeOptions {
    const message = { ...baseExtensionRangeOptions } as ExtensionRangeOptions;
    message.uninterpreted_option = [];
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseFieldDescriptorProto: object = {
  name: "",
  number: 0,
  label: 1,
  type: 1,
  type_name: "",
  extendee: "",
  default_value: "",
  oneof_index: 0,
  json_name: "",
  proto3_optional: false,
};

export const FieldDescriptorProto = {
  encode(
    message: FieldDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
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
    if (message.type_name !== "") {
      writer.uint32(50).string(message.type_name);
    }
    if (message.extendee !== "") {
      writer.uint32(18).string(message.extendee);
    }
    if (message.default_value !== "") {
      writer.uint32(58).string(message.default_value);
    }
    if (message.oneof_index !== 0) {
      writer.uint32(72).int32(message.oneof_index);
    }
    if (message.json_name !== "") {
      writer.uint32(82).string(message.json_name);
    }
    if (message.options !== undefined) {
      FieldOptions.encode(message.options, writer.uint32(66).fork()).ldelim();
    }
    if (message.proto3_optional === true) {
      writer.uint32(136).bool(message.proto3_optional);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FieldDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFieldDescriptorProto } as FieldDescriptorProto;
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
          message.label = reader.int32() as any;
          break;
        case 5:
          message.type = reader.int32() as any;
          break;
        case 6:
          message.type_name = reader.string();
          break;
        case 2:
          message.extendee = reader.string();
          break;
        case 7:
          message.default_value = reader.string();
          break;
        case 9:
          message.oneof_index = reader.int32();
          break;
        case 10:
          message.json_name = reader.string();
          break;
        case 8:
          message.options = FieldOptions.decode(reader, reader.uint32());
          break;
        case 17:
          message.proto3_optional = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FieldDescriptorProto {
    const message = { ...baseFieldDescriptorProto } as FieldDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.number !== undefined && object.number !== null) {
      message.number = Number(object.number);
    } else {
      message.number = 0;
    }
    if (object.label !== undefined && object.label !== null) {
      message.label = fieldDescriptorProto_LabelFromJSON(object.label);
    } else {
      message.label = 1;
    }
    if (object.type !== undefined && object.type !== null) {
      message.type = fieldDescriptorProto_TypeFromJSON(object.type);
    } else {
      message.type = 1;
    }
    if (object.type_name !== undefined && object.type_name !== null) {
      message.type_name = String(object.type_name);
    } else {
      message.type_name = "";
    }
    if (object.extendee !== undefined && object.extendee !== null) {
      message.extendee = String(object.extendee);
    } else {
      message.extendee = "";
    }
    if (object.default_value !== undefined && object.default_value !== null) {
      message.default_value = String(object.default_value);
    } else {
      message.default_value = "";
    }
    if (object.oneof_index !== undefined && object.oneof_index !== null) {
      message.oneof_index = Number(object.oneof_index);
    } else {
      message.oneof_index = 0;
    }
    if (object.json_name !== undefined && object.json_name !== null) {
      message.json_name = String(object.json_name);
    } else {
      message.json_name = "";
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = FieldOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    if (
      object.proto3_optional !== undefined &&
      object.proto3_optional !== null
    ) {
      message.proto3_optional = Boolean(object.proto3_optional);
    } else {
      message.proto3_optional = false;
    }
    return message;
  },

  toJSON(message: FieldDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    message.number !== undefined && (obj.number = message.number);
    message.label !== undefined &&
      (obj.label = fieldDescriptorProto_LabelToJSON(message.label));
    message.type !== undefined &&
      (obj.type = fieldDescriptorProto_TypeToJSON(message.type));
    message.type_name !== undefined && (obj.type_name = message.type_name);
    message.extendee !== undefined && (obj.extendee = message.extendee);
    message.default_value !== undefined &&
      (obj.default_value = message.default_value);
    message.oneof_index !== undefined &&
      (obj.oneof_index = message.oneof_index);
    message.json_name !== undefined && (obj.json_name = message.json_name);
    message.options !== undefined &&
      (obj.options = message.options
        ? FieldOptions.toJSON(message.options)
        : undefined);
    message.proto3_optional !== undefined &&
      (obj.proto3_optional = message.proto3_optional);
    return obj;
  },

  fromPartial(object: DeepPartial<FieldDescriptorProto>): FieldDescriptorProto {
    const message = { ...baseFieldDescriptorProto } as FieldDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.number !== undefined && object.number !== null) {
      message.number = object.number;
    } else {
      message.number = 0;
    }
    if (object.label !== undefined && object.label !== null) {
      message.label = object.label;
    } else {
      message.label = 1;
    }
    if (object.type !== undefined && object.type !== null) {
      message.type = object.type;
    } else {
      message.type = 1;
    }
    if (object.type_name !== undefined && object.type_name !== null) {
      message.type_name = object.type_name;
    } else {
      message.type_name = "";
    }
    if (object.extendee !== undefined && object.extendee !== null) {
      message.extendee = object.extendee;
    } else {
      message.extendee = "";
    }
    if (object.default_value !== undefined && object.default_value !== null) {
      message.default_value = object.default_value;
    } else {
      message.default_value = "";
    }
    if (object.oneof_index !== undefined && object.oneof_index !== null) {
      message.oneof_index = object.oneof_index;
    } else {
      message.oneof_index = 0;
    }
    if (object.json_name !== undefined && object.json_name !== null) {
      message.json_name = object.json_name;
    } else {
      message.json_name = "";
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = FieldOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    if (
      object.proto3_optional !== undefined &&
      object.proto3_optional !== null
    ) {
      message.proto3_optional = object.proto3_optional;
    } else {
      message.proto3_optional = false;
    }
    return message;
  },
};

const baseOneofDescriptorProto: object = { name: "" };

export const OneofDescriptorProto = {
  encode(
    message: OneofDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    if (message.options !== undefined) {
      OneofOptions.encode(message.options, writer.uint32(18).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): OneofDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOneofDescriptorProto } as OneofDescriptorProto;
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

  fromJSON(object: any): OneofDescriptorProto {
    const message = { ...baseOneofDescriptorProto } as OneofDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = OneofOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },

  toJSON(message: OneofDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    message.options !== undefined &&
      (obj.options = message.options
        ? OneofOptions.toJSON(message.options)
        : undefined);
    return obj;
  },

  fromPartial(object: DeepPartial<OneofDescriptorProto>): OneofDescriptorProto {
    const message = { ...baseOneofDescriptorProto } as OneofDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = OneofOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },
};

const baseEnumDescriptorProto: object = { name: "", reserved_name: "" };

export const EnumDescriptorProto = {
  encode(
    message: EnumDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    for (const v of message.value) {
      EnumValueDescriptorProto.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.options !== undefined) {
      EnumOptions.encode(message.options, writer.uint32(26).fork()).ldelim();
    }
    for (const v of message.reserved_range) {
      EnumDescriptorProto_EnumReservedRange.encode(
        v!,
        writer.uint32(34).fork()
      ).ldelim();
    }
    for (const v of message.reserved_name) {
      writer.uint32(42).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): EnumDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEnumDescriptorProto } as EnumDescriptorProto;
    message.value = [];
    message.reserved_range = [];
    message.reserved_name = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.name = reader.string();
          break;
        case 2:
          message.value.push(
            EnumValueDescriptorProto.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.options = EnumOptions.decode(reader, reader.uint32());
          break;
        case 4:
          message.reserved_range.push(
            EnumDescriptorProto_EnumReservedRange.decode(
              reader,
              reader.uint32()
            )
          );
          break;
        case 5:
          message.reserved_name.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): EnumDescriptorProto {
    const message = { ...baseEnumDescriptorProto } as EnumDescriptorProto;
    message.value = [];
    message.reserved_range = [];
    message.reserved_name = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.value !== undefined && object.value !== null) {
      for (const e of object.value) {
        message.value.push(EnumValueDescriptorProto.fromJSON(e));
      }
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = EnumOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    if (object.reserved_range !== undefined && object.reserved_range !== null) {
      for (const e of object.reserved_range) {
        message.reserved_range.push(
          EnumDescriptorProto_EnumReservedRange.fromJSON(e)
        );
      }
    }
    if (object.reserved_name !== undefined && object.reserved_name !== null) {
      for (const e of object.reserved_name) {
        message.reserved_name.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: EnumDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    if (message.value) {
      obj.value = message.value.map((e) =>
        e ? EnumValueDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.value = [];
    }
    message.options !== undefined &&
      (obj.options = message.options
        ? EnumOptions.toJSON(message.options)
        : undefined);
    if (message.reserved_range) {
      obj.reserved_range = message.reserved_range.map((e) =>
        e ? EnumDescriptorProto_EnumReservedRange.toJSON(e) : undefined
      );
    } else {
      obj.reserved_range = [];
    }
    if (message.reserved_name) {
      obj.reserved_name = message.reserved_name.map((e) => e);
    } else {
      obj.reserved_name = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<EnumDescriptorProto>): EnumDescriptorProto {
    const message = { ...baseEnumDescriptorProto } as EnumDescriptorProto;
    message.value = [];
    message.reserved_range = [];
    message.reserved_name = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.value !== undefined && object.value !== null) {
      for (const e of object.value) {
        message.value.push(EnumValueDescriptorProto.fromPartial(e));
      }
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = EnumOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    if (object.reserved_range !== undefined && object.reserved_range !== null) {
      for (const e of object.reserved_range) {
        message.reserved_range.push(
          EnumDescriptorProto_EnumReservedRange.fromPartial(e)
        );
      }
    }
    if (object.reserved_name !== undefined && object.reserved_name !== null) {
      for (const e of object.reserved_name) {
        message.reserved_name.push(e);
      }
    }
    return message;
  },
};

const baseEnumDescriptorProto_EnumReservedRange: object = { start: 0, end: 0 };

export const EnumDescriptorProto_EnumReservedRange = {
  encode(
    message: EnumDescriptorProto_EnumReservedRange,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.start !== 0) {
      writer.uint32(8).int32(message.start);
    }
    if (message.end !== 0) {
      writer.uint32(16).int32(message.end);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): EnumDescriptorProto_EnumReservedRange {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseEnumDescriptorProto_EnumReservedRange,
    } as EnumDescriptorProto_EnumReservedRange;
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

  fromJSON(object: any): EnumDescriptorProto_EnumReservedRange {
    const message = {
      ...baseEnumDescriptorProto_EnumReservedRange,
    } as EnumDescriptorProto_EnumReservedRange;
    if (object.start !== undefined && object.start !== null) {
      message.start = Number(object.start);
    } else {
      message.start = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = Number(object.end);
    } else {
      message.end = 0;
    }
    return message;
  },

  toJSON(message: EnumDescriptorProto_EnumReservedRange): unknown {
    const obj: any = {};
    message.start !== undefined && (obj.start = message.start);
    message.end !== undefined && (obj.end = message.end);
    return obj;
  },

  fromPartial(
    object: DeepPartial<EnumDescriptorProto_EnumReservedRange>
  ): EnumDescriptorProto_EnumReservedRange {
    const message = {
      ...baseEnumDescriptorProto_EnumReservedRange,
    } as EnumDescriptorProto_EnumReservedRange;
    if (object.start !== undefined && object.start !== null) {
      message.start = object.start;
    } else {
      message.start = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = object.end;
    } else {
      message.end = 0;
    }
    return message;
  },
};

const baseEnumValueDescriptorProto: object = { name: "", number: 0 };

export const EnumValueDescriptorProto = {
  encode(
    message: EnumValueDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    if (message.number !== 0) {
      writer.uint32(16).int32(message.number);
    }
    if (message.options !== undefined) {
      EnumValueOptions.encode(
        message.options,
        writer.uint32(26).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): EnumValueDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseEnumValueDescriptorProto,
    } as EnumValueDescriptorProto;
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

  fromJSON(object: any): EnumValueDescriptorProto {
    const message = {
      ...baseEnumValueDescriptorProto,
    } as EnumValueDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.number !== undefined && object.number !== null) {
      message.number = Number(object.number);
    } else {
      message.number = 0;
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = EnumValueOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },

  toJSON(message: EnumValueDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    message.number !== undefined && (obj.number = message.number);
    message.options !== undefined &&
      (obj.options = message.options
        ? EnumValueOptions.toJSON(message.options)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<EnumValueDescriptorProto>
  ): EnumValueDescriptorProto {
    const message = {
      ...baseEnumValueDescriptorProto,
    } as EnumValueDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.number !== undefined && object.number !== null) {
      message.number = object.number;
    } else {
      message.number = 0;
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = EnumValueOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },
};

const baseServiceDescriptorProto: object = { name: "" };

export const ServiceDescriptorProto = {
  encode(
    message: ServiceDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    for (const v of message.method) {
      MethodDescriptorProto.encode(v!, writer.uint32(18).fork()).ldelim();
    }
    if (message.options !== undefined) {
      ServiceOptions.encode(message.options, writer.uint32(26).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ServiceDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseServiceDescriptorProto } as ServiceDescriptorProto;
    message.method = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.name = reader.string();
          break;
        case 2:
          message.method.push(
            MethodDescriptorProto.decode(reader, reader.uint32())
          );
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

  fromJSON(object: any): ServiceDescriptorProto {
    const message = { ...baseServiceDescriptorProto } as ServiceDescriptorProto;
    message.method = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.method !== undefined && object.method !== null) {
      for (const e of object.method) {
        message.method.push(MethodDescriptorProto.fromJSON(e));
      }
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = ServiceOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },

  toJSON(message: ServiceDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    if (message.method) {
      obj.method = message.method.map((e) =>
        e ? MethodDescriptorProto.toJSON(e) : undefined
      );
    } else {
      obj.method = [];
    }
    message.options !== undefined &&
      (obj.options = message.options
        ? ServiceOptions.toJSON(message.options)
        : undefined);
    return obj;
  },

  fromPartial(
    object: DeepPartial<ServiceDescriptorProto>
  ): ServiceDescriptorProto {
    const message = { ...baseServiceDescriptorProto } as ServiceDescriptorProto;
    message.method = [];
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.method !== undefined && object.method !== null) {
      for (const e of object.method) {
        message.method.push(MethodDescriptorProto.fromPartial(e));
      }
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = ServiceOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    return message;
  },
};

const baseMethodDescriptorProto: object = {
  name: "",
  input_type: "",
  output_type: "",
  client_streaming: false,
  server_streaming: false,
};

export const MethodDescriptorProto = {
  encode(
    message: MethodDescriptorProto,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name !== "") {
      writer.uint32(10).string(message.name);
    }
    if (message.input_type !== "") {
      writer.uint32(18).string(message.input_type);
    }
    if (message.output_type !== "") {
      writer.uint32(26).string(message.output_type);
    }
    if (message.options !== undefined) {
      MethodOptions.encode(message.options, writer.uint32(34).fork()).ldelim();
    }
    if (message.client_streaming === true) {
      writer.uint32(40).bool(message.client_streaming);
    }
    if (message.server_streaming === true) {
      writer.uint32(48).bool(message.server_streaming);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MethodDescriptorProto {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMethodDescriptorProto } as MethodDescriptorProto;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.name = reader.string();
          break;
        case 2:
          message.input_type = reader.string();
          break;
        case 3:
          message.output_type = reader.string();
          break;
        case 4:
          message.options = MethodOptions.decode(reader, reader.uint32());
          break;
        case 5:
          message.client_streaming = reader.bool();
          break;
        case 6:
          message.server_streaming = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MethodDescriptorProto {
    const message = { ...baseMethodDescriptorProto } as MethodDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = String(object.name);
    } else {
      message.name = "";
    }
    if (object.input_type !== undefined && object.input_type !== null) {
      message.input_type = String(object.input_type);
    } else {
      message.input_type = "";
    }
    if (object.output_type !== undefined && object.output_type !== null) {
      message.output_type = String(object.output_type);
    } else {
      message.output_type = "";
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = MethodOptions.fromJSON(object.options);
    } else {
      message.options = undefined;
    }
    if (
      object.client_streaming !== undefined &&
      object.client_streaming !== null
    ) {
      message.client_streaming = Boolean(object.client_streaming);
    } else {
      message.client_streaming = false;
    }
    if (
      object.server_streaming !== undefined &&
      object.server_streaming !== null
    ) {
      message.server_streaming = Boolean(object.server_streaming);
    } else {
      message.server_streaming = false;
    }
    return message;
  },

  toJSON(message: MethodDescriptorProto): unknown {
    const obj: any = {};
    message.name !== undefined && (obj.name = message.name);
    message.input_type !== undefined && (obj.input_type = message.input_type);
    message.output_type !== undefined &&
      (obj.output_type = message.output_type);
    message.options !== undefined &&
      (obj.options = message.options
        ? MethodOptions.toJSON(message.options)
        : undefined);
    message.client_streaming !== undefined &&
      (obj.client_streaming = message.client_streaming);
    message.server_streaming !== undefined &&
      (obj.server_streaming = message.server_streaming);
    return obj;
  },

  fromPartial(
    object: DeepPartial<MethodDescriptorProto>
  ): MethodDescriptorProto {
    const message = { ...baseMethodDescriptorProto } as MethodDescriptorProto;
    if (object.name !== undefined && object.name !== null) {
      message.name = object.name;
    } else {
      message.name = "";
    }
    if (object.input_type !== undefined && object.input_type !== null) {
      message.input_type = object.input_type;
    } else {
      message.input_type = "";
    }
    if (object.output_type !== undefined && object.output_type !== null) {
      message.output_type = object.output_type;
    } else {
      message.output_type = "";
    }
    if (object.options !== undefined && object.options !== null) {
      message.options = MethodOptions.fromPartial(object.options);
    } else {
      message.options = undefined;
    }
    if (
      object.client_streaming !== undefined &&
      object.client_streaming !== null
    ) {
      message.client_streaming = object.client_streaming;
    } else {
      message.client_streaming = false;
    }
    if (
      object.server_streaming !== undefined &&
      object.server_streaming !== null
    ) {
      message.server_streaming = object.server_streaming;
    } else {
      message.server_streaming = false;
    }
    return message;
  },
};

const baseFileOptions: object = {
  java_package: "",
  java_outer_classname: "",
  java_multiple_files: false,
  java_generate_equals_and_hash: false,
  java_string_check_utf8: false,
  optimize_for: 1,
  go_package: "",
  cc_generic_services: false,
  java_generic_services: false,
  py_generic_services: false,
  php_generic_services: false,
  deprecated: false,
  cc_enable_arenas: false,
  objc_class_prefix: "",
  csharp_namespace: "",
  swift_prefix: "",
  php_class_prefix: "",
  php_namespace: "",
  php_metadata_namespace: "",
  ruby_package: "",
};

export const FileOptions = {
  encode(message: FileOptions, writer: Writer = Writer.create()): Writer {
    if (message.java_package !== "") {
      writer.uint32(10).string(message.java_package);
    }
    if (message.java_outer_classname !== "") {
      writer.uint32(66).string(message.java_outer_classname);
    }
    if (message.java_multiple_files === true) {
      writer.uint32(80).bool(message.java_multiple_files);
    }
    if (message.java_generate_equals_and_hash === true) {
      writer.uint32(160).bool(message.java_generate_equals_and_hash);
    }
    if (message.java_string_check_utf8 === true) {
      writer.uint32(216).bool(message.java_string_check_utf8);
    }
    if (message.optimize_for !== 1) {
      writer.uint32(72).int32(message.optimize_for);
    }
    if (message.go_package !== "") {
      writer.uint32(90).string(message.go_package);
    }
    if (message.cc_generic_services === true) {
      writer.uint32(128).bool(message.cc_generic_services);
    }
    if (message.java_generic_services === true) {
      writer.uint32(136).bool(message.java_generic_services);
    }
    if (message.py_generic_services === true) {
      writer.uint32(144).bool(message.py_generic_services);
    }
    if (message.php_generic_services === true) {
      writer.uint32(336).bool(message.php_generic_services);
    }
    if (message.deprecated === true) {
      writer.uint32(184).bool(message.deprecated);
    }
    if (message.cc_enable_arenas === true) {
      writer.uint32(248).bool(message.cc_enable_arenas);
    }
    if (message.objc_class_prefix !== "") {
      writer.uint32(290).string(message.objc_class_prefix);
    }
    if (message.csharp_namespace !== "") {
      writer.uint32(298).string(message.csharp_namespace);
    }
    if (message.swift_prefix !== "") {
      writer.uint32(314).string(message.swift_prefix);
    }
    if (message.php_class_prefix !== "") {
      writer.uint32(322).string(message.php_class_prefix);
    }
    if (message.php_namespace !== "") {
      writer.uint32(330).string(message.php_namespace);
    }
    if (message.php_metadata_namespace !== "") {
      writer.uint32(354).string(message.php_metadata_namespace);
    }
    if (message.ruby_package !== "") {
      writer.uint32(362).string(message.ruby_package);
    }
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FileOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFileOptions } as FileOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.java_package = reader.string();
          break;
        case 8:
          message.java_outer_classname = reader.string();
          break;
        case 10:
          message.java_multiple_files = reader.bool();
          break;
        case 20:
          message.java_generate_equals_and_hash = reader.bool();
          break;
        case 27:
          message.java_string_check_utf8 = reader.bool();
          break;
        case 9:
          message.optimize_for = reader.int32() as any;
          break;
        case 11:
          message.go_package = reader.string();
          break;
        case 16:
          message.cc_generic_services = reader.bool();
          break;
        case 17:
          message.java_generic_services = reader.bool();
          break;
        case 18:
          message.py_generic_services = reader.bool();
          break;
        case 42:
          message.php_generic_services = reader.bool();
          break;
        case 23:
          message.deprecated = reader.bool();
          break;
        case 31:
          message.cc_enable_arenas = reader.bool();
          break;
        case 36:
          message.objc_class_prefix = reader.string();
          break;
        case 37:
          message.csharp_namespace = reader.string();
          break;
        case 39:
          message.swift_prefix = reader.string();
          break;
        case 40:
          message.php_class_prefix = reader.string();
          break;
        case 41:
          message.php_namespace = reader.string();
          break;
        case 44:
          message.php_metadata_namespace = reader.string();
          break;
        case 45:
          message.ruby_package = reader.string();
          break;
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FileOptions {
    const message = { ...baseFileOptions } as FileOptions;
    message.uninterpreted_option = [];
    if (object.java_package !== undefined && object.java_package !== null) {
      message.java_package = String(object.java_package);
    } else {
      message.java_package = "";
    }
    if (
      object.java_outer_classname !== undefined &&
      object.java_outer_classname !== null
    ) {
      message.java_outer_classname = String(object.java_outer_classname);
    } else {
      message.java_outer_classname = "";
    }
    if (
      object.java_multiple_files !== undefined &&
      object.java_multiple_files !== null
    ) {
      message.java_multiple_files = Boolean(object.java_multiple_files);
    } else {
      message.java_multiple_files = false;
    }
    if (
      object.java_generate_equals_and_hash !== undefined &&
      object.java_generate_equals_and_hash !== null
    ) {
      message.java_generate_equals_and_hash = Boolean(
        object.java_generate_equals_and_hash
      );
    } else {
      message.java_generate_equals_and_hash = false;
    }
    if (
      object.java_string_check_utf8 !== undefined &&
      object.java_string_check_utf8 !== null
    ) {
      message.java_string_check_utf8 = Boolean(object.java_string_check_utf8);
    } else {
      message.java_string_check_utf8 = false;
    }
    if (object.optimize_for !== undefined && object.optimize_for !== null) {
      message.optimize_for = fileOptions_OptimizeModeFromJSON(
        object.optimize_for
      );
    } else {
      message.optimize_for = 1;
    }
    if (object.go_package !== undefined && object.go_package !== null) {
      message.go_package = String(object.go_package);
    } else {
      message.go_package = "";
    }
    if (
      object.cc_generic_services !== undefined &&
      object.cc_generic_services !== null
    ) {
      message.cc_generic_services = Boolean(object.cc_generic_services);
    } else {
      message.cc_generic_services = false;
    }
    if (
      object.java_generic_services !== undefined &&
      object.java_generic_services !== null
    ) {
      message.java_generic_services = Boolean(object.java_generic_services);
    } else {
      message.java_generic_services = false;
    }
    if (
      object.py_generic_services !== undefined &&
      object.py_generic_services !== null
    ) {
      message.py_generic_services = Boolean(object.py_generic_services);
    } else {
      message.py_generic_services = false;
    }
    if (
      object.php_generic_services !== undefined &&
      object.php_generic_services !== null
    ) {
      message.php_generic_services = Boolean(object.php_generic_services);
    } else {
      message.php_generic_services = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (
      object.cc_enable_arenas !== undefined &&
      object.cc_enable_arenas !== null
    ) {
      message.cc_enable_arenas = Boolean(object.cc_enable_arenas);
    } else {
      message.cc_enable_arenas = false;
    }
    if (
      object.objc_class_prefix !== undefined &&
      object.objc_class_prefix !== null
    ) {
      message.objc_class_prefix = String(object.objc_class_prefix);
    } else {
      message.objc_class_prefix = "";
    }
    if (
      object.csharp_namespace !== undefined &&
      object.csharp_namespace !== null
    ) {
      message.csharp_namespace = String(object.csharp_namespace);
    } else {
      message.csharp_namespace = "";
    }
    if (object.swift_prefix !== undefined && object.swift_prefix !== null) {
      message.swift_prefix = String(object.swift_prefix);
    } else {
      message.swift_prefix = "";
    }
    if (
      object.php_class_prefix !== undefined &&
      object.php_class_prefix !== null
    ) {
      message.php_class_prefix = String(object.php_class_prefix);
    } else {
      message.php_class_prefix = "";
    }
    if (object.php_namespace !== undefined && object.php_namespace !== null) {
      message.php_namespace = String(object.php_namespace);
    } else {
      message.php_namespace = "";
    }
    if (
      object.php_metadata_namespace !== undefined &&
      object.php_metadata_namespace !== null
    ) {
      message.php_metadata_namespace = String(object.php_metadata_namespace);
    } else {
      message.php_metadata_namespace = "";
    }
    if (object.ruby_package !== undefined && object.ruby_package !== null) {
      message.ruby_package = String(object.ruby_package);
    } else {
      message.ruby_package = "";
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: FileOptions): unknown {
    const obj: any = {};
    message.java_package !== undefined &&
      (obj.java_package = message.java_package);
    message.java_outer_classname !== undefined &&
      (obj.java_outer_classname = message.java_outer_classname);
    message.java_multiple_files !== undefined &&
      (obj.java_multiple_files = message.java_multiple_files);
    message.java_generate_equals_and_hash !== undefined &&
      (obj.java_generate_equals_and_hash =
        message.java_generate_equals_and_hash);
    message.java_string_check_utf8 !== undefined &&
      (obj.java_string_check_utf8 = message.java_string_check_utf8);
    message.optimize_for !== undefined &&
      (obj.optimize_for = fileOptions_OptimizeModeToJSON(message.optimize_for));
    message.go_package !== undefined && (obj.go_package = message.go_package);
    message.cc_generic_services !== undefined &&
      (obj.cc_generic_services = message.cc_generic_services);
    message.java_generic_services !== undefined &&
      (obj.java_generic_services = message.java_generic_services);
    message.py_generic_services !== undefined &&
      (obj.py_generic_services = message.py_generic_services);
    message.php_generic_services !== undefined &&
      (obj.php_generic_services = message.php_generic_services);
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    message.cc_enable_arenas !== undefined &&
      (obj.cc_enable_arenas = message.cc_enable_arenas);
    message.objc_class_prefix !== undefined &&
      (obj.objc_class_prefix = message.objc_class_prefix);
    message.csharp_namespace !== undefined &&
      (obj.csharp_namespace = message.csharp_namespace);
    message.swift_prefix !== undefined &&
      (obj.swift_prefix = message.swift_prefix);
    message.php_class_prefix !== undefined &&
      (obj.php_class_prefix = message.php_class_prefix);
    message.php_namespace !== undefined &&
      (obj.php_namespace = message.php_namespace);
    message.php_metadata_namespace !== undefined &&
      (obj.php_metadata_namespace = message.php_metadata_namespace);
    message.ruby_package !== undefined &&
      (obj.ruby_package = message.ruby_package);
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<FileOptions>): FileOptions {
    const message = { ...baseFileOptions } as FileOptions;
    message.uninterpreted_option = [];
    if (object.java_package !== undefined && object.java_package !== null) {
      message.java_package = object.java_package;
    } else {
      message.java_package = "";
    }
    if (
      object.java_outer_classname !== undefined &&
      object.java_outer_classname !== null
    ) {
      message.java_outer_classname = object.java_outer_classname;
    } else {
      message.java_outer_classname = "";
    }
    if (
      object.java_multiple_files !== undefined &&
      object.java_multiple_files !== null
    ) {
      message.java_multiple_files = object.java_multiple_files;
    } else {
      message.java_multiple_files = false;
    }
    if (
      object.java_generate_equals_and_hash !== undefined &&
      object.java_generate_equals_and_hash !== null
    ) {
      message.java_generate_equals_and_hash =
        object.java_generate_equals_and_hash;
    } else {
      message.java_generate_equals_and_hash = false;
    }
    if (
      object.java_string_check_utf8 !== undefined &&
      object.java_string_check_utf8 !== null
    ) {
      message.java_string_check_utf8 = object.java_string_check_utf8;
    } else {
      message.java_string_check_utf8 = false;
    }
    if (object.optimize_for !== undefined && object.optimize_for !== null) {
      message.optimize_for = object.optimize_for;
    } else {
      message.optimize_for = 1;
    }
    if (object.go_package !== undefined && object.go_package !== null) {
      message.go_package = object.go_package;
    } else {
      message.go_package = "";
    }
    if (
      object.cc_generic_services !== undefined &&
      object.cc_generic_services !== null
    ) {
      message.cc_generic_services = object.cc_generic_services;
    } else {
      message.cc_generic_services = false;
    }
    if (
      object.java_generic_services !== undefined &&
      object.java_generic_services !== null
    ) {
      message.java_generic_services = object.java_generic_services;
    } else {
      message.java_generic_services = false;
    }
    if (
      object.py_generic_services !== undefined &&
      object.py_generic_services !== null
    ) {
      message.py_generic_services = object.py_generic_services;
    } else {
      message.py_generic_services = false;
    }
    if (
      object.php_generic_services !== undefined &&
      object.php_generic_services !== null
    ) {
      message.php_generic_services = object.php_generic_services;
    } else {
      message.php_generic_services = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (
      object.cc_enable_arenas !== undefined &&
      object.cc_enable_arenas !== null
    ) {
      message.cc_enable_arenas = object.cc_enable_arenas;
    } else {
      message.cc_enable_arenas = false;
    }
    if (
      object.objc_class_prefix !== undefined &&
      object.objc_class_prefix !== null
    ) {
      message.objc_class_prefix = object.objc_class_prefix;
    } else {
      message.objc_class_prefix = "";
    }
    if (
      object.csharp_namespace !== undefined &&
      object.csharp_namespace !== null
    ) {
      message.csharp_namespace = object.csharp_namespace;
    } else {
      message.csharp_namespace = "";
    }
    if (object.swift_prefix !== undefined && object.swift_prefix !== null) {
      message.swift_prefix = object.swift_prefix;
    } else {
      message.swift_prefix = "";
    }
    if (
      object.php_class_prefix !== undefined &&
      object.php_class_prefix !== null
    ) {
      message.php_class_prefix = object.php_class_prefix;
    } else {
      message.php_class_prefix = "";
    }
    if (object.php_namespace !== undefined && object.php_namespace !== null) {
      message.php_namespace = object.php_namespace;
    } else {
      message.php_namespace = "";
    }
    if (
      object.php_metadata_namespace !== undefined &&
      object.php_metadata_namespace !== null
    ) {
      message.php_metadata_namespace = object.php_metadata_namespace;
    } else {
      message.php_metadata_namespace = "";
    }
    if (object.ruby_package !== undefined && object.ruby_package !== null) {
      message.ruby_package = object.ruby_package;
    } else {
      message.ruby_package = "";
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMessageOptions: object = {
  message_set_wire_format: false,
  no_standard_descriptor_accessor: false,
  deprecated: false,
  map_entry: false,
};

export const MessageOptions = {
  encode(message: MessageOptions, writer: Writer = Writer.create()): Writer {
    if (message.message_set_wire_format === true) {
      writer.uint32(8).bool(message.message_set_wire_format);
    }
    if (message.no_standard_descriptor_accessor === true) {
      writer.uint32(16).bool(message.no_standard_descriptor_accessor);
    }
    if (message.deprecated === true) {
      writer.uint32(24).bool(message.deprecated);
    }
    if (message.map_entry === true) {
      writer.uint32(56).bool(message.map_entry);
    }
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MessageOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMessageOptions } as MessageOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.message_set_wire_format = reader.bool();
          break;
        case 2:
          message.no_standard_descriptor_accessor = reader.bool();
          break;
        case 3:
          message.deprecated = reader.bool();
          break;
        case 7:
          message.map_entry = reader.bool();
          break;
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MessageOptions {
    const message = { ...baseMessageOptions } as MessageOptions;
    message.uninterpreted_option = [];
    if (
      object.message_set_wire_format !== undefined &&
      object.message_set_wire_format !== null
    ) {
      message.message_set_wire_format = Boolean(object.message_set_wire_format);
    } else {
      message.message_set_wire_format = false;
    }
    if (
      object.no_standard_descriptor_accessor !== undefined &&
      object.no_standard_descriptor_accessor !== null
    ) {
      message.no_standard_descriptor_accessor = Boolean(
        object.no_standard_descriptor_accessor
      );
    } else {
      message.no_standard_descriptor_accessor = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (object.map_entry !== undefined && object.map_entry !== null) {
      message.map_entry = Boolean(object.map_entry);
    } else {
      message.map_entry = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MessageOptions): unknown {
    const obj: any = {};
    message.message_set_wire_format !== undefined &&
      (obj.message_set_wire_format = message.message_set_wire_format);
    message.no_standard_descriptor_accessor !== undefined &&
      (obj.no_standard_descriptor_accessor =
        message.no_standard_descriptor_accessor);
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    message.map_entry !== undefined && (obj.map_entry = message.map_entry);
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MessageOptions>): MessageOptions {
    const message = { ...baseMessageOptions } as MessageOptions;
    message.uninterpreted_option = [];
    if (
      object.message_set_wire_format !== undefined &&
      object.message_set_wire_format !== null
    ) {
      message.message_set_wire_format = object.message_set_wire_format;
    } else {
      message.message_set_wire_format = false;
    }
    if (
      object.no_standard_descriptor_accessor !== undefined &&
      object.no_standard_descriptor_accessor !== null
    ) {
      message.no_standard_descriptor_accessor =
        object.no_standard_descriptor_accessor;
    } else {
      message.no_standard_descriptor_accessor = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (object.map_entry !== undefined && object.map_entry !== null) {
      message.map_entry = object.map_entry;
    } else {
      message.map_entry = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseFieldOptions: object = {
  ctype: 0,
  packed: false,
  jstype: 0,
  lazy: false,
  deprecated: false,
  weak: false,
};

export const FieldOptions = {
  encode(message: FieldOptions, writer: Writer = Writer.create()): Writer {
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
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): FieldOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseFieldOptions } as FieldOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.ctype = reader.int32() as any;
          break;
        case 2:
          message.packed = reader.bool();
          break;
        case 6:
          message.jstype = reader.int32() as any;
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
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): FieldOptions {
    const message = { ...baseFieldOptions } as FieldOptions;
    message.uninterpreted_option = [];
    if (object.ctype !== undefined && object.ctype !== null) {
      message.ctype = fieldOptions_CTypeFromJSON(object.ctype);
    } else {
      message.ctype = 0;
    }
    if (object.packed !== undefined && object.packed !== null) {
      message.packed = Boolean(object.packed);
    } else {
      message.packed = false;
    }
    if (object.jstype !== undefined && object.jstype !== null) {
      message.jstype = fieldOptions_JSTypeFromJSON(object.jstype);
    } else {
      message.jstype = 0;
    }
    if (object.lazy !== undefined && object.lazy !== null) {
      message.lazy = Boolean(object.lazy);
    } else {
      message.lazy = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (object.weak !== undefined && object.weak !== null) {
      message.weak = Boolean(object.weak);
    } else {
      message.weak = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: FieldOptions): unknown {
    const obj: any = {};
    message.ctype !== undefined &&
      (obj.ctype = fieldOptions_CTypeToJSON(message.ctype));
    message.packed !== undefined && (obj.packed = message.packed);
    message.jstype !== undefined &&
      (obj.jstype = fieldOptions_JSTypeToJSON(message.jstype));
    message.lazy !== undefined && (obj.lazy = message.lazy);
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    message.weak !== undefined && (obj.weak = message.weak);
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<FieldOptions>): FieldOptions {
    const message = { ...baseFieldOptions } as FieldOptions;
    message.uninterpreted_option = [];
    if (object.ctype !== undefined && object.ctype !== null) {
      message.ctype = object.ctype;
    } else {
      message.ctype = 0;
    }
    if (object.packed !== undefined && object.packed !== null) {
      message.packed = object.packed;
    } else {
      message.packed = false;
    }
    if (object.jstype !== undefined && object.jstype !== null) {
      message.jstype = object.jstype;
    } else {
      message.jstype = 0;
    }
    if (object.lazy !== undefined && object.lazy !== null) {
      message.lazy = object.lazy;
    } else {
      message.lazy = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (object.weak !== undefined && object.weak !== null) {
      message.weak = object.weak;
    } else {
      message.weak = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseOneofOptions: object = {};

export const OneofOptions = {
  encode(message: OneofOptions, writer: Writer = Writer.create()): Writer {
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): OneofOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseOneofOptions } as OneofOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): OneofOptions {
    const message = { ...baseOneofOptions } as OneofOptions;
    message.uninterpreted_option = [];
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: OneofOptions): unknown {
    const obj: any = {};
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<OneofOptions>): OneofOptions {
    const message = { ...baseOneofOptions } as OneofOptions;
    message.uninterpreted_option = [];
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseEnumOptions: object = { allow_alias: false, deprecated: false };

export const EnumOptions = {
  encode(message: EnumOptions, writer: Writer = Writer.create()): Writer {
    if (message.allow_alias === true) {
      writer.uint32(16).bool(message.allow_alias);
    }
    if (message.deprecated === true) {
      writer.uint32(24).bool(message.deprecated);
    }
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): EnumOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEnumOptions } as EnumOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 2:
          message.allow_alias = reader.bool();
          break;
        case 3:
          message.deprecated = reader.bool();
          break;
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): EnumOptions {
    const message = { ...baseEnumOptions } as EnumOptions;
    message.uninterpreted_option = [];
    if (object.allow_alias !== undefined && object.allow_alias !== null) {
      message.allow_alias = Boolean(object.allow_alias);
    } else {
      message.allow_alias = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: EnumOptions): unknown {
    const obj: any = {};
    message.allow_alias !== undefined &&
      (obj.allow_alias = message.allow_alias);
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<EnumOptions>): EnumOptions {
    const message = { ...baseEnumOptions } as EnumOptions;
    message.uninterpreted_option = [];
    if (object.allow_alias !== undefined && object.allow_alias !== null) {
      message.allow_alias = object.allow_alias;
    } else {
      message.allow_alias = false;
    }
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseEnumValueOptions: object = { deprecated: false };

export const EnumValueOptions = {
  encode(message: EnumValueOptions, writer: Writer = Writer.create()): Writer {
    if (message.deprecated === true) {
      writer.uint32(8).bool(message.deprecated);
    }
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): EnumValueOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseEnumValueOptions } as EnumValueOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.deprecated = reader.bool();
          break;
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): EnumValueOptions {
    const message = { ...baseEnumValueOptions } as EnumValueOptions;
    message.uninterpreted_option = [];
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: EnumValueOptions): unknown {
    const obj: any = {};
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<EnumValueOptions>): EnumValueOptions {
    const message = { ...baseEnumValueOptions } as EnumValueOptions;
    message.uninterpreted_option = [];
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseServiceOptions: object = { deprecated: false };

export const ServiceOptions = {
  encode(message: ServiceOptions, writer: Writer = Writer.create()): Writer {
    if (message.deprecated === true) {
      writer.uint32(264).bool(message.deprecated);
    }
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): ServiceOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseServiceOptions } as ServiceOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 33:
          message.deprecated = reader.bool();
          break;
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): ServiceOptions {
    const message = { ...baseServiceOptions } as ServiceOptions;
    message.uninterpreted_option = [];
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: ServiceOptions): unknown {
    const obj: any = {};
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<ServiceOptions>): ServiceOptions {
    const message = { ...baseServiceOptions } as ServiceOptions;
    message.uninterpreted_option = [];
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseMethodOptions: object = { deprecated: false, idempotency_level: 0 };

export const MethodOptions = {
  encode(message: MethodOptions, writer: Writer = Writer.create()): Writer {
    if (message.deprecated === true) {
      writer.uint32(264).bool(message.deprecated);
    }
    if (message.idempotency_level !== 0) {
      writer.uint32(272).int32(message.idempotency_level);
    }
    for (const v of message.uninterpreted_option) {
      UninterpretedOption.encode(v!, writer.uint32(7994).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): MethodOptions {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseMethodOptions } as MethodOptions;
    message.uninterpreted_option = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 33:
          message.deprecated = reader.bool();
          break;
        case 34:
          message.idempotency_level = reader.int32() as any;
          break;
        case 999:
          message.uninterpreted_option.push(
            UninterpretedOption.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): MethodOptions {
    const message = { ...baseMethodOptions } as MethodOptions;
    message.uninterpreted_option = [];
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = Boolean(object.deprecated);
    } else {
      message.deprecated = false;
    }
    if (
      object.idempotency_level !== undefined &&
      object.idempotency_level !== null
    ) {
      message.idempotency_level = methodOptions_IdempotencyLevelFromJSON(
        object.idempotency_level
      );
    } else {
      message.idempotency_level = 0;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: MethodOptions): unknown {
    const obj: any = {};
    message.deprecated !== undefined && (obj.deprecated = message.deprecated);
    message.idempotency_level !== undefined &&
      (obj.idempotency_level = methodOptions_IdempotencyLevelToJSON(
        message.idempotency_level
      ));
    if (message.uninterpreted_option) {
      obj.uninterpreted_option = message.uninterpreted_option.map((e) =>
        e ? UninterpretedOption.toJSON(e) : undefined
      );
    } else {
      obj.uninterpreted_option = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<MethodOptions>): MethodOptions {
    const message = { ...baseMethodOptions } as MethodOptions;
    message.uninterpreted_option = [];
    if (object.deprecated !== undefined && object.deprecated !== null) {
      message.deprecated = object.deprecated;
    } else {
      message.deprecated = false;
    }
    if (
      object.idempotency_level !== undefined &&
      object.idempotency_level !== null
    ) {
      message.idempotency_level = object.idempotency_level;
    } else {
      message.idempotency_level = 0;
    }
    if (
      object.uninterpreted_option !== undefined &&
      object.uninterpreted_option !== null
    ) {
      for (const e of object.uninterpreted_option) {
        message.uninterpreted_option.push(UninterpretedOption.fromPartial(e));
      }
    }
    return message;
  },
};

const baseUninterpretedOption: object = {
  identifier_value: "",
  positive_int_value: 0,
  negative_int_value: 0,
  double_value: 0,
  aggregate_value: "",
};

export const UninterpretedOption = {
  encode(
    message: UninterpretedOption,
    writer: Writer = Writer.create()
  ): Writer {
    for (const v of message.name) {
      UninterpretedOption_NamePart.encode(
        v!,
        writer.uint32(18).fork()
      ).ldelim();
    }
    if (message.identifier_value !== "") {
      writer.uint32(26).string(message.identifier_value);
    }
    if (message.positive_int_value !== 0) {
      writer.uint32(32).uint64(message.positive_int_value);
    }
    if (message.negative_int_value !== 0) {
      writer.uint32(40).int64(message.negative_int_value);
    }
    if (message.double_value !== 0) {
      writer.uint32(49).double(message.double_value);
    }
    if (message.string_value.length !== 0) {
      writer.uint32(58).bytes(message.string_value);
    }
    if (message.aggregate_value !== "") {
      writer.uint32(66).string(message.aggregate_value);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): UninterpretedOption {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseUninterpretedOption } as UninterpretedOption;
    message.name = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 2:
          message.name.push(
            UninterpretedOption_NamePart.decode(reader, reader.uint32())
          );
          break;
        case 3:
          message.identifier_value = reader.string();
          break;
        case 4:
          message.positive_int_value = longToNumber(reader.uint64() as Long);
          break;
        case 5:
          message.negative_int_value = longToNumber(reader.int64() as Long);
          break;
        case 6:
          message.double_value = reader.double();
          break;
        case 7:
          message.string_value = reader.bytes();
          break;
        case 8:
          message.aggregate_value = reader.string();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): UninterpretedOption {
    const message = { ...baseUninterpretedOption } as UninterpretedOption;
    message.name = [];
    if (object.name !== undefined && object.name !== null) {
      for (const e of object.name) {
        message.name.push(UninterpretedOption_NamePart.fromJSON(e));
      }
    }
    if (
      object.identifier_value !== undefined &&
      object.identifier_value !== null
    ) {
      message.identifier_value = String(object.identifier_value);
    } else {
      message.identifier_value = "";
    }
    if (
      object.positive_int_value !== undefined &&
      object.positive_int_value !== null
    ) {
      message.positive_int_value = Number(object.positive_int_value);
    } else {
      message.positive_int_value = 0;
    }
    if (
      object.negative_int_value !== undefined &&
      object.negative_int_value !== null
    ) {
      message.negative_int_value = Number(object.negative_int_value);
    } else {
      message.negative_int_value = 0;
    }
    if (object.double_value !== undefined && object.double_value !== null) {
      message.double_value = Number(object.double_value);
    } else {
      message.double_value = 0;
    }
    if (object.string_value !== undefined && object.string_value !== null) {
      message.string_value = bytesFromBase64(object.string_value);
    }
    if (
      object.aggregate_value !== undefined &&
      object.aggregate_value !== null
    ) {
      message.aggregate_value = String(object.aggregate_value);
    } else {
      message.aggregate_value = "";
    }
    return message;
  },

  toJSON(message: UninterpretedOption): unknown {
    const obj: any = {};
    if (message.name) {
      obj.name = message.name.map((e) =>
        e ? UninterpretedOption_NamePart.toJSON(e) : undefined
      );
    } else {
      obj.name = [];
    }
    message.identifier_value !== undefined &&
      (obj.identifier_value = message.identifier_value);
    message.positive_int_value !== undefined &&
      (obj.positive_int_value = message.positive_int_value);
    message.negative_int_value !== undefined &&
      (obj.negative_int_value = message.negative_int_value);
    message.double_value !== undefined &&
      (obj.double_value = message.double_value);
    message.string_value !== undefined &&
      (obj.string_value = base64FromBytes(
        message.string_value !== undefined
          ? message.string_value
          : new Uint8Array()
      ));
    message.aggregate_value !== undefined &&
      (obj.aggregate_value = message.aggregate_value);
    return obj;
  },

  fromPartial(object: DeepPartial<UninterpretedOption>): UninterpretedOption {
    const message = { ...baseUninterpretedOption } as UninterpretedOption;
    message.name = [];
    if (object.name !== undefined && object.name !== null) {
      for (const e of object.name) {
        message.name.push(UninterpretedOption_NamePart.fromPartial(e));
      }
    }
    if (
      object.identifier_value !== undefined &&
      object.identifier_value !== null
    ) {
      message.identifier_value = object.identifier_value;
    } else {
      message.identifier_value = "";
    }
    if (
      object.positive_int_value !== undefined &&
      object.positive_int_value !== null
    ) {
      message.positive_int_value = object.positive_int_value;
    } else {
      message.positive_int_value = 0;
    }
    if (
      object.negative_int_value !== undefined &&
      object.negative_int_value !== null
    ) {
      message.negative_int_value = object.negative_int_value;
    } else {
      message.negative_int_value = 0;
    }
    if (object.double_value !== undefined && object.double_value !== null) {
      message.double_value = object.double_value;
    } else {
      message.double_value = 0;
    }
    if (object.string_value !== undefined && object.string_value !== null) {
      message.string_value = object.string_value;
    } else {
      message.string_value = new Uint8Array();
    }
    if (
      object.aggregate_value !== undefined &&
      object.aggregate_value !== null
    ) {
      message.aggregate_value = object.aggregate_value;
    } else {
      message.aggregate_value = "";
    }
    return message;
  },
};

const baseUninterpretedOption_NamePart: object = {
  name_part: "",
  is_extension: false,
};

export const UninterpretedOption_NamePart = {
  encode(
    message: UninterpretedOption_NamePart,
    writer: Writer = Writer.create()
  ): Writer {
    if (message.name_part !== "") {
      writer.uint32(10).string(message.name_part);
    }
    if (message.is_extension === true) {
      writer.uint32(16).bool(message.is_extension);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): UninterpretedOption_NamePart {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseUninterpretedOption_NamePart,
    } as UninterpretedOption_NamePart;
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.name_part = reader.string();
          break;
        case 2:
          message.is_extension = reader.bool();
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): UninterpretedOption_NamePart {
    const message = {
      ...baseUninterpretedOption_NamePart,
    } as UninterpretedOption_NamePart;
    if (object.name_part !== undefined && object.name_part !== null) {
      message.name_part = String(object.name_part);
    } else {
      message.name_part = "";
    }
    if (object.is_extension !== undefined && object.is_extension !== null) {
      message.is_extension = Boolean(object.is_extension);
    } else {
      message.is_extension = false;
    }
    return message;
  },

  toJSON(message: UninterpretedOption_NamePart): unknown {
    const obj: any = {};
    message.name_part !== undefined && (obj.name_part = message.name_part);
    message.is_extension !== undefined &&
      (obj.is_extension = message.is_extension);
    return obj;
  },

  fromPartial(
    object: DeepPartial<UninterpretedOption_NamePart>
  ): UninterpretedOption_NamePart {
    const message = {
      ...baseUninterpretedOption_NamePart,
    } as UninterpretedOption_NamePart;
    if (object.name_part !== undefined && object.name_part !== null) {
      message.name_part = object.name_part;
    } else {
      message.name_part = "";
    }
    if (object.is_extension !== undefined && object.is_extension !== null) {
      message.is_extension = object.is_extension;
    } else {
      message.is_extension = false;
    }
    return message;
  },
};

const baseSourceCodeInfo: object = {};

export const SourceCodeInfo = {
  encode(message: SourceCodeInfo, writer: Writer = Writer.create()): Writer {
    for (const v of message.location) {
      SourceCodeInfo_Location.encode(v!, writer.uint32(10).fork()).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SourceCodeInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseSourceCodeInfo } as SourceCodeInfo;
    message.location = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.location.push(
            SourceCodeInfo_Location.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SourceCodeInfo {
    const message = { ...baseSourceCodeInfo } as SourceCodeInfo;
    message.location = [];
    if (object.location !== undefined && object.location !== null) {
      for (const e of object.location) {
        message.location.push(SourceCodeInfo_Location.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: SourceCodeInfo): unknown {
    const obj: any = {};
    if (message.location) {
      obj.location = message.location.map((e) =>
        e ? SourceCodeInfo_Location.toJSON(e) : undefined
      );
    } else {
      obj.location = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<SourceCodeInfo>): SourceCodeInfo {
    const message = { ...baseSourceCodeInfo } as SourceCodeInfo;
    message.location = [];
    if (object.location !== undefined && object.location !== null) {
      for (const e of object.location) {
        message.location.push(SourceCodeInfo_Location.fromPartial(e));
      }
    }
    return message;
  },
};

const baseSourceCodeInfo_Location: object = {
  path: 0,
  span: 0,
  leading_comments: "",
  trailing_comments: "",
  leading_detached_comments: "",
};

export const SourceCodeInfo_Location = {
  encode(
    message: SourceCodeInfo_Location,
    writer: Writer = Writer.create()
  ): Writer {
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
    if (message.leading_comments !== "") {
      writer.uint32(26).string(message.leading_comments);
    }
    if (message.trailing_comments !== "") {
      writer.uint32(34).string(message.trailing_comments);
    }
    for (const v of message.leading_detached_comments) {
      writer.uint32(50).string(v!);
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): SourceCodeInfo_Location {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseSourceCodeInfo_Location,
    } as SourceCodeInfo_Location;
    message.path = [];
    message.span = [];
    message.leading_detached_comments = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.path.push(reader.int32());
            }
          } else {
            message.path.push(reader.int32());
          }
          break;
        case 2:
          if ((tag & 7) === 2) {
            const end2 = reader.uint32() + reader.pos;
            while (reader.pos < end2) {
              message.span.push(reader.int32());
            }
          } else {
            message.span.push(reader.int32());
          }
          break;
        case 3:
          message.leading_comments = reader.string();
          break;
        case 4:
          message.trailing_comments = reader.string();
          break;
        case 6:
          message.leading_detached_comments.push(reader.string());
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): SourceCodeInfo_Location {
    const message = {
      ...baseSourceCodeInfo_Location,
    } as SourceCodeInfo_Location;
    message.path = [];
    message.span = [];
    message.leading_detached_comments = [];
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
    if (
      object.leading_comments !== undefined &&
      object.leading_comments !== null
    ) {
      message.leading_comments = String(object.leading_comments);
    } else {
      message.leading_comments = "";
    }
    if (
      object.trailing_comments !== undefined &&
      object.trailing_comments !== null
    ) {
      message.trailing_comments = String(object.trailing_comments);
    } else {
      message.trailing_comments = "";
    }
    if (
      object.leading_detached_comments !== undefined &&
      object.leading_detached_comments !== null
    ) {
      for (const e of object.leading_detached_comments) {
        message.leading_detached_comments.push(String(e));
      }
    }
    return message;
  },

  toJSON(message: SourceCodeInfo_Location): unknown {
    const obj: any = {};
    if (message.path) {
      obj.path = message.path.map((e) => e);
    } else {
      obj.path = [];
    }
    if (message.span) {
      obj.span = message.span.map((e) => e);
    } else {
      obj.span = [];
    }
    message.leading_comments !== undefined &&
      (obj.leading_comments = message.leading_comments);
    message.trailing_comments !== undefined &&
      (obj.trailing_comments = message.trailing_comments);
    if (message.leading_detached_comments) {
      obj.leading_detached_comments = message.leading_detached_comments.map(
        (e) => e
      );
    } else {
      obj.leading_detached_comments = [];
    }
    return obj;
  },

  fromPartial(
    object: DeepPartial<SourceCodeInfo_Location>
  ): SourceCodeInfo_Location {
    const message = {
      ...baseSourceCodeInfo_Location,
    } as SourceCodeInfo_Location;
    message.path = [];
    message.span = [];
    message.leading_detached_comments = [];
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
    if (
      object.leading_comments !== undefined &&
      object.leading_comments !== null
    ) {
      message.leading_comments = object.leading_comments;
    } else {
      message.leading_comments = "";
    }
    if (
      object.trailing_comments !== undefined &&
      object.trailing_comments !== null
    ) {
      message.trailing_comments = object.trailing_comments;
    } else {
      message.trailing_comments = "";
    }
    if (
      object.leading_detached_comments !== undefined &&
      object.leading_detached_comments !== null
    ) {
      for (const e of object.leading_detached_comments) {
        message.leading_detached_comments.push(e);
      }
    }
    return message;
  },
};

const baseGeneratedCodeInfo: object = {};

export const GeneratedCodeInfo = {
  encode(message: GeneratedCodeInfo, writer: Writer = Writer.create()): Writer {
    for (const v of message.annotation) {
      GeneratedCodeInfo_Annotation.encode(
        v!,
        writer.uint32(10).fork()
      ).ldelim();
    }
    return writer;
  },

  decode(input: Reader | Uint8Array, length?: number): GeneratedCodeInfo {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = { ...baseGeneratedCodeInfo } as GeneratedCodeInfo;
    message.annotation = [];
    while (reader.pos < end) {
      const tag = reader.uint32();
      switch (tag >>> 3) {
        case 1:
          message.annotation.push(
            GeneratedCodeInfo_Annotation.decode(reader, reader.uint32())
          );
          break;
        default:
          reader.skipType(tag & 7);
          break;
      }
    }
    return message;
  },

  fromJSON(object: any): GeneratedCodeInfo {
    const message = { ...baseGeneratedCodeInfo } as GeneratedCodeInfo;
    message.annotation = [];
    if (object.annotation !== undefined && object.annotation !== null) {
      for (const e of object.annotation) {
        message.annotation.push(GeneratedCodeInfo_Annotation.fromJSON(e));
      }
    }
    return message;
  },

  toJSON(message: GeneratedCodeInfo): unknown {
    const obj: any = {};
    if (message.annotation) {
      obj.annotation = message.annotation.map((e) =>
        e ? GeneratedCodeInfo_Annotation.toJSON(e) : undefined
      );
    } else {
      obj.annotation = [];
    }
    return obj;
  },

  fromPartial(object: DeepPartial<GeneratedCodeInfo>): GeneratedCodeInfo {
    const message = { ...baseGeneratedCodeInfo } as GeneratedCodeInfo;
    message.annotation = [];
    if (object.annotation !== undefined && object.annotation !== null) {
      for (const e of object.annotation) {
        message.annotation.push(GeneratedCodeInfo_Annotation.fromPartial(e));
      }
    }
    return message;
  },
};

const baseGeneratedCodeInfo_Annotation: object = {
  path: 0,
  source_file: "",
  begin: 0,
  end: 0,
};

export const GeneratedCodeInfo_Annotation = {
  encode(
    message: GeneratedCodeInfo_Annotation,
    writer: Writer = Writer.create()
  ): Writer {
    writer.uint32(10).fork();
    for (const v of message.path) {
      writer.int32(v);
    }
    writer.ldelim();
    if (message.source_file !== "") {
      writer.uint32(18).string(message.source_file);
    }
    if (message.begin !== 0) {
      writer.uint32(24).int32(message.begin);
    }
    if (message.end !== 0) {
      writer.uint32(32).int32(message.end);
    }
    return writer;
  },

  decode(
    input: Reader | Uint8Array,
    length?: number
  ): GeneratedCodeInfo_Annotation {
    const reader = input instanceof Uint8Array ? new Reader(input) : input;
    let end = length === undefined ? reader.len : reader.pos + length;
    const message = {
      ...baseGeneratedCodeInfo_Annotation,
    } as GeneratedCodeInfo_Annotation;
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
          } else {
            message.path.push(reader.int32());
          }
          break;
        case 2:
          message.source_file = reader.string();
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

  fromJSON(object: any): GeneratedCodeInfo_Annotation {
    const message = {
      ...baseGeneratedCodeInfo_Annotation,
    } as GeneratedCodeInfo_Annotation;
    message.path = [];
    if (object.path !== undefined && object.path !== null) {
      for (const e of object.path) {
        message.path.push(Number(e));
      }
    }
    if (object.source_file !== undefined && object.source_file !== null) {
      message.source_file = String(object.source_file);
    } else {
      message.source_file = "";
    }
    if (object.begin !== undefined && object.begin !== null) {
      message.begin = Number(object.begin);
    } else {
      message.begin = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = Number(object.end);
    } else {
      message.end = 0;
    }
    return message;
  },

  toJSON(message: GeneratedCodeInfo_Annotation): unknown {
    const obj: any = {};
    if (message.path) {
      obj.path = message.path.map((e) => e);
    } else {
      obj.path = [];
    }
    message.source_file !== undefined &&
      (obj.source_file = message.source_file);
    message.begin !== undefined && (obj.begin = message.begin);
    message.end !== undefined && (obj.end = message.end);
    return obj;
  },

  fromPartial(
    object: DeepPartial<GeneratedCodeInfo_Annotation>
  ): GeneratedCodeInfo_Annotation {
    const message = {
      ...baseGeneratedCodeInfo_Annotation,
    } as GeneratedCodeInfo_Annotation;
    message.path = [];
    if (object.path !== undefined && object.path !== null) {
      for (const e of object.path) {
        message.path.push(e);
      }
    }
    if (object.source_file !== undefined && object.source_file !== null) {
      message.source_file = object.source_file;
    } else {
      message.source_file = "";
    }
    if (object.begin !== undefined && object.begin !== null) {
      message.begin = object.begin;
    } else {
      message.begin = 0;
    }
    if (object.end !== undefined && object.end !== null) {
      message.end = object.end;
    } else {
      message.end = 0;
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
