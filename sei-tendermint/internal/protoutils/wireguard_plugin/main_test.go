package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	sizedFieldNum        = 414126221
	maxCountFieldNum     = 414126218
	maxSizeFieldNum      = 414126219
	maxTotalSizeFieldNum = 414126220
)

// buildFieldOptions returns a FieldOptions whose unknown-field bytes carry
// the requested wireguard extensions at the configured numbers. Encoding the
// extensions this way avoids depending on the gogofaster-generated extension
// descriptors at test time — the plugin reads them via dynamic types
// regardless.
func buildFieldOptions(exts map[protowire.Number]uint32) *descriptorpb.FieldOptions {
	var bz []byte
	for fieldNum, value := range exts {
		bz = protowire.AppendTag(bz, fieldNum, protowire.VarintType)
		bz = protowire.AppendVarint(bz, uint64(value))
	}
	fo := &descriptorpb.FieldOptions{}
	fo.ProtoReflect().SetUnknown(bz)
	return fo
}

// wireguardFDP returns a FileDescriptorProto for a minimal wireguard.proto
// that declares the wireguard FieldOptions extensions. The plugin needs this
// in its descriptor set to resolve them.
func wireguardFDP() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:       proto.String("wireguard/wireguard.proto"),
		Package:    proto.String("wireguard"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/descriptor.proto"},
		Extension: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("sized"),
				Number:   proto.Int32(sizedFieldNum),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Extendee: proto.String(".google.protobuf.MessageOptions"),
				JsonName: proto.String("sized"),
			},
			{
				Name:     proto.String("max_count"),
				Number:   proto.Int32(maxCountFieldNum),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Extendee: proto.String(".google.protobuf.FieldOptions"),
				JsonName: proto.String("maxCount"),
			},
			{
				Name:     proto.String("max_size"),
				Number:   proto.Int32(maxSizeFieldNum),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Extendee: proto.String(".google.protobuf.FieldOptions"),
				JsonName: proto.String("maxSize"),
			},
			{
				Name:     proto.String("max_total_size"),
				Number:   proto.Int32(maxTotalSizeFieldNum),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Extendee: proto.String(".google.protobuf.FieldOptions"),
				JsonName: proto.String("maxTotalSize"),
			},
		},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test/wireguard"),
		},
	}
}

func buildMessageOptionsSized() *descriptorpb.MessageOptions {
	bz := protowire.AppendTag(nil, sizedFieldNum, protowire.VarintType)
	bz = protowire.AppendVarint(bz, 1)
	opts := &descriptorpb.MessageOptions{}
	opts.ProtoReflect().SetUnknown(bz)
	return opts
}

// descriptorProtoFDP returns the FileDescriptorProto for the well-known
// google/protobuf/descriptor.proto. It is required as a transitive
// dependency by wireguard.proto.
func descriptorProtoFDP(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()
	fd, err := protoregistry.GlobalFiles.FindFileByPath("google/protobuf/descriptor.proto")
	require.NoError(t, err)
	return protodesc.ToFileDescriptorProto(fd)
}

// runPlugin builds an in-process protogen.Plugin from the given files,
// invokes run, and returns the generated content keyed by file name.
func runPlugin(t *testing.T, files []*descriptorpb.FileDescriptorProto, fileToGenerate string, params string) (map[string]string, error) {
	t.Helper()
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{fileToGenerate},
		ProtoFile:      files,
		Parameter:      proto.String(params),
	}
	// Reset package-level flag values between tests. protogen.New calls
	// ParamFunc for each `key=value` in Parameter; the bound vars below
	// are updated through the existing flags FlagSet.
	*moduleFlag = ""
	plug, err := protogen.Options{ParamFunc: flags.Set}.New(req)
	if err != nil {
		return nil, err
	}
	if err := run(plug); err != nil {
		return nil, err
	}
	resp := plug.Response()
	out := map[string]string{}
	for _, f := range resp.File {
		out[f.GetName()] = f.GetContent()
	}
	return out, nil
}

// TestPlugin_AutoDescentAndMaxCount mirrors the case in the PR review:
//
//	message A {
//	  repeated int32 field_1 = 1 [(wireguard.max_count) = 5];
//	  B        field_2 = 2;             // singular — should auto-descend
//	}
//	message B {
//	  repeated int32 y = 1 [(wireguard.max_count) = 10];
//	}
//
// Expected output: init() registers A with field_1 capped and field_2
// descending into B; and registers B with y capped.

// TestPlugin_RejectsMaxCountZero verifies that (wireguard.max_count) = 0
// is rejected at codegen, since the runtime treats MaxCount == 0 as "no
// cap" and silently accepting the annotation would mislead.
func TestPlugin_RejectsMaxCountZero(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("xs"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 0}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msg},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	var positiveErr errMustBePositive
	require.ErrorAs(t, err, &positiveErr)
	require.Equal(t, "max_count", positiveErr.Rule)
}

func TestPlugin_RejectsMaxSizeZero(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("payload"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxSizeFieldNum: 0}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msg},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	var positiveErr errMustBePositive
	require.ErrorAs(t, err, &positiveErr)
	require.Equal(t, "max_size", positiveErr.Rule)
}

func TestPlugin_RejectsMaxTotalSizeZero(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("payload"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxTotalSizeFieldNum: 0}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msg},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	var positiveErr errMustBePositive
	require.ErrorAs(t, err, &positiveErr)
	require.Equal(t, "max_total_size", positiveErr.Rule)
}

func TestPlugin_RejectsSizeRulesOnPackedScalarField(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("xs"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxSizeFieldNum: 8}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msg},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	require.ErrorIs(t, err, errSizeRulesRequireSizedFieldType)
}

func TestPlugin_SizedRejectsUnboundedBytesField(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name:    proto.String("A"),
		Options: buildMessageOptionsSized(),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("payload"),
				Number: proto.Int32(1),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msg},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	require.ErrorIs(t, err, errSizedFieldNeedsSizeOrSizedNest)
}

func TestPlugin_SizedRejectsRepeatedFieldWithoutMaxCount(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name:    proto.String("A"),
		Options: buildMessageOptionsSized(),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("xs"),
				Number: proto.Int32(1),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msg},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	require.ErrorIs(t, err, errSizedFieldMissingMaxCount)
}

func TestPlugin_SizedRejectsUnsizedNestedMessageWithoutFieldSize(t *testing.T) {
	msgInner := &descriptorpb.DescriptorProto{
		Name: proto.String("Inner"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("payload"),
				Number: proto.Int32(1),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	msgOuter := &descriptorpb.DescriptorProto{
		Name:    proto.String("Outer"),
		Options: buildMessageOptionsSized(),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("inner"),
				Number:   proto.Int32(1),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.Inner"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgOuter, msgInner},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	require.ErrorIs(t, err, errSizedFieldNeedsSizeOrSizedNest)
}

func TestPlugin_SizedRepeatedMessageRequiresSizeOrSizedNested(t *testing.T) {
	msgInner := &descriptorpb.DescriptorProto{
		Name: proto.String("Inner"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("xs"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 2}),
			},
		},
	}
	msgOuter := &descriptorpb.DescriptorProto{
		Name:    proto.String("Outer"),
		Options: buildMessageOptionsSized(),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("inners"),
				Number:   proto.Int32(1),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.Inner"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options:  buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 2}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgOuter, msgInner},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	_, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.Error(t, err)
	require.ErrorIs(t, err, errSizedRepeatedFieldNeedsSizeOrSizedNest)
}

func init() {
	// Touch protoregistry to make sure descriptor.proto is available.
	if _, err := protoregistry.GlobalFiles.FindFileByPath("google/protobuf/descriptor.proto"); err != nil {
		panic("descriptor.proto not registered: " + err.Error())
	}
}
