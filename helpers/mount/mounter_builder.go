package mount

import "runtime"

func NewMounter() Interface {
	var mounter Interface
	if runtime.GOOS == "linux" {
		mounter = &MounterLinux{}
	} else if runtime.GOOS == "windows" {
		mounter = nil
	}
	return mounter
}
