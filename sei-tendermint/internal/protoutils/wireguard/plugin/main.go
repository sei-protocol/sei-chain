// Package main implements a protoc plugin that turns wireguard.proto field
// annotations into *wireguard.Schema variables, one per annotated message
// type. The output sits next to the .pb.go files as `<name>.wireguard.go`.
//
// Only one annotation is needed:
//
//	(wireguard.max_count) = N      // cap on a repeated field's occurrences
//
// Descent into nested message fields is automatic: if a field's target type
// has a Schema (i.e. has annotations somewhere in its reachable subtree),
// the parent's rule descends into it. Fields whose target type has no
// annotations are walked past.
//
// Strict mode (`--strict`): every reachable repeated field must carry
// (wireguard.max_count); a missing annotation is a codegen error. Default
// off so this plugin can land before the full audit of repeated fields
// across the proto tree.
package main

import (
	"errors"
	"flag"
	"fmt"
	"iter"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/pluginpb"
)

var flags flag.FlagSet

var (
	moduleFlag = flags.String("module", "", "prefix to strip from the absolute generated file path. Same as in protoc-gen-go")
	strictFlag = flags.Bool("strict", false, "every reachable repeated field must carry (wireguard.max_count); a missing annotation is a codegen error")
)

func main() {
	protogen.Options{ParamFunc: flags.Set}.Run(run)
}

const (
	wireguardRuntime = "github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/wireguard"
	utilsPkg         = "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

func findMaxCountExt(files *protoregistry.Files) (protoreflect.ExtensionType, error) {
	dyn := dynamicpb.NewTypes(files)
	mc, err := dyn.FindExtensionByName("wireguard.max_count")
	if err != nil {
		return nil, fmt.Errorf("max_count extension: %w", err)
	}
	return mc, nil
}

func run(p *protogen.Plugin) error {
	p.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	// Rebuild the file set so dynamic options resolve against the full graph,
	// including imports we don't generate code for.
	fds := &descriptorpb.FileDescriptorSet{File: p.Request.ProtoFile}
	preFiles, err := protodesc.NewFiles(fds)
	if err != nil {
		return fmt.Errorf("protodesc.NewFiles(): %w", err)
	}
	raw, err := proto.Marshal(fds)
	if err != nil {
		return fmt.Errorf("proto.Marshal(): %w", err)
	}
	if err := (proto.UnmarshalOptions{Resolver: dynamicpb.NewTypes(preFiles)}).Unmarshal(raw, fds); err != nil {
		return fmt.Errorf("re-unmarshal with dynamic types: %w", err)
	}
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		return fmt.Errorf("protodesc.NewFiles() final: %w", err)
	}

	maxCountExt, err := findMaxCountExt(files)
	if err != nil {
		if errors.Is(err, protoregistry.NotFound) {
			return nil
		}
		return err
	}

	// Index every message descriptor by full name.
	byName := map[protoreflect.FullName]protoreflect.MessageDescriptor{}
	for d := range allMDs(files) {
		byName[d.FullName()] = d
	}

	// A message has a Schema if it has at least one (max_count) field, or
	// if it reaches a message with a Schema via a message-typed field. Find
	// the closure.
	inSchema := map[protoreflect.FullName]bool{}
	for fullName, d := range byName {
		if hasMaxCount(d, maxCountExt) {
			inSchema[fullName] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for fullName, d := range byName {
			if inSchema[fullName] {
				continue
			}
			fields := d.Fields()
			for i := range fields.Len() {
				target := fields.Get(i).Message()
				if target != nil && inSchema[target.FullName()] {
					inSchema[fullName] = true
					changed = true
					break
				}
			}
		}
	}

	if *strictFlag {
		if err := strictCheck(byName, inSchema, maxCountExt); err != nil {
			return err
		}
	}

	return emit(p, inSchema, byName, maxCountExt)
}

func allMDs(files *protoregistry.Files) iter.Seq[protoreflect.MessageDescriptor] {
	return func(yield func(protoreflect.MessageDescriptor) bool) {
		for file := range files.RangeFiles {
			for d := range walkMsgs(file.Messages()) {
				if !yield(d) {
					return
				}
			}
		}
	}
}

func walkMsgs(mds protoreflect.MessageDescriptors) iter.Seq[protoreflect.MessageDescriptor] {
	return func(yield func(protoreflect.MessageDescriptor) bool) {
		for i := range mds.Len() {
			d := mds.Get(i)
			if !yield(d) {
				return
			}
			for nested := range walkMsgs(d.Messages()) {
				if !yield(nested) {
					return
				}
			}
		}
	}
}

func hasMaxCount(d protoreflect.MessageDescriptor, ext protoreflect.ExtensionType) bool {
	fields := d.Fields()
	for i := range fields.Len() {
		opts := fields.Get(i).Options().(*descriptorpb.FieldOptions).ProtoReflect()
		if opts.Has(ext.TypeDescriptor()) {
			return true
		}
	}
	return false
}

func strictCheck(byName map[protoreflect.FullName]protoreflect.MessageDescriptor, inSchema map[protoreflect.FullName]bool, ext protoreflect.ExtensionType) error {
	for fullName := range inSchema {
		d := byName[fullName]
		fields := d.Fields()
		for i := range fields.Len() {
			f := fields.Get(i)
			if !f.IsList() {
				continue
			}
			opts := f.Options().(*descriptorpb.FieldOptions).ProtoReflect()
			if !opts.Has(ext.TypeDescriptor()) {
				return fmt.Errorf("strict: %s.%s is repeated but missing (wireguard.max_count)", d.FullName(), f.Name())
			}
		}
	}
	return nil
}

// emit walks files and emits per-file <name>.wireguard.go containing Schema
// vars for messages in the closure that are defined in that file.
func emit(p *protogen.Plugin, inSchema map[protoreflect.FullName]bool, byName map[protoreflect.FullName]protoreflect.MessageDescriptor, ext protoreflect.ExtensionType) error {
	for _, file := range p.Files {
		if !file.Generate {
			continue
		}
		var targets []*protogen.Message
		for m := range allPMs(file) {
			if inSchema[m.Desc.FullName()] {
				targets = append(targets, m.Message)
			}
		}
		if len(targets) == 0 {
			continue
		}

		genDir, err := filepath.Rel(*moduleFlag, string(file.GoImportPath))
		if err != nil {
			return fmt.Errorf("filepath.Rel(): %w", err)
		}
		genFileName := strings.TrimSuffix(filepath.Base(file.Desc.Path()), ".proto") + ".wireguard.go"
		g := p.NewGeneratedFile(filepath.Join(genDir, genFileName), file.GoImportPath)
		g.P("// Code generated by sei-tendermint/internal/protoutils/wireguard/plugin. DO NOT EDIT.")
		g.P("package ", file.GoPackageName)
		g.P()
		wgIdent := g.QualifiedGoIdent(protogen.GoIdent{GoName: "Schema", GoImportPath: protogen.GoImportPath(wireguardRuntime)})
		ruleIdent := g.QualifiedGoIdent(protogen.GoIdent{GoName: "Rule", GoImportPath: protogen.GoImportPath(wireguardRuntime)})
		numIdent := g.QualifiedGoIdent(protogen.GoIdent{GoName: "Number", GoImportPath: protogen.GoImportPath(wireguardRuntime)})
		mustFieldIdent := g.QualifiedGoIdent(protogen.GoIdent{GoName: "MustFieldNum", GoImportPath: protogen.GoImportPath(wireguardRuntime)})
		utilsSomeIdent := g.QualifiedGoIdent(protogen.GoIdent{GoName: "Some", GoImportPath: protogen.GoImportPath(utilsPkg)})

		for _, m := range targets {
			emitSchema(g, m, byName, inSchema, ext, wgIdent, ruleIdent, numIdent, mustFieldIdent, utilsSomeIdent)
		}
	}
	return nil
}

func emitSchema(
	g *protogen.GeneratedFile,
	m *protogen.Message,
	byName map[protoreflect.FullName]protoreflect.MessageDescriptor,
	inSchema map[protoreflect.FullName]bool,
	ext protoreflect.ExtensionType,
	wgIdent, ruleIdent, numIdent, mustFieldIdent, utilsSomeIdent string,
) {
	// Use the descriptor from byName (which has dynamic extension options
	// resolved) rather than m.Desc (protogen's view, which doesn't).
	d := byName[m.Desc.FullName()]
	g.P("// SchemaFor", m.GoIdent.GoName, " is the wireguard.Schema generated for ", d.FullName(), ".")
	g.P("var SchemaFor", m.GoIdent.GoName, " = &", wgIdent, "{")
	g.P("\tRules: map[", numIdent, "]", ruleIdent, "{")
	for _, pf := range m.Fields {
		f := d.Fields().Get(pf.Desc.Index())
		opts := f.Options().(*descriptorpb.FieldOptions).ProtoReflect()
		hasMax := opts.Has(ext.TypeDescriptor())

		var nestedTarget protoreflect.MessageDescriptor
		if target := f.Message(); target != nil && inSchema[target.FullName()] {
			nestedTarget = target
		}
		if !hasMax && nestedTarget == nil {
			continue
		}

		// For a oneof variant, the wire tag is on the wrapper struct
		// (e.g. Message_BlockResponse), not the parent message.
		ownerType := g.QualifiedGoIdent(m.GoIdent)
		if f.ContainingOneof() != nil {
			ownerType = g.QualifiedGoIdent(protogen.GoIdent{
				GoName:       m.GoIdent.GoName + "_" + pf.GoName,
				GoImportPath: m.GoIdent.GoImportPath,
			})
		}
		fieldNumExpr := fmt.Sprintf("%s[%s](%q)", mustFieldIdent, ownerType, string(f.Name()))

		var pieces []string
		if hasMax {
			cap := opts.Get(ext.TypeDescriptor()).Uint()
			pieces = append(pieces, fmt.Sprintf("MaxCount: %d", cap))
		}
		if nestedTarget != nil {
			targetExpr := schemaVarForDescriptor(g, nestedTarget)
			pieces = append(pieces, fmt.Sprintf("Nested: %s(%s)", utilsSomeIdent, targetExpr))
		}
		g.P("\t\t", fieldNumExpr, ": {", strings.Join(pieces, ", "), "},")
	}
	g.P("\t},")
	g.P("}")
	g.P()
}

// schemaVarForDescriptor returns the Go expression that references the
// generated SchemaFor variable for the given message descriptor, qualified
// with the right import if it's in a different package.
func schemaVarForDescriptor(g *protogen.GeneratedFile, d protoreflect.MessageDescriptor) string {
	goIdent, ok := descriptorGoIdent(d)
	if !ok {
		panic(fmt.Sprintf("wireguard: cannot resolve Go identifier for %s", d.FullName()))
	}
	schemaIdent := protogen.GoIdent{
		GoName:       "SchemaFor" + goIdent.GoName,
		GoImportPath: goIdent.GoImportPath,
	}
	return g.QualifiedGoIdent(schemaIdent)
}

// descriptorGoIdent reverse-engineers the Go identifier for a proto message
// from its descriptor: go_package option for the import path, and a
// dot-joined CamelCase nesting for the name.
func descriptorGoIdent(d protoreflect.MessageDescriptor) (protogen.GoIdent, bool) {
	file := d.ParentFile()
	goPkg := file.Options().(*descriptorpb.FileOptions).GetGoPackage()
	if goPkg == "" {
		return protogen.GoIdent{}, false
	}
	if i := strings.IndexByte(goPkg, ';'); i >= 0 {
		goPkg = goPkg[:i]
	}
	parts := []string{string(d.Name())}
	for parent := d.Parent(); parent != nil; parent = parent.Parent() {
		md, ok := parent.(protoreflect.MessageDescriptor)
		if !ok {
			break
		}
		parts = append([]string{string(md.Name())}, parts...)
	}
	return protogen.GoIdent{
		GoName:       strings.Join(parts, "_"),
		GoImportPath: protogen.GoImportPath(goPkg),
	}, true
}

type pm struct{ *protogen.Message }

func (m pm) walk(yield func(pm) bool) bool {
	if !yield(m) {
		return false
	}
	for _, x := range m.Messages {
		if !(pm{x}).walk(yield) {
			return false
		}
	}
	return true
}

func allPMs(f *protogen.File) iter.Seq[pm] {
	return func(yield func(pm) bool) {
		for _, m := range f.Messages {
			if !(pm{m}).walk(yield) {
				return
			}
		}
	}
}
