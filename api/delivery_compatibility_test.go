package api

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestDeliveryWireCompatibility(t *testing.T) {
	wire, err := os.ReadFile("testdata/downlink-delivery.bin")
	require.NoError(t, err)
	var current AnalyticsMetrics
	require.NoError(t, proto.Unmarshal(wire, &current))
	require.Len(t, current.Downlinks, 1)
	require.Len(t, current.DownlinkOutcomes, 1)
	require.Equal(t, current.Downlinks[0].Delivery.Id, current.DownlinkOutcomes[0].Id)
	require.Equal(t, DownlinkOutcomeStatus_OUTCOME_ACCEPTED, current.DownlinkOutcomes[0].Status)
	require.Len(t, current.Downlinks[0].PhyPayload, 13)
	require.EqualValues(t, 1795096801000, current.DownlinkOutcomes[0].ObservedUnixMs)
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(&current)
	require.NoError(t, err)
	require.Equal(t, wire, encoded, "shared Go/C++/engine fixture must reproduce exactly")

	old := protodesc.ToFileDescriptorProto(File_analytics_proto)
	for _, message := range old.MessageType {
		fields := message.Field[:0]
		for _, field := range message.Field {
			if message.GetName() == "AnalyticsMetrics" && field.GetNumber() == 8 {
				continue
			}
			if message.GetName() == "AnalyticsDownlink" && field.GetNumber() == 22 {
				continue
			}
			fields = append(fields, field)
		}
		message.Field = fields
	}
	descriptor, err := protodesc.NewFile(old, nil)
	require.NoError(t, err)
	legacy := dynamicpb.NewMessage(descriptor.Messages().ByName("AnalyticsMetrics"))
	require.NoError(t, proto.Unmarshal(wire, legacy))
	packets := legacy.Descriptor().Fields().ByName("downlinks")
	require.Equal(t, 1, legacy.Get(packets).List().Len(), "an old server receives one packet, never a phantom ACK packet")
	legacy.Clear(packets)
	unknownOnly, err := proto.Marshal(legacy)
	require.NoError(t, err)
	var roundtrip AnalyticsMetrics
	require.NoError(t, proto.Unmarshal(unknownOnly, &roundtrip))
	require.Len(t, roundtrip.DownlinkOutcomes, 1, "unknown fields survive an old transport relay")
	require.Empty(t, roundtrip.Downlinks)

	legacy.SetUnknown(nil)
	legacyOnly, err := proto.Marshal(legacy)
	require.NoError(t, err)
	var receiver AnalyticsMetrics
	require.NoError(t, proto.Unmarshal(legacyOnly, &receiver))
	require.Empty(t, receiver.DownlinkOutcomes, "legacy senders fabricate no outcome evidence")
}
