package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"

	"github.com/kudzutechnologies/analytics/api"
)

func deliveryID(gateway []byte, configured string, peer *net.UDPAddr, token uint16) string {
	scope := "peer:" + peer.String()
	if configured != "" {
		scope = "id:" + configured
	} else if len(gateway) == 8 {
		scope = "eui:" + hex.EncodeToString(gateway)
	}
	return fmt.Sprintf("%s:%04x", scope, token)
}

func commandDelivery(message *SemtechUDPMessage, gateway []byte, configured string, peer *net.UDPAddr) *api.AnalyticsDownlinkDelivery {
	sum := sha256.Sum256(message.Data)
	return &api.AnalyticsDownlinkDelivery{
		Id: deliveryID(gateway, configured, peer, message.Token), CommandKey: hex.EncodeToString(sum[:]),
		ObservedUnixMs: message.Timestamp.UnixMilli(),
	}
}

func ackOutcome(message *SemtechUDPMessage, configured string, peer *net.UDPAddr) *api.AnalyticsDownlinkOutcome {
	out := &api.AnalyticsDownlinkOutcome{
		Id: deliveryID(message.GatewayEUI(), configured, peer, message.Token), ObservedUnixMs: message.Timestamp.UnixMilli(),
	}
	body := message.GetBody()
	// Semtech permits a header-only TX_ACK to indicate success.
	if len(body) == 0 {
		out.Status = api.DownlinkOutcomeStatus_OUTCOME_ACCEPTED
		return out
	}
	var payload struct {
		ACK *struct {
			Error *string `json:"error"`
		} `json:"txpk_ack"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.ACK == nil || payload.ACK.Error == nil ||
		len(*payload.ACK.Error) == 0 || len(*payload.ACK.Error) > 256 {
		out.Reason = "ack_invalid"
		return out
	}
	if *payload.ACK.Error == "NONE" {
		out.Status = api.DownlinkOutcomeStatus_OUTCOME_ACCEPTED
	} else {
		out.Status, out.Reason = api.DownlinkOutcomeStatus_OUTCOME_REJECTED, *payload.ACK.Error
	}
	return out
}
