package main

import (
	"fmt"
	"iter"
	"log"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type md struct{ protoreflect.MessageDescriptor }
type mds = map[protoreflect.FullName]md

func getExtType(p *protogen.Plugin, name protoreflect.FullName) (protoreflect.ExtensionType, bool) {
	for _, file := range p.Files {
		es := file.Desc.Extensions()
		for i := range es.Len() {
			if e := es.Get(i); e.FullName() == name {
				return dynamicpb.NewExtensionType(e), true
			}
		}
	}
	return nil, false
}

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

func allMDs(plugin *protogen.Plugin) iter.Seq[md] {
	return func(yield func(md) bool) {
		for _, file := range plugin.Files {
			descs := file.Desc.Messages()
			for i := range descs.Len() {
				if !(md{descs.Get(i)}).walk(yield) {
					return
				}
			}
		}
	}
}

func (d md) GetBoolOption(opt protoreflect.ExtensionType) bool {
	options, ok := d.Options().(*descriptorpb.MessageOptions)
	if !ok || !proto.HasExtension(options, opt) {
		return false
	}
	has, ok := proto.GetExtension(options, opt).(bool)
	return ok && has
}

// run reads the proto descriptors and checks that the can_hash messages satisfy the following constraints:
// * all can_hash messages have to use proto3 syntax
// * message fields of can_hash messages have to be can_hash as well
// * fields of can_hash messages have to be repeated/optional (explicit presence)
// * fields of can_hash messages cannot be maps
func run(p *protogen.Plugin) error {
	canHashName := protoreflect.FullName("tendermint.utils.can_hash")
	canHashOpt, ok := getExtType(p, canHashName)
	if !ok {
		// When the module being processed does not declare the extension we have nothing to validate.
		return nil
	}
	descs := mds{}
	for d := range allMDs(p) {
		if d.GetBoolOption(canHashOpt) {
			descs[d.FullName()] = d
		}
	}
	log.Printf("buf_plugin: found can_hash option; %d message type(s) marked with it", len(descs))
	for _, d := range descs {
		if d.Syntax() != protoreflect.Proto3 {
			return fmt.Errorf("%q: can_hash messages have to be in proto3 syntax", d.FullName())
		}
		fields := d.Fields()
		for i := 0; i < fields.Len(); i++ {
			f := fields.Get(i)
			if f.IsMap() {
				return fmt.Errorf("%q: maps are not allowed in can_hash messages", f.FullName())
			}
			if !f.IsList() && !f.HasPresence() {
				return fmt.Errorf("%q: all fields of can_hash messages should be optional or repeated", f.FullName())
			}
			if f.Kind() == protoreflect.MessageKind {
				if _, ok := descs[f.Message().FullName()]; !ok {
					return fmt.Errorf("%q: message fields of can_hash messages have to be can_hash", f.FullName())
				}
			}
		}
	}
	return nil
}

func main() {
	protogen.Options{}.Run(run)
}
