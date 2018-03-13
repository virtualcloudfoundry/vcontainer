package mount // import "github.com/virtualcloudfoundry/vcontainer/helpers/mount"
import (
	"fmt"
	"os/exec"
	"strings"

	"code.cloudfoundry.org/lager"
)

type MounterLinux struct {
	// mounterPath string
	logger lager.Logger
}

func (mounter *MounterLinux) Mount(source string, target string, fstype string, options []string) error {
	// fsTypesNeedMounter := helpers.NewString("nfs", "glusterfs", "ceph", "cifs")
	// if fsTypesNeedMounter.Has(fstype) {
	// 	mounter.mounterPath = mounter.mounterPath
	// }
	mounter.logger.Info("mounter-linux-mount", lager.Data{"source": source, "target": target})
	return mounter.doMount(defaultMountCommand, source, target, fstype, options)
}

func (mounter *MounterLinux) doMount(mountCmd string, source string, target string, fstype string, options []string) error {
	mountArgs := makeMountArgs(source, target, fstype, options)
	// if len(mounterPath) > 0 {
	// 	mountArgs = append([]string{mountCmd}, mountArgs...)
	// 	mountCmd = mounterPath
	// }
	withSudo := make([]string, len(mountArgs)+1)
	// withSudo[0] = "sudo"
	withSudo[0] = mountCmd
	copy(withSudo[1:], mountArgs)
	mountScript := strings.Join(withSudo, " ")
	// finalScript := strings.Join([]string{"-c"}, " ")
	// withSudo = append(withSudo, mountArgs)
	command := exec.Command("sh", "-c", mountScript)
	output, err := command.CombinedOutput()
	if err != nil {
		// args := strings.Join(mountArgs, " ")
		// glog.Errorf("Mount failed: %v\nMounting command: %s\nMounting arguments: %s\nOutput: %s\n", err, mountCmd, args, string(output))
		return fmt.Errorf("mount failed: %v\nMounting command: %s\nMounting arguments: %s\nOutput: %s\n",
			err, mountCmd, mountScript, string(output))
	}
	return err
}

func (mounter *MounterLinux) Umount(target string) error {
	command := exec.Command("umount", target)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Umount failed: %v\nUmounting arguments: %s\nOutput: %s\n", err, target, string(output))
	}
	return nil
}

// sudo mount -t cifs //<storage-account-name>.file.core.windows.net/<share-name> <mount-point>
// -o vers=<smb-version>,username=<storage-account-name>,password=<storage-account-key>,dir_mode=0777,file_mode=0777
// makeMountArgs makes the arguments to the mount(8) command.
func makeMountArgs(source, target, fstype string, options []string) []string {
	// Build mount command as follows:
	//   mount [-t $fstype] [-o $options] [$source] $target
	mountArgs := []string{}
	if len(fstype) > 0 {
		mountArgs = append(mountArgs, "-t", fstype)
	}
	if len(source) > 0 {
		mountArgs = append(mountArgs, source)
	}
	mountArgs = append(mountArgs, target)

	if len(options) > 0 {
		mountArgs = append(mountArgs, "-o", strings.Join(options, ","))
	}
	return mountArgs
}

// func NewMounterLinux(mounterPath string) Interface {
// 	return &MounterLinux{
// 		mounterPath: mounterPath,
// 	}
// }
