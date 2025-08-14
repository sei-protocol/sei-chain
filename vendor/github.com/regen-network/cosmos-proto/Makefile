.PHONY: proto-gen test

all: proto-gen test

proto-gen:
	go get github.com/gogo/protobuf/protoc-gen-gogoslick
	protoc --gogoslick_out=Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor,paths=source_relative:. cosmos.proto

test:
	go install ./protoc-gen-gocosmos
	protoc -I=. --gocosmos_out=plugins=interfacetype,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types:. test/abc.proto
	go test ./test
