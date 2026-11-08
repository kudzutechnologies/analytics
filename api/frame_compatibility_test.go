package api

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestFrameSchemaBackwardCompatibility(t *testing.T) {
	old := protodesc.ToFileDescriptorProto(File_analytics_proto)
	for _, m := range old.MessageType {
		max := int32(0)
		if m.GetName() == "AnalyticsUplink" {
			max = 17
		}
		if m.GetName() == "AnalyticsDownlink" {
			max = 19
		}
		if max == 0 {
			continue
		}
		fields := m.Field[:0]
		for _, f := range m.Field {
			if f.GetNumber() <= max {
				fields = append(fields, f)
			}
		}
		m.Field = fields
	}
	legacy, err := protodesc.NewFile(old, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, newMessage := range []proto.Message{&AnalyticsUplink{Fhdr: []byte{1, 2}, Size: 6, PhyPayload: []byte{1, 2, 3, 4, 5, 6}, GatewayContext: []byte{7}}, &AnalyticsDownlink{Fhdr: []byte{1, 2}, Size: 6, PhyPayload: []byte{1, 2, 3, 4, 5, 6}, GatewayContext: []byte{7}}} {
		t.Run(string(newMessage.ProtoReflect().Descriptor().Name()), func(t *testing.T) {
			wire, err := proto.Marshal(newMessage)
			if err != nil {
				t.Fatal(err)
			}
			receiver := dynamicpb.NewMessage(legacy.Messages().ByName(newMessage.ProtoReflect().Descriptor().Name()))
			if err := proto.Unmarshal(wire, receiver); err != nil {
				t.Fatal(err)
			}
			fhdr := receiver.Descriptor().Fields().ByName("fhdr")
			size := receiver.Descriptor().Fields().ByName("size")
			if string(receiver.Get(fhdr).Bytes()) != "\x01\x02" || receiver.Get(size).Uint() != 6 {
				t.Fatal("new sender changed legacy fields")
			}
			sender := dynamicpb.NewMessage(receiver.Descriptor())
			sender.Set(fhdr, protoreflect.ValueOfBytes([]byte{1, 2}))
			sender.Set(size, protoreflect.ValueOfUint32(6))
			wire, err = proto.Marshal(sender)
			if err != nil {
				t.Fatal(err)
			}
			modern := newMessage.ProtoReflect().New().Interface()
			if err := proto.Unmarshal(wire, modern); err != nil {
				t.Fatal(err)
			}
			field := modern.ProtoReflect().Descriptor().Fields().ByName("phyPayload")
			if len(modern.ProtoReflect().Get(field).Bytes()) != 0 {
				t.Fatal("old sender fabricated a full payload")
			}
		})
	}
}
