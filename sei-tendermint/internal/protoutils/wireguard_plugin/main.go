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
// * every singular field is implicitly capped to one wire occurrence
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
// Scan imposes an implicit max_count = 1 for singular fields.
//
// TODO: dedup with sei-tendermint/internal/hashable/plugin in a later PR.
package main

import (
	"errors"
	"fmt"
	"iter"
	"math"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/pluginpb"
)

type errMustBePositive struct {
	Rule string
}

func (err errMustBePositive) Error() string {
	return fmt.Sprintf("wireguard.%s must be > 0", err.Rule)
}

var (
	errSizeRulesRequireSizedFieldType         = errors.New("wireguard size rules require a string, bytes, or message field")
	errMaxTotalSizeRequiresRepeatedField      = errors.New("wireguard.max_total_size requires a repeated field")
	errSizedMapField                          = errors.New("wireguard.sized messages must not contain map fields")
	errSizedGroupField                        = errors.New("wireguard.sized messages must not contain group fields")
	errSizedFieldMissingMaxCount              = errors.New("wireguard.sized repeated field missing wireguard.max_count")
	errSizedRepeatedFieldNeedsSizeOrSizedNest = errors.New("wireguard.sized repeated field needs a size bound or sized nested message")
	errSizedFieldNeedsSizeOrSizedNest         = errors.New("wireguard.sized field needs a size bound or sized nested message")
	errSizedRecursiveMessage                  = errors.New("wireguard.sized messages must not recursively include themselves")
)

func main() {
	protogen.Options{}.Run(run)
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

	if err := validateRuleValues(byName, exts); err != nil {
		return err
	}
	if err := validateSizedMessages(byName, exts); err != nil {
		return err
	}
	maxSizes, err := computeSizedMessageMaxSizes(byName, exts)
	if err != nil {
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
		maxSizes: maxSizes,
		exts:     exts,
	})
}

type sizedMessageMaxState struct {
	byName map[protoreflect.FullName]protoreflect.MessageDescriptor
	exts   wireguardExts
	cache  map[protoreflect.FullName]int
	stack  map[protoreflect.FullName]struct{}
}

func computeSizedMessageMaxSizes(byName map[protoreflect.FullName]protoreflect.MessageDescriptor, exts wireguardExts) (map[protoreflect.FullName]int, error) {
	state := sizedMessageMaxState{
		byName: byName,
		exts:   exts,
		cache:  map[protoreflect.FullName]int{},
		stack:  map[protoreflect.FullName]struct{}{},
	}
	for _, d := range byName {
		if !hasTrueMessageOption(d, exts.sized) {
			continue
		}
		if _, err := state.messageSize(d); err != nil {
			return nil, err
		}
	}
	return state.cache, nil
}

func (s *sizedMessageMaxState) messageSize(d protoreflect.MessageDescriptor) (int, error) {
	if size, ok := s.cache[d.FullName()]; ok {
		return size, nil
	}
	if _, ok := s.stack[d.FullName()]; ok {
		return 0, fmt.Errorf("%s: %w", d.FullName(), errSizedRecursiveMessage)
	}
	s.stack[d.FullName()] = struct{}{}
	defer delete(s.stack, d.FullName())

	var total int
	fields := d.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if oo := fd.ContainingOneof(); oo != nil && !oo.IsSynthetic() {
			if oo.Fields().Get(0) != fd {
				continue
			}
			var oneofMax int
			for j := range oo.Fields().Len() {
				fieldSize, err := s.fieldSize(oo.Fields().Get(j))
				if err != nil {
					return 0, err
				}
				oneofMax = max(oneofMax, fieldSize)
			}
			total = addInt(total, oneofMax)
			continue
		}

		fieldSize, err := s.fieldSize(fd)
		if err != nil {
			return 0, err
		}
		total = addInt(total, fieldSize)
	}

	s.cache[d.FullName()] = total
	return total, nil
}

func (s *sizedMessageMaxState) fieldSize(fd protoreflect.FieldDescriptor) (int, error) {
	switch fd.Kind() {
	case protoreflect.StringKind, protoreflect.BytesKind:
		bound, _ := bytesFieldBound(fd, s.exts)
		return bound, nil
	case protoreflect.MessageKind:
		if bound, ok := bytesFieldBound(fd, s.exts); ok {
			return bound, nil
		}
		nestedSize, err := s.messageSize(fd.Message())
		if err != nil {
			return 0, err
		}
		return mulInt(maxCount(fd, s.exts), bytesFieldSize(fd.Number(), nestedSize)), nil
	default:
		valueSize := s.scalarValueSize(fd)
		count := maxCount(fd, s.exts)
		size := mulInt(count, addInt(protowire.SizeTag(fd.Number()), valueSize))
		if fd.IsList() {
			size = max(size, bytesFieldSize(fd.Number(), mulInt(count, valueSize)))
		}
		return size, nil
	}
}

func (s *sizedMessageMaxState) scalarValueSize(fd protoreflect.FieldDescriptor) int {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return 1
	case protoreflect.EnumKind:
		size := 1
		values := fd.Enum().Values()
		for i := range values.Len() {
			size = max(size, protowire.SizeVarint(uint64(int64(values.Get(i).Number())))) // nolint: gosec // WAI, negative enums are encoded as uint64
		}
		return size
	case protoreflect.Int32Kind, protoreflect.Int64Kind:
		return 10
	case protoreflect.Sint32Kind:
		return protowire.SizeVarint(protowire.EncodeZigZag(-1 << 31))
	case protoreflect.Sint64Kind:
		return protowire.SizeVarint(protowire.EncodeZigZag(-1 << 63))
	case protoreflect.Uint32Kind:
		return protowire.SizeVarint(^uint64(uint32(0)))
	case protoreflect.Uint64Kind:
		return 10
	case protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.FloatKind:
		return protowire.SizeFixed32()
	case protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind, protoreflect.DoubleKind:
		return protowire.SizeFixed64()
	default:
		panic(fmt.Errorf("%s: unsupported field kind %s", fd.FullName(), fd.Kind()))
	}
}

func fieldIntOption(fd protoreflect.FieldDescriptor, ext protoreflect.ExtensionType) (int, bool) {
	opts := fd.Options().(*descriptorpb.FieldOptions).ProtoReflect()
	if !opts.Has(ext.TypeDescriptor()) {
		return 0, false
	}
	return int(opts.Get(ext.TypeDescriptor()).Uint()), true // nolint: gosec // proto.Unmarshal is using int for sizes, no point in specifying higher limits
}

func maxCount(fd protoreflect.FieldDescriptor, exts wireguardExts) int {
	if fd.IsList() {
		count, ok := fieldIntOption(fd, exts.maxCount)
		if !ok {
			panic("missing max_count on a repeated field")
		}
		return count
	}
	return 1
}

func bytesFieldSize(f protoreflect.FieldNumber, rawSize int) int {
	return addInt(protowire.SizeTag(f), protowire.SizeVarint(uint64(rawSize)), rawSize) // nolint: gosec // always >=0
}

func bytesFieldBound(fd protoreflect.FieldDescriptor, exts wireguardExts) (int, bool) {
	count := maxCount(fd, exts)
	item := math.MaxInt
	total := math.MaxInt
	bounded := false
	if m, ok := fieldIntOption(fd, exts.maxTotalSize); ok {
		total = min(total, m)
		item = min(item, m)
		bounded = true
	}
	if m, ok := fieldIntOption(fd, exts.maxSize); ok {
		total = min(total, mulInt(count, m))
		item = min(item, m)
		bounded = true
	}
	if !bounded {
		return 0, false
	}
	if count == 0 {
		return 0, true
	}
	// Overapproximation by at most <count> bytes.
	// Total size is maximized by maximizing the number of instances that <total> is spread across.
	item = min(item, total/count+1)
	return mulInt(count, bytesFieldSize(fd.Number(), item)), true
}

func addInt(vs ...int) int {
	sum := 0
	for _, v := range vs {
		if v > math.MaxInt-sum {
			return math.MaxInt
		}
		sum += v
	}
	return sum
}

func mulInt(a int, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxInt/b {
		return math.MaxInt
	}
	return a * b
}

// validateRuleValues rejects explicit zero-valued wireguard annotations,
// which would silently mean "no cap" at runtime. An explicit zero is almost
// certainly a mistake; pick a positive cap or drop the annotation if the
// field is genuinely unbounded.
func validateRuleValues(byName map[protoreflect.FullName]protoreflect.MessageDescriptor, exts wireguardExts) error {
	for _, d := range byName {
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
			if opts.Has(exts.maxTotalSize.TypeDescriptor()) && !f.IsList() {
				return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errMaxTotalSizeRequiresRepeatedField)
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
			if f.Kind() == protoreflect.GroupKind {
				return fmt.Errorf("%s.%s: %w", d.FullName(), f.Name(), errSizedGroupField)
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

	// maxSizes stores the generated MaxSize() return value for each sized
	// message.
	maxSizes map[protoreflect.FullName]int

	// exts are the wireguard FieldOptions extensions resolved against the
	// rebuilt descriptor graph.
	exts wireguardExts
}

// emitIdents lazily qualifies the Go identifiers a generated file actually
// uses, so we don't force imports into files that never reference them.
type emitIdents struct{ g *protogen.GeneratedFile }

func newEmitIdents(g *protogen.GeneratedFile) emitIdents { return emitIdents{g: g} }

func (i emitIdents) qual(pkg, name string) string {
	return i.g.QualifiedGoIdent(protogen.GoIdent{GoName: name, GoImportPath: protogen.GoImportPath(pkg)})
}

func (i emitIdents) schema() string       { return i.qual(wireguardRuntime, "Schema") }
func (i emitIdents) mustRegister() string { return i.qual(wireguardRuntime, "MustRegister") }
func (i emitIdents) utilsSome() string    { return i.qual(utilsPkg, "Some") }
func (i emitIdents) reflectTypeFor() string {
	return i.qual("reflect", "TypeFor")
}
func (i emitIdents) varintType() string  { return i.qual(wireguardRuntime, "VarintType") }
func (i emitIdents) fixed32Type() string { return i.qual(wireguardRuntime, "Fixed32Type") }
func (i emitIdents) fixed64Type() string { return i.qual(wireguardRuntime, "Fixed64Type") }

// emit walks files and emits per-file <name>.wireguard.go containing init()
// registrations for messages defined in that file.
func emit(p *protogen.Plugin, ctx emitCtx) error {
	for _, file := range p.Files {
		if !file.Generate {
			continue
		}
		var targets []*protogen.Message
		for m := range allPMs(file) {
			if m.Desc.IsMapEntry() {
				continue
			}
			targets = append(targets, m.Message)
		}
		if len(targets) == 0 {
			continue
		}

		genFileName := strings.TrimSuffix(filepath.Base(file.Desc.Path()), ".proto") + ".wireguard.go"
		g := p.NewGeneratedFile(filepath.Join(string(file.GoImportPath), genFileName), file.GoImportPath)
		g.P("// Code generated by sei-tendermint/internal/protoutils/wireguard_plugin. DO NOT EDIT.")
		g.P("package ", file.GoPackageName)
		g.P()
		idents := newEmitIdents(g)
		emitRegistrations(g, targets, ctx, idents)
	}
	return nil
}

func emitRegistrations(g *protogen.GeneratedFile, targets []*protogen.Message, ctx emitCtx, idents emitIdents) {
	for _, m := range targets {
		if !hasTrueMessageOption(ctx.byName[m.Desc.FullName()], ctx.exts.sized) {
			continue
		}
		emitMaxSizeMethod(g, m, ctx)
	}

	g.P("func init() {")
	for _, m := range targets {
		emitSchemaRegistration(g, m, ctx, idents)
	}
	g.P("}")
	g.P()
}

func emitMaxSizeMethod(g *protogen.GeneratedFile, m *protogen.Message, ctx emitCtx) {
	size := ctx.maxSizes[m.Desc.FullName()]
	g.P("func (*", m.GoIdent.GoName, ") MaxSize() int {")
	g.P("return ", size)
	g.P("}")
	g.P()
}

func emitSchemaRegistration(g *protogen.GeneratedFile, m *protogen.Message, ctx emitCtx, idents emitIdents) {
	// Use the descriptor from ctx.byName (which has dynamic extension
	// options resolved) rather than m.Desc (protogen's view, which
	// doesn't).
	d := ctx.byName[m.Desc.FullName()]
	g.P("// Register the wireguard.Schema generated for ", d.FullName(), ".")
	g.P(idents.mustRegister(), "[*", m.GoIdent.GoName, "](", idents.schema(), "{")
	for _, pf := range m.Fields {
		f := d.Fields().Get(pf.Desc.Index())
		opts := f.Options().(*descriptorpb.FieldOptions).ProtoReflect()
		hasMaxCount := opts.Has(ctx.exts.maxCount.TypeDescriptor())
		hasMaxSize := opts.Has(ctx.exts.maxSize.TypeDescriptor())
		hasMaxTotalSize := opts.Has(ctx.exts.maxTotalSize.TypeDescriptor())
		implicitSingularMaxCount := !f.IsList() && !f.IsMap()

		nestedTarget := f.Message()
		if f.IsMap() {
			nestedTarget = f.MapValue().Message()
		} else if nestedTarget != nil && nestedTarget.IsMapEntry() {
			nestedTarget = nil
		}
		if !implicitSingularMaxCount && !hasMaxCount && !hasMaxSize && !hasMaxTotalSize && nestedTarget == nil && !f.IsMap() {
			continue
		}

		var pieces []string
		if implicitSingularMaxCount || hasMaxCount {
			pieces = append(pieces, fmt.Sprintf("MaxCount: %d", maxCount(f, ctx.exts)))
			if packedTypeExpr := packedTypeExpr(f, idents); packedTypeExpr != "" {
				pieces = append(pieces, fmt.Sprintf("PackedType: %s(%s)", idents.utilsSome(), packedTypeExpr))
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
		if f.IsMap() {
			pieces = append(pieces, "IsMap: true")
		}
		if nestedTarget != nil {
			targetExpr := typeExprForTarget(g, ctx, nestedTarget, idents)
			pieces = append(pieces, fmt.Sprintf("Nested: %s(%s)", idents.utilsSome(), targetExpr))
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
	return fmt.Sprintf("%s[*%s]()", idents.reflectTypeFor(), target)
}

func packedTypeExpr(f protoreflect.FieldDescriptor, idents emitIdents) string {
	if !f.IsList() {
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
		return idents.varintType()
	case protoreflect.Fixed32Kind,
		protoreflect.Sfixed32Kind,
		protoreflect.FloatKind:
		return idents.fixed32Type()
	case protoreflect.Fixed64Kind,
		protoreflect.Sfixed64Kind,
		protoreflect.DoubleKind:
		return idents.fixed64Type()
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
