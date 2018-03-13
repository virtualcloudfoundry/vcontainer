package interop

import (
	"fmt"
	"os"
	"path/filepath"

	"code.cloudfoundry.org/lager"
	uuid "github.com/satori/go.uuid"
	"github.com/virtualcloudfoundry/vcontainer/helpers/fsync"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

func (c *containerInterop) DispatchFolderTask(src, dst string) (string, error) {
	c.logger.Info("container-interop-dispatch-folder-task", lager.Data{"handle": c.handle, "src": src, "dst": dst})
	fsync := fsync.NewFSync(c.logger)
	relativePath := filepath.Join(c.getSwapInFolder(), dst)
	targetFolder := filepath.Join(c.mountedPath, relativePath)
	err := fsync.CopyFolder(src, targetFolder)
	if err != nil {
		c.logger.Error("container-interop-copy-folder-failed", err, lager.Data{"src": src, "dest": dst})
		return "", verrors.New("failed to copy folder.")
	}
	srcFolderPath := filepath.Join(c.getSwapRoot(), relativePath)
	destFolderPath := dst

	mkdirCommand := RunTask{
		User: "root",
		Env:  []string{},
		Path: "mkdir",
		Args: []string{"-p", destFolderPath},
		Tag:  "mkdir",
	}

	err = c.scheduleTask(OneOffTask, &mkdirCommand, StreamIn)
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err)
		return "", verrors.New("failed to schedule or wait for task exit.")
	}

	// chmod
	chmodCommand := RunTask{
		User: "root",
		Env:  []string{},
		Path: "chmod",
		Args: []string{"777", destFolderPath},
		Tag:  "chmod",
	}

	err = c.scheduleTask(OneOffTask, &chmodCommand, StreamIn)
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err)
		return "", verrors.New("failed to change permission.")
	}

	syncCommand := RunTask{
		User: "root",
		Env:  []string{},
		Path: "rsync",
		Args: []string{"-a", fmt.Sprintf("%s/", srcFolderPath), destFolderPath},
		Tag:  "rsync",
	}

	if err = c.scheduleTask(OneOffTask, &syncCommand, StreamIn); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return syncCommand.ID, nil
}

// prepare the task
func (c *containerInterop) PrepareExtractFile(dest string) (string, *os.File, error) {
	c.logger.Info("container-interop-prepare-extract-file", lager.Data{"handle": c.handle})
	id, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return "", nil, err
	}
	fileToExtractName := fmt.Sprintf("extract_%s", id.String())
	filePath := filepath.Join(c.mountedPath, c.getSwapInFolder(), fileToExtractName)
	file, err := os.Create(filePath)

	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err, lager.Data{"filepath": filePath})
		return "", nil, err
	}
	return fileToExtractName, file, nil
}

// dest should be full path.
func (c *containerInterop) DispatchExtractFileTask(fileToExtractName, dest, user string) (string, error) {
	c.logger.Info("container-interop-dispatch-extract-file-task", lager.Data{"handle": c.handle})
	// prepare the extract target folder first.
	mkdirCommand := RunTask{
		User: "root",
		Env:  []string{},
		Path: "mkdir",
		Args: []string{"-p", dest},
		Tag:  "mkdir",
	}

	err := c.scheduleTask(OneOffTask, &mkdirCommand, StreamIn)
	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err)
		return "", verrors.New("failed to schedule or wait for task exit.")
	}

	// chmod
	chmodCommand := RunTask{
		User: "root",
		Env:  []string{},
		Path: "chmod",
		Args: []string{"777", dest},
		Tag:  "chmod",
	}

	err = c.scheduleTask(OneOffTask, &chmodCommand, StreamIn)
	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err)
		return "", verrors.New("failed to change permission.")
	}

	extractCmd := RunTask{
		User: user,
		Env:  []string{},
		Path: "tar",
		Args: []string{"-C", dest, "-xf", filepath.Join(c.getSwapRoot(), c.getSwapInFolder(), fileToExtractName)},
		Tag:  "tar",
	}

	if err := c.scheduleTask(OneOffTask, &extractCmd, StreamIn); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return extractCmd.ID, nil
}
