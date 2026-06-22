// Package main implements a protoc plugin that turns wireguard.proto field
// annotations into init()-time wireguard schema registrations, one per
// annotated message type. The output sits next to the .pb.go files as
// `<name>.wireguard.go`.
//
// Supported message annotations:
//
//	(wireguard.sized) = true       // require structurally bounded message size
//
// Supported field annotations:
//
//	(wireguard.max_count) = N      // cap on a repeated field's instances per message
//	(wireguard.max_size) = N       // cap on one string/bytes/message instance
//	(wireguard.max_total_size) = N // cap on summed bytes across field instances per message
//
// In particular a sized message needs to have
// * max_count on every repeated int/sized message field
// * max_size on every singular bytes/string/non-sized message field
// * max_count AND (max_size OR max_total_size) on every repeated bytes/string/non-sized message field
// Note that setting max_count and max_size effectively also bounds the total size by max_size * max_count,
// but you can also set all: max_count, max_size, max_total_size, in which case the total size is bounded
// by min(max_total_size,max_size * max_count).
//
// NOTE: maps are NOT allowed in sized messages.
//
// Annotations represent constraints on the field sizes.
// Scan[T] traverses the binary encoded proto message checking that the constraints are satisfied.
// This is useful for validating potentially malicious inputs BEFORE decoding the message - decoded message
// might be significantly larger than the encoded message, which in turn might cause an OOM.
//
// NOTE: proto encoding containing multiple instances of a singular field, is a correct encoding (the last instance wins).
// Scan does NOT impose implicit max_count = 1 for singular fields (even for sized messages), as these are NOT harmful:
// during Unmarshal all but the last instances are simply ignored (it is just garbage data).
//
// TODO: dedup with sei-tendermint/internal/hashable/plugin in a later PR.
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

var moduleFlag = flags.String("module", "", "prefix to strip from the absolute generated file path. Same as in protoc-gen-go")

type errMustBePositive struct {
	Rule string
}

func (err errMustBePositive) Error() string {
	return fmt.Sprintf("wireguard.%s must be > 0", err.Rule)
}

var (
	errSizeRulesRequireSizedFieldType         = errors.New("wireguard size rules require a string, bytes, or message field")
	errSizedMapField                          = errors.New("wireguard.sized messages must not contain map fields")
	errSizedFieldMissingMaxCount              = errors.New("wireguard.sized repeated field missing wireguard.max_count")
	errSizedRepeatedFieldNeedsSizeOrSizedNest = errors.New("wireguard.sized repeated field needs a size bound or sized nested message")
	errSizedFieldNeedsSizeOrSizedNest         = errors.New("wireguard.sized field needs a size bound or sized nested message")
)

func main() {
	protogen.Options{ParamFunc: flags.Set}.Run(run)
}

const wireguardRuntime = "github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/runtime"
const utilsPkg = "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"

type wireguardExts struct {
	sized        protoreflect.ExtensionType
	maxCount     protoreflect.ExtensionType
	maxSize      protoreflect.ExtensionType
	maxTotalSize protoreflect.ExtensionType
}

func findWireguardExts(files *protoregistry.Files) (wireguardExts, error) {
	dyn := dynamicpb.NewTypes(files)
	sized, err := dyn.FindExtensionByName("wireguard.sized")
	if err != nil {
		return wireguardExts{}, fmt.Errorf("sized extension: %w", err)
	}
	mc, err := dyn.FindExtensionByName("wireguard.max_count")
	if err != nil {
		return wireguardExts{}, fmt.Errorf("max_count extension: %w", err)
	}
	ms, err := dyn.FindExtensionByName("wireguard.max_size")
	if err != nil {
		return wireguardExts{}, fmt.Errorf("max_size extension: %w", err)
	}
	mts, err := dyn.FindExtensionByName("wireguard.max_total_size")
	if err != nil {
		return wireguardExts{}, fmt.Errorf("max_total_size extension: %w", err)
	}
	return wireguardExts{
		sized:        sized,
		maxCount:     mc,
		maxSize:      ms,
		maxTotalSize: mts,
	}, nil
}

func extName(ext protoreflect.ExtensionType) string {
	return string(ext.TypeDescriptor().Name())
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

	exts, err := findWireguardExts(files)
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

	// A message has a Schema if it has at least one wireguard-annotated field,
	// or if it reaches a message with a Schema via a message-typed field.
	// Find the closure.
	inSchema := map[protoreflect.FullName]bool{}
	for fullName, d := range byName {
		if hasWireguardRule(d, exts) {
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

	if err := validateRuleValues(byName, inSchema, exts); err != nil {
		return err
	}
	if err := validateSizedMessages(byName, exts); err != nil {
		return err
	}

	// Index every protogen.Message reachable from the request by FullName.
	// emit uses it to resolve a cross-file descent target's Go identifier
	// from the parent generator's view of the file, rather than
	// reconstructing the name from the descriptor.
	byMsg := map[protoreflect.FullName]*protogen.Message{}
	for _, file := range p.Files {
		for m := range allPMs(file) {
			byMsg[m.Desc.FullName()] = m.Message
		}
	}

	return emit(p, emitCtx{
		byName:   byName,
		byMsg:    byMsg,
		inSchema: inSchema,
		exts:     exts,
	})
}

// validateRuleValues rejects explicit zero-valued wireguard annotations,
// which would silently mean "no cap" at runtime. An explicit zero is almost
// certainly a mistake; pick a positive cap or drop the annotation if the
// field is genuinely unbounded.
func validateRuleValues(byName map[protoreflect.FullName]protoreflect.MessageDescriptor, inSchema map[protoreflect.FullName]bool, exts wireguardExts) error {
	for fullName := range inSchema {
		d := byName[fullName]
		fields := d.Fields()
		for i := range fields.Len() {
			f := fields.Get(i)
			opts := f.Options().(*descriptorpb.FieldOptions).ProtoReflect()
			for _, ext := range []protoreflect.ExtensionType{exts.maxCount, exts.maxSize, exts.maxTotalSize} {
				if !opts.Has(ext.TypeDescriptor()) {
					continue
				}
				if opts.Get(ext.TypeDescriptor()).Uint() == 0 {
					return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errMustBePositive{Rule: extName(ext)})
				}
			}
			if (opts.Has(exts.maxSize.TypeDescriptor()) || opts.Has(exts.maxTotalSize.TypeDescriptor())) && !supportsSizeRules(f) {
				return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errSizeRulesRequireSizedFieldType)
			}
		}
	}
	return nil
}

func validateSizedMessages(byName map[protoreflect.FullName]protoreflect.MessageDescriptor, exts wireguardExts) error {
	for _, d := range byName {
		if !hasTrueMessageOption(d, exts.sized) {
			continue
		}
		fields := d.Fields()
		for i := range fields.Len() {
			f := fields.Get(i)
			if f.IsMap() {
				return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errSizedMapField)
			}
			opts := f.Options().(*descriptorpb.FieldOptions).ProtoReflect()
			hasMaxCount := opts.Has(exts.maxCount.TypeDescriptor())
			hasMaxSize := opts.Has(exts.maxSize.TypeDescriptor())
			hasMaxTotalSize := opts.Has(exts.maxTotalSize.TypeDescriptor())

			if f.IsList() && !hasMaxCount {
				return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errSizedFieldMissingMaxCount)
			}

			if f.Kind() != protoreflect.StringKind && f.Kind() != protoreflect.BytesKind && f.Kind() != protoreflect.MessageKind {
				continue
			}
			if hasMaxSize || hasMaxTotalSize {
				continue
			}
			if f.Kind() == protoreflect.MessageKind && hasTrueMessageOption(f.Message(), exts.sized) {
				continue
			}
			if f.IsList() {
				return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errSizedRepeatedFieldNeedsSizeOrSizedNest)
			}
			return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errSizedFieldNeedsSizeOrSizedNest)
		}
	}
	return nil
}

func hasTrueMessageOption(d protoreflect.MessageDescriptor, ext protoreflect.ExtensionType) bool {
	options := d.Options().(*descriptorpb.MessageOptions).ProtoReflect()
	if !options.Has(ext.TypeDescriptor()) {
		return false
	}
	return options.Get(ext.TypeDescriptor()).Bool()
}

func supportsSizeRules(f protoreflect.FieldDescriptor) bool {
	switch f.Kind() {
	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind:
		return true
	default:
		return false
	}
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

func hasWireguardRule(d protoreflect.MessageDescriptor, exts wireguardExts) bool {
	fields := d.Fields()
	for i := range fields.Len() {
		opts := fields.Get(i).Options().(*descriptorpb.FieldOptions).ProtoReflect()
		if opts.Has(exts.maxCount.TypeDescriptor()) || opts.Has(exts.maxSize.TypeDescriptor()) || opts.Has(exts.maxTotalSize.TypeDescriptor()) {
			return true
		}
	}
	return false
}

// emitCtx is the read-only context threaded into per-file/per-message
// emission. It batches the lookup tables and the wireguard / utils / reflect Go
// identifier expressions that emit needs but don't change between calls.
type emitCtx struct {
	// byName indexes every message descriptor (including transitive
	// imports) by FullName. Its values carry dynamic-extension options
	// resolved against the wireguard.proto descriptor, which protogen's
	// own Message.Desc does not.
	byName map[protoreflect.FullName]protoreflect.MessageDescriptor

	// byMsg indexes every protogen.Message reachable from the plugin
	// request by FullName. We use it to resolve a cross-file descent
	// target to its Go identifier without hand-constructing the name.
	byMsg map[protoreflect.FullName]*protogen.Message

	// inSchema is the set of message types we emit schemas for.
	inSchema map[protoreflect.FullName]bool

	// exts are the wireguard FieldOptions extensions resolved against the
	// rebuilt descriptor graph.
	exts wireguardExts
}

// emitIdents holds the Go identifier expressions for the wireguard /
// utils / reflect symbols a generated file refers to. They are produced from the
// current *protogen.GeneratedFile (which records the imports) and so are
// rebuilt for every emitted file.
type emitIdents struct {
	schema, mustRegister, utilsSome, reflectTypeFor string
	varintType, fixed32Type, fixed64Type            string
}

func newEmitIdents(g *protogen.GeneratedFile) emitIdents {
	q := func(pkg, name string) string {
		return g.QualifiedGoIdent(protogen.GoIdent{GoName: name, GoImportPath: protogen.GoImportPath(pkg)})
	}
	return emitIdents{
		schema:         q(wireguardRuntime, "Schema"),
		mustRegister:   q(wireguardRuntime, "MustRegister"),
		utilsSome:      q(utilsPkg, "Some"),
		reflectTypeFor: q("reflect", "TypeFor"),
		varintType:     q(wireguardRuntime, "VarintType"),
		fixed32Type:    q(wireguardRuntime, "Fixed32Type"),
		fixed64Type:    q(wireguardRuntime, "Fixed64Type"),
	}
}

// emit walks files and emits per-file <name>.wireguard.go containing init()
// registrations for messages in the closure that are defined in that file.
func emit(p *protogen.Plugin, ctx emitCtx) error {
	for _, file := range p.Files {
		if !file.Generate {
			continue
		}
		var targets []*protogen.Message
		for m := range allPMs(file) {
			if ctx.inSchema[m.Desc.FullName()] {
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
		g.P("// Code generated by sei-tendermint/internal/protoutils/wireguard_plugin. DO NOT EDIT.")
		g.P("package ", file.GoPackageName)
		g.P()
		idents := newEmitIdents(g)
		emitRegistrations(g, targets, ctx, idents)
	}
	return nil
}

func emitRegistrations(g *protogen.GeneratedFile, targets []*protogen.Message, ctx emitCtx, idents emitIdents) {
	g.P("func init() {")
	for _, m := range targets {
		emitSchemaRegistration(g, m, ctx, idents)
	}
	g.P("}")
	g.P()
}

func emitSchemaRegistration(g *protogen.GeneratedFile, m *protogen.Message, ctx emitCtx, idents emitIdents) {
	// Use the descriptor from ctx.byName (which has dynamic extension
	// options resolved) rather than m.Desc (protogen's view, which
	// doesn't).
	d := ctx.byName[m.Desc.FullName()]
	g.P("// Register the wireguard.Schema generated for ", d.FullName(), ".")
	g.P(idents.mustRegister, "[*", m.GoIdent.GoName, "](", idents.schema, "{")
	for _, pf := range m.Fields {
		f := d.Fields().Get(pf.Desc.Index())
		opts := f.Options().(*descriptorpb.FieldOptions).ProtoReflect()
		hasMaxCount := opts.Has(ctx.exts.maxCount.TypeDescriptor())
		hasMaxSize := opts.Has(ctx.exts.maxSize.TypeDescriptor())
		hasMaxTotalSize := opts.Has(ctx.exts.maxTotalSize.TypeDescriptor())

		var nestedTarget protoreflect.MessageDescriptor
		if target := f.Message(); target != nil && ctx.inSchema[target.FullName()] {
			nestedTarget = target
		}
		if !hasMaxCount && !hasMaxSize && !hasMaxTotalSize && nestedTarget == nil {
			continue
		}

		var pieces []string
		if hasMaxCount {
			maxCount := opts.Get(ctx.exts.maxCount.TypeDescriptor()).Uint()
			pieces = append(pieces, fmt.Sprintf("MaxCount: %d", maxCount))
			if packedTypeExpr := packedTypeExpr(f, idents); packedTypeExpr != "" {
				pieces = append(pieces, fmt.Sprintf("PackedType: %s(%s)", idents.utilsSome, packedTypeExpr))
			}
		}
		if hasMaxSize {
			maxSize := opts.Get(ctx.exts.maxSize.TypeDescriptor()).Uint()
			pieces = append(pieces, fmt.Sprintf("MaxSize: %d", maxSize))
		}
		if hasMaxTotalSize {
			maxTotalSize := opts.Get(ctx.exts.maxTotalSize.TypeDescriptor()).Uint()
			pieces = append(pieces, fmt.Sprintf("MaxTotalSize: %d", maxTotalSize))
		}
		if nestedTarget != nil {
			targetExpr := typeExprForTarget(g, ctx, nestedTarget, idents)
			pieces = append(pieces, fmt.Sprintf("Nested: %s(%s)", idents.utilsSome, targetExpr))
		}
		g.P(f.Number(), ": {", strings.Join(pieces, ", "), "},")
	}
	g.P("})")
	g.P()
}

// typeExprForTarget returns the Go expression that references the target
// message type for the given descriptor, wrapped in reflect.TypeFor so Nested
// can resolve it through the wireguard registry at runtime. We reuse the
// protogen.Message.GoIdent that the standard go generator computed
// rather than reconstructing the Go identifier from the descriptor.
func typeExprForTarget(g *protogen.GeneratedFile, ctx emitCtx, d protoreflect.MessageDescriptor, idents emitIdents) string {
	m, ok := ctx.byMsg[d.FullName()]
	if !ok {
		panic(fmt.Sprintf("wireguard: no protogen.Message for %s", d.FullName()))
	}
	target := g.QualifiedGoIdent(protogen.GoIdent{
		GoName:       m.GoIdent.GoName,
		GoImportPath: m.GoIdent.GoImportPath,
	})
	return fmt.Sprintf("%s[*%s]()", idents.reflectTypeFor, target)
}

func packedTypeExpr(f protoreflect.FieldDescriptor, idents emitIdents) string {
	if !f.IsList() || !f.IsPacked() {
		return ""
	}
	switch f.Kind() {
	case protoreflect.BoolKind,
		protoreflect.EnumKind,
		protoreflect.Int32Kind,
		protoreflect.Int64Kind,
		protoreflect.Uint32Kind,
		protoreflect.Uint64Kind,
		protoreflect.Sint32Kind,
		protoreflect.Sint64Kind:
		return idents.varintType
	case protoreflect.Fixed32Kind,
		protoreflect.Sfixed32Kind,
		protoreflect.FloatKind:
		return idents.fixed32Type
	case protoreflect.Fixed64Kind,
		protoreflect.Sfixed64Kind,
		protoreflect.DoubleKind:
		return idents.fixed64Type
	default:
		return ""
	}
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
