//go:build windows

package machineid

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

// rawID returns a hardware identifier built from the motherboard, CPU and
// first disk serials queried via WMI (PowerShell runs as the current user, no
// admin needed). If the query fails or returns nothing it falls back to the
// MachineGuid installed at setup time.
func rawID() string {
	if s := hardwareID(); s != "" {
		return s
	}
	return machineGuid()
}

// machineGuid returns the MachineGuid value installed at OS setup time; it is
// unique per Windows installation and readable without admin.
func machineGuid() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return guid
}

// hardwareID runs a single PowerShell command that concatenates the baseboard
// serial, CPU id and first disk serial 鈥?the identifiers that survive an OS
// reinstall 鈥?and returns it. A timeout guards against a hung PowerShell.
func hardwareID() string {
	const script = `$b=(Get-CimInstance Win32_BaseBoard).SerialNumber;$p=(Get-CimInstance Win32_Processor).ProcessorId;$d=(Get-CimInstance Win32_DiskDrive|Select-Object -First 1).SerialNumber;"$b$p$d"`
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
