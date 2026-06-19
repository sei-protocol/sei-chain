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
func TestPlugin_AutoDescentAndMaxCount(t *testing.T) {
	msgA := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("field_1"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 5}),
			},
			{
				Name:     proto.String("field_2"),
				Number:   proto.Int32(2),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.B"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	msgB := &descriptorpb.DescriptorProto{
		Name: proto.String("B"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("y"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 10}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgA, msgB},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}

	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	out, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.NoError(t, err)

	content := out["test/test.wireguard.go"]
	require.NotEmpty(t, content, "expected generated wireguard.go for test.proto; got files %v", keys(out))

	// A: field_1 capped, field_2 nested into B.
	require.Contains(t, content, "func init() {")
	require.Contains(t, content, "MustRegister[*A](runtime.Schema{")
	require.Contains(t, content, `1: {MaxCount: 5}`)
	require.Contains(t, content, `2: {Nested: `)
	require.Contains(t, content, "Some(reflect.TypeFor[*B]())")

	// B: y capped.
	require.Contains(t, content, "MustRegister[*B](runtime.Schema{")
	require.Contains(t, content, `1: {MaxCount: 10}`)
}

// TestPlugin_NoDescentWhenTargetHasNoSchema confirms the plugin skips
// emitting a Nested rule when the field's message type has no annotations
// anywhere in its subtree — and skips emitting a Schema for the parent
// entirely if it has no annotations of its own and no annotated target.
func TestPlugin_NoDescentWhenTargetHasNoSchema(t *testing.T) {
	// Parent has a message-typed field pointing at unrelated C, which has
	// no annotations. Parent has no own annotations either. No Schema
	// should be emitted for Parent or C.
	msgParent := &descriptorpb.DescriptorProto{
		Name: proto.String("Parent"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("c"),
				Number:   proto.Int32(1),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.C"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	msgC := &descriptorpb.DescriptorProto{
		Name: proto.String("C"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("x"), Number: proto.Int32(1), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgParent, msgC},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}

	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	out, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.NoError(t, err)

	// Nothing in the closure → no file emitted at all.
	require.NotContains(t, keys(out), "test/test.wireguard.go", "no Schema should be emitted when no annotations reach")
}

// TestPlugin_OneofDescentUsesConcreteFieldNumber verifies that a oneof
// variant pointing at an annotated target emits the concrete wire field
// number directly in the generated schema.
func TestPlugin_OneofDescentUsesConcreteFieldNumber(t *testing.T) {
	// Outer has a oneof "sum" with one variant: variant_a of type Inner.
	// Inner has a max_count field.
	outerOneof := []*descriptorpb.OneofDescriptorProto{{Name: proto.String("sum")}}
	msgOuter := &descriptorpb.DescriptorProto{
		Name:      proto.String("Outer"),
		OneofDecl: outerOneof,
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:       proto.String("variant_a"),
				Number:     proto.Int32(1),
				Type:       descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName:   proto.String(".test.Inner"),
				Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				OneofIndex: proto.Int32(0),
			},
		},
	}
	msgInner := &descriptorpb.DescriptorProto{
		Name: proto.String("Inner"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("items"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 3}),
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
	out, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.NoError(t, err)

	content := out["test/test.wireguard.go"]
	require.NotEmpty(t, content)
	require.Contains(t, content, `1: {Nested: `)
}

// TestPlugin_RepeatedMessageWithMaxCountAndDescent verifies that a
// repeated message field carrying both (wireguard.max_count) and a target
// type in the closure produces a rule with both MaxCount and Nested set.
func TestPlugin_RepeatedMessageWithMaxCountAndDescent(t *testing.T) {
	msgA := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("bs"),
				Number:   proto.Int32(1),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.B"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options:  buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 7}),
			},
		},
	}
	msgB := &descriptorpb.DescriptorProto{
		Name: proto.String("B"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("ys"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 11}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgA, msgB},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}
	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	out, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.NoError(t, err)

	content := out["test/test.wireguard.go"]
	require.NotEmpty(t, content)
	// A.bs has both MaxCount and Nested rules.
	require.Contains(t, content, `1: {MaxCount: 7, Nested: `)
	require.Contains(t, content, "Some(reflect.TypeFor[*B]())")
	require.Contains(t, content, `1: {MaxCount: 11}`)
}

func TestPlugin_EmitsSizeRules(t *testing.T) {
	msgA := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("payload"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxSizeFieldNum: 8, maxTotalSizeFieldNum: 11}),
			},
			{
				Name:     proto.String("child"),
				Number:   proto.Int32(2),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".test.B"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	msgB := &descriptorpb.DescriptorProto{
		Name: proto.String("B"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("name"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxSizeFieldNum: 4}),
			},
		},
	}
	testFile := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("test.proto"),
		Package:     proto.String("test"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgA, msgB},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/test;testpb"),
		},
	}

	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), testFile}
	out, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.NoError(t, err)

	content := out["test/test.wireguard.go"]
	require.NotEmpty(t, content)
	require.Contains(t, content, `1: {MaxSize: 8, MaxTotalSize: 11}`)
	require.Contains(t, content, `2: {Nested: `)
	require.Contains(t, content, "MustRegister[*B](runtime.Schema{")
	require.Contains(t, content, `1: {MaxSize: 4}`)
}

// TestPlugin_CrossFileReference verifies that a field in one file whose
// target type lives in a different file emits a qualified reference to
// the other package's Go type inside reflect.TypeFor.
func TestPlugin_CrossFileReference(t *testing.T) {
	// File b.proto: message B with a max_count field.
	msgB := &descriptorpb.DescriptorProto{
		Name: proto.String("B"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("items"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 3}),
			},
		},
	}
	fileB := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("b.proto"),
		Package:     proto.String("testb"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgB},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/testb;testbpb"),
		},
	}
	// File a.proto: message A with a singular B field. A must transitively
	// land in the closure because its field's target (B) is in the closure.
	msgA := &descriptorpb.DescriptorProto{
		Name: proto.String("A"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String("b"),
				Number:   proto.Int32(1),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
				TypeName: proto.String(".testb.B"),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			},
		},
	}
	fileA := &descriptorpb.FileDescriptorProto{
		Name:        proto.String("a.proto"),
		Package:     proto.String("testa"),
		Syntax:      proto.String("proto3"),
		Dependency:  []string{"b.proto", "wireguard/wireguard.proto"},
		MessageType: []*descriptorpb.DescriptorProto{msgA},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/example/testa;testapb"),
		},
	}

	files := []*descriptorpb.FileDescriptorProto{descriptorProtoFDP(t), wireguardFDP(), fileB, fileA}
	// Generate code for both a.proto and b.proto. runPlugin only sets one
	// in FileToGenerate, so build the request manually here.
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"a.proto", "b.proto"},
		ProtoFile:      files,
		Parameter:      proto.String("module=github.com/example"),
	}
	*moduleFlag = ""
	plug, err := protogen.Options{ParamFunc: flags.Set}.New(req)
	require.NoError(t, err)
	require.NoError(t, run(plug))
	resp := plug.Response()
	out := map[string]string{}
	for _, f := range resp.File {
		out[f.GetName()] = f.GetContent()
	}

	contentA := out["testa/a.wireguard.go"]
	contentB := out["testb/b.wireguard.go"]
	require.NotEmpty(t, contentA, "a.wireguard.go expected; got files %v", keys(out))
	require.NotEmpty(t, contentB, "b.wireguard.go expected")

	// a.wireguard.go imports the testb package and references B via the
	// import alias inside reflect.TypeFor.
	require.Contains(t, contentA, `"github.com/example/testb"`)
	require.Contains(t, contentA, "reflect.TypeFor[*testb.B]()")
	// b.wireguard.go registers B.
	require.Contains(t, contentB, "MustRegister[*B](runtime.Schema{")
	require.Contains(t, contentB, `1: {MaxCount: 3}`)
}

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
	require.Contains(t, err.Error(), "max_count")
	require.Contains(t, err.Error(), "> 0")
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
	require.Contains(t, err.Error(), "max_size")
	require.Contains(t, err.Error(), "> 0")
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
	require.Contains(t, err.Error(), "max_total_size")
	require.Contains(t, err.Error(), "> 0")
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
	require.Contains(t, err.Error(), "string, bytes, or message field")
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
	require.Contains(t, err.Error(), "sized string/bytes/message fields need")
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
	require.Contains(t, err.Error(), "must have (wireguard.max_count)")
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
	require.Contains(t, err.Error(), "sized string/bytes/message fields need")
}

func TestPlugin_SizedAcceptsSizedNestedMessage(t *testing.T) {
	msgInner := &descriptorpb.DescriptorProto{
		Name:    proto.String("Inner"),
		Options: buildMessageOptionsSized(),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:    proto.String("payload"),
				Number:  proto.Int32(1),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxSizeFieldNum: 8}),
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
	require.NoError(t, err)
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
	require.Contains(t, err.Error(), "repeated sized fields need")
}

func TestPlugin_SizedRepeatedMessageAcceptsMaxTotalSize(t *testing.T) {
	msgInner := &descriptorpb.DescriptorProto{Name: proto.String("Inner")}
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
				Options:  buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 2, maxTotalSizeFieldNum: 64}),
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
	require.NoError(t, err)
}

// TestPlugin_Proto3OptionalDoesNotEmitWrapperType verifies the fix for the
// synthetic-oneof bug: a proto3 optional field (which the compiler encodes as
// a FieldDescriptorProto with Proto3Optional=true and a synthetic oneof) must
// not produce a nonexistent Foo_Bar wrapper reference in the output.
// Before the fix the plugin treated the synthetic oneof like a real oneof and
// emitted a nonexistent Go type.
func TestPlugin_Proto3OptionalDoesNotEmitWrapperType(t *testing.T) {
	// message Foo {
	//   optional int32 bar   = 1;              // proto3 optional → synthetic oneof
	//   repeated int32 items = 2 [(wireguard.max_count) = 5];
	// }
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("Foo"),
		// synthetic oneof generated by the proto3 optional field
		OneofDecl: []*descriptorpb.OneofDescriptorProto{
			{Name: proto.String("_bar")},
		},
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:           proto.String("bar"),
				Number:         proto.Int32(1),
				Type:           descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				OneofIndex:     proto.Int32(0),
				Proto3Optional: proto.Bool(true),
			},
			{
				Name:    proto.String("items"),
				Number:  proto.Int32(2),
				Type:    descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
				Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
				Options: buildFieldOptions(map[protowire.Number]uint32{maxCountFieldNum: 5}),
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
	out, err := runPlugin(t, files, "test.proto", "module=github.com/example")
	require.NoError(t, err)

	content := out["test/test.wireguard.go"]
	require.NotEmpty(t, content)

	// items must be capped normally.
	require.Contains(t, content, `2: {MaxCount: 5}`)
	// The synthetic-oneof wrapper type Foo_Bar must NOT appear.
	require.NotContains(t, content, "Foo_Bar")
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func init() {
	// Touch protoregistry to make sure descriptor.proto is available.
	if _, err := protoregistry.GlobalFiles.FindFileByPath("google/protobuf/descriptor.proto"); err != nil {
		panic("descriptor.proto not registered: " + err.Error())
	}
}
