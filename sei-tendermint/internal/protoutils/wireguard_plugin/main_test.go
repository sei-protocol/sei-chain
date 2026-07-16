package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bufbuild/protocompile"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/pluginpb"
)

func compilePluginFixture(t *testing.T, fileToGenerate string) (*pluginpb.CodeGeneratorRequest, error) {
	t.Helper()

	fixtureRoot, protoRoot := pluginTestRoots(t)
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			ImportPaths: []string{fixtureRoot, protoRoot},
		}),
	}
	files, err := compiler.Compile(t.Context(), fileToGenerate)
	if err != nil {
		return nil, err
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{fileToGenerate},
		Parameter:      proto.String("module=github.com/example"),
	}
	for _, file := range files {
		collectDescriptors(file, req)
	}
	return req, nil
}

func runPluginRequest(req *pluginpb.CodeGeneratorRequest) error {
	plug, err := protogen.Options{}.New(req)
	if err != nil {
		return err
	}
	return run(plug)
}

func pluginTestRoots(t *testing.T) (fixtureRoot, protoRoot string) {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(currentFile)
	goModuleRoot := findGoModuleRoot(t, dir)
	return filepath.Join(dir, "testdata"), filepath.Join(goModuleRoot, "sei-tendermint/proto")
}

func findGoModuleRoot(t *testing.T, start string) string {
	t.Helper()

	for dir := start; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "failed to find go module root from %s", start)
	}
}

func collectDescriptors(root protoreflect.FileDescriptor, req *pluginpb.CodeGeneratorRequest) {
	seen := map[string]struct{}{}
	var visit func(protoreflect.FileDescriptor)
	visit = func(file protoreflect.FileDescriptor) {
		if _, ok := seen[file.Path()]; ok {
			return
		}
		seen[file.Path()] = struct{}{}
		imports := file.Imports()
		for i := range imports.Len() {
			visit(imports.Get(i).FileDescriptor)
		}
		req.ProtoFile = append(req.ProtoFile, protodesc.ToFileDescriptorProto(file))
	}
	visit(root)
}

func TestPlugin_Rejections(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		fixture string
		wantErr error
	}{
		{"max_count_zero.proto", errMustBePositive{Rule: "max_count"}},
		{"max_size_zero.proto", errMustBePositive{Rule: "max_size"}},
		{"max_total_size_zero.proto", errMustBePositive{Rule: "max_total_size"}},
		{"singular_max_total_size.proto", errMaxTotalSizeRequiresRepeatedField},
		{"size_rule_on_scalar.proto", errSizeRulesRequireSizedFieldType},
		{"sized_map_field.proto", errSizedMapField},
		{"sized_group_field.proto", errSizedGroupField},
		{"sized_unbounded_bytes.proto", errSizedFieldNeedsSizeOrSizedNest},
		{"sized_repeated_without_max_count.proto", errSizedFieldMissingMaxCount},
		{"sized_unsized_nested_without_field_size.proto", errSizedFieldNeedsSizeOrSizedNest},
		{"sized_repeated_message_needs_size_or_sized_nested.proto", errSizedRepeatedFieldNeedsSizeOrSizedNest},
		{"sized_recursive.proto", errSizedRecursiveMessage},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()

			req, err := compilePluginFixture(t, tc.fixture)
			require.NoError(t, err)
			require.ErrorIs(t, runPluginRequest(req), tc.wantErr)
		})
	}
}
