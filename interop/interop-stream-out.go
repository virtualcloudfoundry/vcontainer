package interop

import (
	"fmt"
	"os"
	"path/filepath"

	"code.cloudfoundry.org/lager"
	uuid "github.com/satori/go.uuid"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

func (c *containerInterop) DispatchStreamOutTask(outSpec *vcontainermodels.StreamOutSpec) (string, string, error) {
	c.logger.Info("container-interop-dispatch-stream-out-task", lager.Data{"handle": c.handle, "spec": outSpec})
	id, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return "", "", err
	}

	destFolderPath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getStreamOutFolder())
	mkdirCommand := RunTask{
		User: outSpec.User,
		Env:  []string{},
		Path: "mkdir",
		Args: []string{"-p", destFolderPath},
		Tag:  "mkdir",
	}

	err = c.scheduleTask(OneOffTask, &mkdirCommand, StreamOut)
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err, lager.Data{"cmd": mkdirCommand})
		return "", "", verrors.New("failed to schedule or wait for task exit.")
	}

	destFilePath := fmt.Sprintf("%s/%s", destFolderPath, id.String())
	syncCommand := RunTask{
		User: outSpec.User,
		Env:  []string{},
		Path: "tar",
		Args: []string{"-cf", destFilePath, outSpec.Path},
		Tag:  "tar-cf",
	}
	if err = c.scheduleTask(OneOffTask, &syncCommand, StreamOut); err != nil {
		c.logger.Error("container-interop-new-task-failed", err, lager.Data{"cmd": syncCommand})
		return "", "", verrors.New("failed to create task.")
	}
	c.logger.Info("container-interop-sync-command", lager.Data{"cmd": syncCommand})
	//err = c.WaitForTaskExit(syncCommand.ID)
	return syncCommand.ID, id.String(), nil
}

func (c *containerInterop) OpenStreamOutFile(fileId string) (*os.File, error) {
	c.logger.Info("container-interop-open-stream-out-file", lager.Data{"handle": c.handle, "file_id": fileId})
	filePath := filepath.Join(c.mountedPath, c.getSwapOutFolder(), c.getStreamOutFolder(), fileId)

	file, err := os.Open(filePath)
	if err != nil {
		c.logger.Error("container-interop-open-stream-out-file-failed", err, lager.Data{"handle": c.handle, "file_path": filePath})
		return nil, verrors.New("open-file-failed")
	}
	c.logger.Info("container-interop-open-stream-out-file-succeeded", lager.Data{"handle": c.handle, "file_path": filePath})
	return file, nil
}
