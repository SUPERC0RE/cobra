//go:build darwin

package machineid

import (
	"os/exec"
	"strings"
)

// rawID returns the host's hardware UUID and serial number from
// IOPlatformExpertDevice, both unique per Mac and stable across OS reinstalls.
// ioreg is invoked by absolute path and without privileges, so a shim earlier
// in PATH cannot spoof the identity.
func rawID() string {
	out, err := exec.Command("/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	uuid := ioregValue(string(out), "IOPlatformUUID")
	serial := ioregValue(string(out), "IOPlatformSerialNumber")
	switch {
	case uuid != "" && serial != "":
		return uuid + "|" + serial
	case uuid != "":
		return uuid
	case serial != "":
		return serial
	}
	return ""
}

// ioregValue extracts the quoted value following the given key in ioreg output.
func ioregValue(s, key string) string {
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	eq := strings.Index(rest, "=")
	if eq < 0 {
		return ""
	}
	rest = rest[eq+1:]
	open := strings.Index(rest, `"`)
	if open < 0 {
		return ""
	}
	rest = rest[open+1:]
	close := strings.Index(rest, `"`)
	if close < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:close])
}
