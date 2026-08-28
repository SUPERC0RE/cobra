//go:build linux

package machineid

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rawID returns a composite hardware identifier built from the SMBIOS/DMI
// UUID, motherboard/system serials, the systemd machine-id and the serials of
// internal (non-removable) disks. These values are stable per physical machine
// and survive OS reinstalls.
func rawID() string {
	var parts []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}

	for _, p := range []string{
		"/sys/class/dmi/id/product_uuid",
		"/sys/devices/virtual/dmi/id/product_uuid",
		"/sys/class/dmi/id/board_serial",
		"/sys/class/dmi/id/product_serial",
	} {
		if b, err := os.ReadFile(p); err == nil {
			add(string(b))
		}
	}

	add(machineID())

	for _, s := range diskSerials() {
		add(s)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "|")
}

// machineID reads the systemd/DBus machine-id.
func machineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// diskSerials returns "dev:serial" for every internal (non-removable) block
// device that exposes a serial number, sorted so the result is deterministic.
func diskSerials() []string {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dev := e.Name()
		// Skip removable drives (USB sticks, card readers) so the identifier
		// does not change when external media is plugged in.
		if b, err := os.ReadFile(filepath.Join("/sys/block", dev, "removable")); err == nil && strings.TrimSpace(string(b)) == "1" {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/sys/block", dev, "device", "serial"))
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			out = append(out, dev+":"+s)
		}
	}
	sort.Strings(out)
	return out
}
