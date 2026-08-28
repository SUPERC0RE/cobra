package firefox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bwplus/types"
)

// Browser is one Firefox installation: the Profiles directory holding one or
// more profiles. Firefox keys are per-profile (each profile's key4.db), so the
// installation does not implement KeyManager.
type Browser struct {
	cfg      types.BrowserConfig
	profiles []*profile
}

// NewBrowser discovers the Firefox profiles under cfg.UserDataDir and returns
// the installation, or nil if no profile with resolvable sources exists.
// Firefox profile directories have random names (e.g. "97nszz88.default-release");
// any subdirectory containing known data files is treated as a valid profile.
func NewBrowser(cfg types.BrowserConfig) (*Browser, error) {
	var profiles []*profile
	for _, profileDir := range discoverProfiles(cfg.UserDataDir, firefoxSources) {
		sourcePaths := resolveSourcePaths(firefoxSources, profileDir)
		if len(sourcePaths) == 0 {
			continue
		}
		profiles = append(profiles, &profile{
			profileDir:  profileDir,
			browserName: cfg.Name,
			sourcePaths: sourcePaths,
		})
	}
	if len(profiles) == 0 {
		return nil, nil
	}
	return &Browser{cfg: cfg, profiles: profiles}, nil
}

func (b *Browser) BrowserName() string { return b.cfg.Name }
func (b *Browser) UserDataDir() string { return b.cfg.UserDataDir }

// Profiles returns the identity of every profile in this installation.
func (b *Browser) Profiles() []types.Profile {
	out := make([]types.Profile, 0, len(b.profiles))
	for _, p := range b.profiles {
		out = append(out, types.Profile{Name: p.name(), Dir: p.profileDir})
	}
	return out
}

// Extract extracts every profile, deriving each profile's key independently.
func (b *Browser) Extract(categories []types.Category) ([]types.ExtractResult, error) {
	results := make([]types.ExtractResult, 0, len(b.profiles))
	for _, p := range b.profiles {
		results = append(results, types.ExtractResult{
			Profile: types.Profile{Name: p.name(), Dir: p.profileDir},
			Data:    p.extract(categories),
		})
	}
	return results, nil
}

// CountEntries counts entries per category for every profile without decryption.
func (b *Browser) CountEntries(categories []types.Category) ([]types.CountResult, error) {
	results := make([]types.CountResult, 0, len(b.profiles))
	for _, p := range b.profiles {
		results = append(results, types.CountResult{
			Profile: types.Profile{Name: p.name(), Dir: p.profileDir},
			Counts:  p.count(categories),
		})
	}
	return results, nil
}

// retrieveMasterKey reads key4.db and derives the master key using NSS.
// If loginsPath is non-empty, the derived key is validated against actual
// login data to ensure the correct candidate is selected.
func retrieveMasterKey(key4Path, loginsPath string) ([]byte, error) {
	k4, err := readKey4DB(key4Path)
	if err != nil {
		return nil, err
	}

	keys, err := k4.deriveKeys()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("no valid master key candidates in key4.db")
	}

	// No logins to validate against 鈥?return the first derived key.
	if loginsPath == "" {
		return keys[0], nil
	}

	// Validate against actual login data.
	if key := validateKeyWithLogins(keys, loginsPath); key != nil {
		return key, nil
	}

	return nil, fmt.Errorf("derived %d key(s) but none could decrypt logins", len(keys))
}

// resolvedPath holds the absolute path and type for a discovered source.
type resolvedPath struct {
	absPath string
	isDir   bool
}

// discoverProfiles lists the profile directories for a Firefox installation.
// Candidates come from two sources, deduplicated:
//   - direct subdirectories of userDataDir that contain a known data file;
//   - profiles.ini entries, which also cover profiles stored at custom
//     absolute paths (IsRelative=0) that never live under the standard dir.
func discoverProfiles(userDataDir string, sources map[types.Category][]sourcePath) []string {
	var profiles []string
	seen := make(map[string]bool)

	add := func(dir string) {
		dir = filepath.Clean(dir)
		if seen[dir] {
			return
		}
		if !hasAnySource(sources, dir) {
			return
		}
		seen[dir] = true
		profiles = append(profiles, dir)
	}

	if entries, err := os.ReadDir(userDataDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				add(filepath.Join(userDataDir, e.Name()))
			}
		}
	}

	for _, dir := range profilesFromIni(userDataDir) {
		add(dir)
	}

	return profiles
}

// profilesFromIni parses Firefox's profiles.ini and returns the absolute paths
// of the profiles it declares. The file sits next to the profile directories
// on Linux/Windows (~/.mozilla/firefox/profiles.ini) and in their parent on
// macOS (.../Firefox/profiles.ini beside the Profiles dir), so both are tried.
func profilesFromIni(userDataDir string) []string {
	var iniPath string
	for _, cand := range []string{
		filepath.Join(userDataDir, "profiles.ini"),
		filepath.Join(filepath.Dir(userDataDir), "profiles.ini"),
	} {
		if _, err := os.Stat(cand); err == nil {
			iniPath = cand
			break
		}
	}
	if iniPath == "" {
		return nil
	}
	raw, err := os.ReadFile(iniPath)
	if err != nil {
		return nil
	}
	baseDir := filepath.Dir(iniPath)

	var profiles []string
	var rel string
	relative, inProfile := true, false

	flush := func() {
		if !inProfile || rel == "" {
			return
		}
		if relative {
			profiles = append(profiles, filepath.Clean(filepath.Join(baseDir, rel)))
		} else {
			profiles = append(profiles, filepath.Clean(rel))
		}
	}

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			inProfile = strings.HasPrefix(section, "Profile")
			rel = ""
			relative = true
			continue
		}
		if !inProfile {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Path":
			rel = strings.TrimSpace(val)
		case "IsRelative":
			// "0" means the Path is absolute; anything else (incl. absent,
			// e.g. an IsRelative=1 entry) is treated as relative.
			relative = strings.TrimSpace(val) != "0"
		}
	}
	flush()
	return profiles
}

// hasAnySource checks if dir contains at least one source file or directory.
func hasAnySource(sources map[types.Category][]sourcePath, dir string) bool {
	for _, candidates := range sources {
		for _, sp := range candidates {
			abs := filepath.Join(dir, sp.rel)
			if _, err := os.Stat(abs); err == nil {
				return true
			}
		}
	}
	return false
}

// resolveSourcePaths checks which sources actually exist in profileDir.
// Candidates are tried in priority order; the first existing path wins.
func resolveSourcePaths(sources map[types.Category][]sourcePath, profileDir string) map[types.Category]resolvedPath {
	resolved := make(map[types.Category]resolvedPath)
	for cat, candidates := range sources {
		for _, sp := range candidates {
			abs := filepath.Join(profileDir, sp.rel)
			info, err := os.Stat(abs)
			if err != nil {
				continue
			}
			if sp.isDir == info.IsDir() {
				resolved[cat] = resolvedPath{abs, sp.isDir}
				break
			}
		}
	}
	return resolved
}

// Firefox uses three timestamp units. Helpers emit UTC and return the zero
// time.Time for non-positive or out-of-JSON-range input.
//
//   - firefoxMicros: PRTime (渭s since Unix epoch) 鈥?moz_* tables.
//   - firefoxMillis: Date.now() (ms) 鈥?logins.json, download endTime.
//   - firefoxSeconds: seconds 鈥?moz_cookies.expiry only.
func firefoxMicros(us int64) time.Time {
	if us <= 0 {
		return time.Time{}
	}
	return clampJSON(time.UnixMicro(us).UTC())
}

func firefoxMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return clampJSON(time.UnixMilli(ms).UTC())
}

func firefoxSeconds(s int64) time.Time {
	if s <= 0 {
		return time.Time{}
	}
	return clampJSON(time.Unix(s, 0).UTC())
}

// clampJSON maps years outside time.Time.MarshalJSON's [1, 9999] window
// to the zero time, so JSON export can't crash on sentinel inputs.
func clampJSON(t time.Time) time.Time {
	if t.Year() < 1 || t.Year() > 9999 {
		return time.Time{}
	}
	return t
}
