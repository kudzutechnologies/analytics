package main

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/kudzutechnologies/analytics/api"
	log "github.com/sirupsen/logrus"
)

// ParsedGatewaySync holds validated gateway synchronization fields from config.
type ParsedGatewaySync struct {
	GatewayID  string
	ExternalID string
	EUI        []byte
	Name       string
	Location   *api.GatewayLocation
	Radios     []*api.GatewayRadio
	HasAny     bool
}

func (c ForwarderConfig) hasGatewaySyncOptions() bool {
	return c.GatewayLocation != "" ||
		c.GatewayEUI != "" ||
		c.GatewayEID != "" ||
		c.GatewayName != "" ||
		c.GatewayRadios != ""
}

// ParseGatewaySyncConfig validates and parses gateway-* sync options.
// Returns nil when no sync options are present.
func ParseGatewaySyncConfig(c ForwarderConfig) (*ParsedGatewaySync, error) {
	if !c.hasGatewaySyncOptions() {
		return nil, nil
	}

	out := &ParsedGatewaySync{
		HasAny:     true,
		GatewayID:  strings.TrimSpace(c.GatewayId),
		ExternalID: strings.TrimSpace(c.GatewayEID),
		Name:       strings.TrimSpace(c.GatewayName),
	}

	if c.GatewayEUI != "" {
		eui, err := parseGatewayEUI(c.GatewayEUI)
		if err != nil {
			return nil, err
		}
		out.EUI = eui
	}

	if c.GatewayLocation != "" {
		loc, err := parseGatewayLocation(c.GatewayLocation)
		if err != nil {
			return nil, err
		}
		out.Location = loc
	}

	if c.GatewayRadios != "" {
		radios, err := parseGatewayRadios(c.GatewayRadios)
		if err != nil {
			return nil, err
		}
		out.Radios = radios
	}

	if out.ExternalID == "" && len(out.EUI) == 0 {
		return nil, fmt.Errorf("gateway sync requires gateway-eid or gateway-eui when any gateway-* option is set")
	}

	return out, nil
}

func parseGatewayEUI(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ToLower(s)
	if len(s) != 16 {
		return nil, fmt.Errorf("gateway-eui must be 16 hex characters (8 bytes), got %d chars", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("gateway-eui: %w", err)
	}
	return b, nil
}

func parseGatewayLocation(raw string) (*api.GatewayLocation, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, fmt.Errorf("gateway-location must be lat,lon or lat,lon,alt")
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("gateway-location latitude: %w", err)
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return nil, fmt.Errorf("gateway-location longitude: %w", err)
	}
	if lat < -90 || lat > 90 {
		return nil, fmt.Errorf("gateway-location latitude out of range: %v", lat)
	}
	if lon < -180 || lon > 180 {
		return nil, fmt.Errorf("gateway-location longitude out of range: %v", lon)
	}
	loc := &api.GatewayLocation{
		Latitude:  lat,
		Longitude: lon,
	}
	if len(parts) == 3 {
		alt, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		if err != nil {
			return nil, fmt.Errorf("gateway-location altitude: %w", err)
		}
		loc.Altitude = &alt
	}
	return loc, nil
}

func parseGatewayRadios(raw string) ([]*api.GatewayRadio, error) {
	entries := strings.Split(raw, ",")
	seen := map[int32]struct{}{}
	out := make([]*api.GatewayRadio, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		if len(parts) != 2 && len(parts) != 3 {
			return nil, fmt.Errorf("gateway-radios entry %q must be rf_chain_id:max_tx_power[:tx_sensitivity]", entry)
		}
		chain, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("gateway-radios rf_chain_id: %w", err)
		}
		power, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 32)
		if err != nil {
			return nil, fmt.Errorf("gateway-radios max_tx_power: %w", err)
		}
		rf := int32(chain)
		if _, ok := seen[rf]; ok {
			return nil, fmt.Errorf("gateway-radios duplicate rf_chain_id %d", rf)
		}
		seen[rf] = struct{}{}
		radio := &api.GatewayRadio{
			RfChain:     rf,
			MaxTxPower:  float32(power),
		}
		if len(parts) == 3 {
			sens, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 32)
			if err != nil {
				return nil, fmt.Errorf("gateway-radios tx_sensitivity: %w", err)
			}
			v := float32(sens)
			radio.GainSensitivity = &v
		}
		out = append(out, radio)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("gateway-radios is empty")
	}
	return out, nil
}

func (p *ParsedGatewaySync) toUpsertRequest() *api.ReqGatewayUpsert {
	req := &api.ReqGatewayUpsert{}
	if p.GatewayID != "" {
		id := p.GatewayID
		req.GatewayId = &id
	}
	if p.ExternalID != "" {
		eid := p.ExternalID
		req.ExternalId = &eid
	}
	if len(p.EUI) > 0 {
		req.GatewayEui = p.EUI
	}
	if p.Name != "" {
		name := p.Name
		req.Name = &name
	}
	if p.Location != nil {
		req.Location = p.Location
	}
	if p.Radios != nil {
		req.Radios = &api.GatewayRadios{Values: p.Radios}
	}
	return req
}

// scheduleGatewayUpsert runs GatewayUpsert in a detached goroutine so proxying
// and the analytics flush loop are never blocked by the sync RPC.
func (f *AnalyticsForwarder) scheduleGatewayUpsert(parsed *ParsedGatewaySync) {
	if parsed == nil || !parsed.HasAny {
		return
	}
	req := parsed.toUpsertRequest()
	go func() {
		resp, err := f.client.GatewayUpsert(req)
		if err != nil {
			log.Warnf("Gateway upsert failed: %v", err)
			return
		}
		if resp != nil && resp.Applied {
			log.Infof("Gateway upsert applied (id=%s)", resp.GatewayId)
		} else if resp != nil {
			log.Infof("Gateway upsert acknowledged without mutation (id=%s)", resp.GatewayId)
		} else {
			log.Infof("Gateway upsert acknowledged without mutation")
		}
	}()
}
