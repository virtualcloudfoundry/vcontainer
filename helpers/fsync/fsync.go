package fsync // import "github.com/virtualcloudfoundry/vcontainer/helpers/fsync"
import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"code.cloudfoundry.org/lager"
)

type FSync interface {
	CopyFolder(src, dest string) error
	WriteToFile(reader io.Reader, dest string) error
}

type fSync struct {
	logger lager.Logger
}

const (
	// Default mount command if mounter path is not specified
	defaultRsyncCommand string = "rsync"
)

func NewFSync(logger lager.Logger) FSync {
	return &fSync{
		logger: logger,
	}
}

func (f *fSync) WriteToFile(reader io.Reader, dest string) error {
	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, reader)
	return err
}

func (f *fSync) CopyFolder(src, dest string) error {
	return f.doRsync(defaultRsyncCommand, src, dest)
}

func (f *fSync) doRsync(rsyncCommand, src, dest string) error {
	err := os.MkdirAll(dest, os.ModeDir)
	if err != nil {
		f.logger.Info("fsync-dorsync-mkdir-failed.", lager.Data{"dest": dest})
		return err
	}
	rsyncArgs := makeRsyncArgs(fmt.Sprintf("%s/", src), dest)
	command := exec.Command("rsync", rsyncArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		args := strings.Join(rsyncArgs, " ")
		// glog.Errorf("Mount failed: %v\nMounting command: %s\nMounting arguments: %s\nOutput: %s\n", err, mountCmd, args, string(output))
		return fmt.Errorf("rsync failed: %v\nRsync command: %s\nRsync arguments: %s\nOutput: %s\n",
			err, rsyncCommand, args, string(output))
	}
	f.logger.Info("fsync-dorsync-succeed", lager.Data{"src": src, "dest": dest})
	return err
}

// makeMountArgs makes the arguments to the mount(8) command.
func makeRsyncArgs(source, target string) []string {
	mountArgs := []string{}
	mountArgs = append(mountArgs, "-a", source, target)
	return mountArgs
}
