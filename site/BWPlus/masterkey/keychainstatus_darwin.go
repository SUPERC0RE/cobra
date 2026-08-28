//go:build darwin

package masterkey

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// sessionProbeTimeout bounds each probe command (launchctl / osascript), which
// otherwise could hang a silent background run.
const sessionProbeTimeout = 4 * time.Second

var unlockProbe struct {
	once sync.Once
	ok   bool
}

// keychainCanBeQueriedSilently reports whether the login keychain is (a) inside
// a logged-in GUI session and (b) currently unlocked. Only then does the
// `security` CLI return secrets without popping an authorization dialog. The
// probe itself is read-only (SecKeychainGetStatus) and never shows UI; any
// uncertainty yields false so the caller skips rather than risks a prompt.
func keychainCanBeQueriedSilently() bool {
	unlockProbe.once.Do(func() {
		unlockProbe.ok = inAquaSession() && loginKeychainUnlocked()
	})
	return unlockProbe.ok
}

// inAquaSession reports whether the process runs inside a logged-in GUI user
// session (launchd "Aqua"). Outside one, `security` cannot prompt anyway and
// would just fail, so the caller bails out early and quietly.
func inAquaSession() bool {
	ctx, cancel := context.WithTimeout(context.Background(), sessionProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/launchctl", "managername").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Aqua"
}

// loginKeychainUnlocked inspects the default keychain's status through the
// Security framework via a short JXA/ObjC bridge probe. SecKeychainGetStatus
// only reads a status bit and never triggers an authorization dialog. Any error
// in the probe degrades to false ("stay silent") so a broken environment can
// never turn into a visible prompt.
func loginKeychainUnlocked() bool {
	// kSecUnlockStateStatus is bit 0x1; SecKeychainCopyDefault resolves the
	// user's login keychain without needing the on-disk path as a C string.
	script := `
ObjC.import('Security');
var kc = Ref(), st;
if ((st = $.SecKeychainCopyDefault(kc)) !== 0) { 'err'; }
var status = Ref();
if ($.SecKeychainGetStatus(kc[0], status) !== 0) { 'err'; }
(status[0] & 1) !== 0 ? 'unlocked' : 'locked';
`
	ctx, cancel := context.WithTimeout(context.Background(), sessionProbeTimeout+time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", script).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "unlocked"
}