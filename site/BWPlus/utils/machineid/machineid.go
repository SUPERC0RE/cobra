// Package machineid returns a stable, unique hardware-based identifier for
// the current machine. The value is used as the basename for encrypted output
// archives so that data from different hosts never collides, and to identify
// the host across OS reinstalls.
package machineid

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"strings"
)

// ID returns the machine identifier as a short lowercase hex string.
//
// The value is built from whichever identifiers can be read WITHOUT elevated
// privileges, so it works for a normal user process:
//   - platform hardware identifiers (Linux SMBIOS/DMI + machine-id + internal
//     disk serials, macOS IOPlatformUUID, Windows baseboard/CPU/disk serials);
//   - the MAC address of the first usable network interface, which is always
//     readable and unique per machine 鈥?this keeps the identifier distinct even
//     across cloned systems or duplicated machine-ids.
//
// A SHA-256 digest is used so the value is safe as a filename on every OS; only
// the hostname remains as a last resort so the result is never empty.
func ID() string {
	parts := make([]string, 0, 2)
	if raw := strings.TrimSpace(rawID()); raw != "" {
		parts = append(parts, raw)
	}
	if mac := strings.TrimSpace(macID()); mac != "" {
		parts = append(parts, mac)
	}
	if len(parts) == 0 {
		if h, _ := os.Hostname(); h != "" {
			parts = append(parts, strings.TrimSpace(h))
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])[:16]
}

// macID returns the MAC address of the first usable non-loopback, up network
// interface. It is readable without privileges and unique per machine, so it
// keeps the identifier distinct even when OS machine-ids are duplicated.
func macID() string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagUp == 0 {
			continue
		}
		if len(i.HardwareAddr) == 0 {
			continue
		}
		return i.HardwareAddr.String()
	}
	return ""
}
