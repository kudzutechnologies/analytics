package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParseGatewayID returns one exact 64-bit EUI representation, never a sanitized
// filename assembled from arbitrary input. ID6 has four 16-bit groups.
func ParseGatewayID(input string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	if strings.Contains(s, "-") {
		parts := strings.Split(s, "-")
		if len(parts) != 8 {
			return "", fmt.Errorf("invalid EUI")
		}
		for _, p := range parts {
			if len(p) != 2 {
				return "", fmt.Errorf("invalid EUI")
			}
		}
		s = strings.Join(parts, "")
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) == 8 {
			bytes := true
			for _, p := range parts {
				if len(p) != 2 {
					bytes = false
				}
			}
			if bytes {
				s = strings.Join(parts, "")
				parts = nil
			}
		}
		if parts != nil {
			var groups []string
			if strings.Count(s, "::") == 1 {
				sides := strings.Split(s, "::")
				left, right := []string{}, []string{}
				if sides[0] != "" {
					left = strings.Split(sides[0], ":")
				}
				if sides[1] != "" {
					right = strings.Split(sides[1], ":")
				}
				if len(left)+len(right) >= 4 {
					return "", fmt.Errorf("invalid compressed ID6")
				}
				groups = append(groups, left...)
				for len(groups) < 4-len(right) {
					groups = append(groups, "0")
				}
				groups = append(groups, right...)
			} else {
				groups = parts
			}
			if len(groups) != 4 {
				return "", fmt.Errorf("ID6 requires four groups")
			}
			s = ""
			for _, g := range groups {
				if len(g) < 1 || len(g) > 4 {
					return "", fmt.Errorf("invalid ID6 group")
				}
				n, e := strconv.ParseUint(g, 16, 16)
				if e != nil {
					return "", fmt.Errorf("invalid ID6")
				}
				s += fmt.Sprintf("%04x", n)
			}
		}
	}
	if len(s) != 16 {
		return "", fmt.Errorf("gateway EUI must encode 64 bits")
	}
	if _, e := hex.DecodeString(s); e != nil {
		return "", fmt.Errorf("invalid gateway EUI")
	}
	return s, nil
}
func NormalizeGatewayID(s string) string { id, _ := ParseGatewayID(s); return id }
func parseRouterJSON(raw json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return ParseGatewayID(s)
	}
	n, e := strconv.ParseUint(string(raw), 10, 64)
	if e != nil {
		return "", fmt.Errorf("invalid numeric router")
	}
	return fmt.Sprintf("%016x", n), nil
}
func (r *RouterInfoRequest) UnmarshalJSON(raw []byte) error {
	var m struct {
		Router json.RawMessage `json:"router"`
	}
	if e := json.Unmarshal(raw, &m); e != nil {
		return e
	}
	id, e := parseRouterJSON(m.Router)
	r.Router = id
	return e
}
