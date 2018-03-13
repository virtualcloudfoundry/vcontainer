package mount

import (
	"runtime"

	"code.cloudfoundry.org/lager"
)

func NewMounter(logger lager.Logger) Interface {
	var mounter Interface
	if runtime.GOOS == "linux" {
		mounter = &MounterLinux{
			logger: logger,
		}
	} else if runtime.GOOS == "windows" {
		mounter = nil
	}
	return mounter
}
