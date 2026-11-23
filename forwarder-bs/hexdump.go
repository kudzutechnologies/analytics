package forwarderbs

import (
	"fmt"
	"strings"
)

const hexdumpLineBytes = 16

// Hexdump formats data as canonical hex+ASCII (max maxBytes bytes, 0 = no limit).
// Each line: offset (8 hex) + 16 bytes hex + 16 bytes ASCII; non-printable shown as '.'.
func Hexdump(prefix string, data []byte, maxBytes int) string {
	if len(data) == 0 {
		return prefix + "(empty)\n"
	}
	origLen := len(data)
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
	}
	var b strings.Builder
	for i := 0; i < len(data); i += hexdumpLineBytes {
		chunk := data[i:]
		if len(chunk) > hexdumpLineBytes {
			chunk = chunk[:hexdumpLineBytes]
		}
		fmt.Fprintf(&b, "%s%08x  ", prefix, i)
		for j := 0; j < hexdumpLineBytes; j++ {
			if j < len(chunk) {
				fmt.Fprintf(&b, "%02x ", chunk[j])
			} else {
				b.WriteString("   ")
			}
		}
		b.WriteString(" |")
		for _, c := range chunk {
			if c >= 32 && c < 127 {
				b.WriteByte(c)
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteString("|\n")
	}
	if maxBytes > 0 && origLen > maxBytes {
		fmt.Fprintf(&b, "%s... (truncated from %d to %d bytes)\n", prefix, origLen, maxBytes)
	}
	return b.String()
}
