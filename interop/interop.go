package interop

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
	uuid "github.com/satori/go.uuid"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/helpers/mount"
	"github.com/virtualcloudfoundry/vcontainer/vstore"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

func NewContainerInterop(handle string, logger lager.Logger) ContainerInterop {
	return &containerInterop{
		handle: handle,
		logger: logger,
	}
}

type ContainerInterop interface {
	// prepare the container interop, return the entry point commands, and the volume/volume mount
	Prepare() (*ContainerInteropInfo, error)
	// prepare the swap root for the container
	Open() error
	// unmount the swap root folder in the host.
	Close() error
	// dispatch one command to the container.
	DispatchRunTask(cmd RunTask) (string, error)
	// copy the src folder to the prepared swap root. and write one task into it.
	DispatchStreamOutTask(outSpec *vcontainermodels.StreamOutSpec) (string, string, error)
	// open the file for read.
	OpenStreamOutFile(fileId string) (*os.File, error)
	// dispatch a copy folder task.
	DispatchFolderTask(src, dest string) (string, error)
	// prepare an file opened for writing, so the extract task can extract it to the dest folder in the container.
	PrepareExtractFile(dest string) (string, *os.File, error)
	// dispatch an extract file task.
	DispatchExtractFileTask(fileToExtract, dest, user string) (string, error)
	// wait for the task with the taskId exit.
	WaitForTaskExit(taskId string) error
	// get the pid from the task id.
	GetProcessIdFromTaskId(taskId string) (int64, error)
	// judge whether the task exited.
	TaskExited(taskId string) (vcontainermodels.WaitResponse, error)
	// clear the task related files.
	CleanTask(taskId string) error
}

type containerInterop struct {
	logger      lager.Logger
	handle      string
	mountedPath string
}

func (c *containerInterop) Open() error {
	c.logger.Info("container-interop-open", lager.Data{"handle": c.handle})
	mountedRootFolder, err := c.mountContainerRoot(c.handle)
	if err != nil {
		c.logger.Error("container-interop-mount-container-root-failed", err)
		return verrors.New("failed to mount container root.")
	}
	c.mountedPath = mountedRootFolder
	return nil
}

func (c *containerInterop) Close() error {
	c.logger.Info("container-interop-close", lager.Data{"handle": c.handle})
	mounter := mount.NewMounter(c.logger)
	err := mounter.Umount(c.mountedPath)
	if err != nil {
		c.logger.Error("container-interop-close-failed", err)
		return verrors.New("container-interop-close-failed")
	}

	err = os.Remove(c.mountedPath)
	if err != nil {
		c.logger.Error("container-interop-close-remove-mounted-path-failed", err)
	}
	return nil
}

func (c *containerInterop) scheduleTask(taskFolder string, cmd *RunTask, taskStep TaskStep) error {
	c.logger.Info("container-interop-schedule-command", lager.Data{"handle": c.handle, "cmd": cmd, "task_step": taskStep, "task_folder": taskFolder})
	fileId, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return err
	}
	taskId := fmt.Sprintf("%s_%s", cmd.Tag, fileId.String())
	cmd.ID = taskId
	taskFolderOuterFullPath := filepath.Join(c.mountedPath, taskFolder, string(taskStep))
	taskFolderInnerFullPath := filepath.Join(c.getSwapRoot(), taskFolder, string(taskStep))
	// make the task file can be sorted by time.
	taskNamePrefix := fmt.Sprintf("%d_%s", time.Now().UnixNano(), taskId)

	originFilePath := filepath.Join(taskFolderOuterFullPath, fmt.Sprintf("%s.sh", taskNamePrefix))
	envFilePath := filepath.Join(taskFolderOuterFullPath, fmt.Sprintf("%s.env", taskNamePrefix))
	envFileInnerPath := filepath.Join(taskFolderInnerFullPath, fmt.Sprintf("%s.env", taskNamePrefix))
	err = c.writeEnvFile(envFilePath, cmd.Env)
	if err != nil {
		c.logger.Error("container-interop-new-task-write-env-file-failed", err)
		return verrors.New("failed to create env file.")
	}
	tempFilePath := fmt.Sprintf("%s.temp", originFilePath)
	f, err := os.Create(tempFilePath)
	// TODO better error handling.
	if err != nil {
		c.logger.Error("container-interop-new-task-create-file-failed", err)
		return verrors.New("failed to create entry script.")
	}
	if f != nil {
		defer f.Close()
	}
	var buffer bytes.Buffer
	buffer.WriteString("#!/bin/bash\n")
	pidFilePath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.pid", taskId))

	// pre-processing the envs and the args.
	processedArgs := c.getProcessedArgs(cmd.Args)
	errorFilePath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.err", taskId))
	buffer.WriteString(fmt.Sprintf(`su - %s -c 'export HOME=/home/%s/app
source %s
export APP_ROOT=/home/%s/app
%s %s 2>%s & export CMD_PID=$!
echo $CMD_PID > %s
wait $CMD_PID
`, cmd.User, cmd.User, envFileInnerPath, cmd.User, cmd.Path,
		strings.Join(processedArgs, " "), errorFilePath, pidFilePath))
	// write the return code to the .exit file.
	buffer.WriteString(c.getTaskOutputScript(cmd))
	buffer.WriteString(`'
	`)
	_, err = f.WriteString(buffer.String())
	if err != nil {
		c.logger.Error("container-interop-new-task-write-failed", err)
		return verrors.New("failed to create entry script.")
	}
	err = f.Close()
	if err != nil {
		c.logger.Error("container-interop-new-task-file-close-failed", err)
		return verrors.New("failed to close file.")
	}
	err = os.Rename(tempFilePath, originFilePath)
	if err != nil {
		c.logger.Error("container-interop-new-task-file-rename-failed", err)
		return verrors.New("failed to rename file.")
	}
	return nil
}

func (c *containerInterop) scheduleAndWait(taskFolder string, cmd *RunTask, taskStep TaskStep) error {
	err := c.scheduleTask(taskFolder, cmd, taskStep)
	if err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return verrors.New("failed to create task.")
	}

	err = c.WaitForTaskExit(cmd.ID)
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err)
		return verrors.New("failed to dispatch folder task.")
	}
	return nil
}

func (c *containerInterop) writeEnvFile(filePath string, envs []string) error {
	var buffer bytes.Buffer
	buffer.WriteString("#!/bin/bash\n")
	processedEnvs := c.getProcessedEnvs(envs)
	buffer.WriteString(strings.Join(processedEnvs, "\n"))
	f, err := os.Create(filePath)
	// TODO better error handling.
	if f != nil {
		defer f.Close()
	}
	if err != nil {
		c.logger.Error("container-interop-new-task-create-env-file-failed", err)
		return verrors.New("failed to create env file.")
	}
	_, err = f.WriteString(buffer.String())
	if err != nil {
		return verrors.New("failed to write env file.")
	}
	return nil
}

func (c *containerInterop) getProcessedEnvs(envs []string) []string {
	processedEnvs := make([]string, len(envs))
	for i, _ := range envs {
		if strings.Contains(envs[i], "=") {
			firstEqualIndex := strings.Index(envs[i], "=")
			processedEnvs[i] = fmt.Sprintf("export %s='%s'", envs[i][:firstEqualIndex], envs[i][firstEqualIndex+1:])
		} else {
			processedEnvs[i] = fmt.Sprintf("export %s", envs[i])
		}
	}
	return processedEnvs
}

func (c *containerInterop) getProcessedArgs(args []string) []string {
	processedArgs := make([]string, len(args))
	for i, _ := range args {
		if args[i] == "" {
			processedArgs[i] = "\"\""
		} else {
			if strings.Contains(args[i], "=") {
				firstEqualIndex := strings.Index(args[i], "=")
				processedArgs[i] = fmt.Sprintf("%s=\"%s\"", args[i][:firstEqualIndex], args[i][firstEqualIndex+1:])
			} else {
				processedArgs[i] = args[i]
			}
		}
	}
	return processedArgs
}

func (c *containerInterop) getPidFilePath(taskId string) string {
	pidFilePath := filepath.Join(c.mountedPath, c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.pid", taskId))
	return pidFilePath
}

func (c *containerInterop) getTaskExitFilePath(taskId string) string {
	taskOutputPath := filepath.Join(c.mountedPath, c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.exit", taskId))
	return taskOutputPath
}

func (c *containerInterop) getTaskOutputScript(cmd *RunTask) string {
	taskOutputPath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.exit", cmd.ID))
	return fmt.Sprintf("echo $? > %s", taskOutputPath)
}

func (c *containerInterop) mountContainerRoot(handle string) (string, error) {
	c.logger.Info("container-interop-mount-container-root", lager.Data{"handle": handle})
	shareName := c.getContainerSwapRootShareFolder(handle)
	vs := vstore.NewVStore()
	// 1. prepare the volumes.
	// create share folder
	err := vs.CreateShareFolder(shareName)
	if err != nil {
		c.logger.Error("container-interop-mount-container-root-create-share-folder-failed-warn", err,
			lager.Data{"sharename": shareName, "handle": handle})
	}
	mountedRootFolder, err := ioutil.TempDir("/tmp", "folder_to_azure_")
	storageID := config.GetVContainerEnvInstance().ACIConfig.StorageId
	storageSecret := config.GetVContainerEnvInstance().ACIConfig.StorageSecret
	options := []string{
		"vers=3.0",
		fmt.Sprintf("username=%s", storageID),
		fmt.Sprintf("password=%s", storageSecret),
		"dir_mode=0777,file_mode=0777,serverino",
	}
	// TODO because 445 port is blocked in microsoft, so we use the proxy to do it...
	var azureFilePath string
	if proxyIP := config.GetVContainerEnvInstance().SMBProxy.IP; proxyIP != "" {
		options = append(options, fmt.Sprintf("port=%d", config.GetVContainerEnvInstance().SMBProxy.Port))
		azureFilePath = fmt.Sprintf("//%s/%s", proxyIP, shareName)
	} else {
		// TODO support other azure location.
		azureFilePath = fmt.Sprintf("//%s.file.core.windows.net/%s", storageID, shareName)
	}

	mounter := mount.NewMounter(c.logger)
	err = mounter.Mount(azureFilePath, mountedRootFolder, "cifs", options)
	if err != nil {
		c.logger.Error("container-interop-mount-container-root-mount-failed", err)
		return "", err
	}
	return mountedRootFolder, nil
}

func (c *containerInterop) getContainerSwapRootShareFolder(handle string) string {
	shareName := fmt.Sprintf("root-%s", handle)
	return shareName
}

// clear the task related files.
func (c *containerInterop) CleanTask(taskId string) error {
	return nil
}
