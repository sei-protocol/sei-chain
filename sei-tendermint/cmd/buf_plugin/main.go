package main

import (
	"fmt"
	"iter"
	"log"
	"errors"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/pluginpb"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func (d md) GetBoolOption(ext protoreflect.ExtensionTypeDescriptor) bool {
	options := d.Options().(*descriptorpb.MessageOptions).ProtoReflect()
	if !options.Has(ext) { return false }
	return options.Get(ext).Bool()
}
type md struct{ protoreflect.MessageDescriptor }
type mds = map[protoreflect.FullName]md

func (d md) walk(yield func(md) bool) bool {
	if !yield(d) {
		return false
	}
	descs := d.Messages()
	for i := range descs.Len() {
		if !(md{descs.Get(i)}).walk(yield) {
			return false
		}
	}
	return true
}

func allMDs(files *protoregistry.Files) iter.Seq[md] {
	return func(yield func(md) bool) {
		for file := range files.RangeFiles {
			descs := file.Messages()
			for i := range descs.Len() {
				if !(md{descs.Get(i)}).walk(yield) {
					return
				}
			}
		}
	}
}

func OrPanic(err error) {
	if err!=nil { panic(err) }
}

func OrPanic1[T any](v T, err error) T {
	OrPanic(err)
	return v
}

// run reads the proto descriptors and checks that the hashable messages satisfy the following constraints:
// * all hashable messages have to use proto3 syntax
// * message fields of hashable messages have to be hashable as well
// * fields of hashable messages have to be repeated/optional (explicit presence)
// * fields of hashable messages cannot be maps
func run(p *protogen.Plugin) error {
	p.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

	fds := &descriptorpb.FileDescriptorSet{File: p.Request.ProtoFile}
	// Re-unmarshal proto files, so that dynamic options are registered.
	OrPanic(proto.UnmarshalOptions{
		Resolver: dynamicpb.NewTypes(OrPanic1(protodesc.NewFiles(fds))),
	}.Unmarshal(OrPanic1(proto.Marshal(fds)),fds))
	files := OrPanic1(protodesc.NewFiles(fds))

	hashableOpt,err := dynamicpb.NewTypes(files).FindExtensionByName("hashable.hashable")
	if err!=nil {
		if errors.Is(err,protoregistry.NotFound) {
			return nil
		}
		panic(fmt.Errorf("files.FindExtensionByName(): %w",err))
	}
	descs := mds{}
	for d := range allMDs(files) {
		if d.GetBoolOption(hashableOpt.TypeDescriptor()) {
			descs[d.FullName()] = d
		}
	}
	log.Printf("buf_plugin: found hashable option; %d message type(s) marked with it", len(descs))
	for _, d := range descs {
		if d.Syntax() != protoreflect.Proto3 {
			return fmt.Errorf("%q: hashable messages have to be in proto3 syntax", d.FullName())
		}
		fields := d.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.IsExtension() {
				return fmt.Errorf("%q: extension fields are not hashable", f.FullName())
			}
			if f.IsMap() {
				return fmt.Errorf("%q: maps are not hashable", f.FullName())
			}
			if !f.IsList() && !f.HasPresence() {
				return fmt.Errorf("%q: all fields of hashable messages should be optional or repeated", f.FullName())
			}
			switch f.Kind() {
			case protoreflect.FloatKind, protoreflect.DoubleKind:
				return fmt.Errorf("%q: float fields are not hashable",f.FullName())
			case protoreflect.GroupKind:
				return fmt.Errorf("%q: group field are not hashable",f.FullName())
			case protoreflect.MessageKind:
				if _, ok := descs[f.Message().FullName()]; !ok {
					return fmt.Errorf("%q: message fields of hashable messages have to be hashable", f.FullName())
				}
			}
		}
	}
	return nil
}

func main() {
	protogen.Options{}.Run(run)
}
