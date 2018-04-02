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
	"github.com/virtualcloudfoundry/goaci/aci"
	"github.com/virtualcloudfoundry/vcontainer/common"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/helpers/fsync"
	"github.com/virtualcloudfoundry/vcontainer/helpers/mount"
	"github.com/virtualcloudfoundry/vcontainer/vstore"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

// components to do interop with the container.
// 1. one interop root folder to interop with the container.
// 2. one components managing the tasks dispatched to the the containers
// 	  a. one tasks folder ends with .task
// 3. one daemon to interate with the tasks.

type Priority int32

const (
	StreamIn Priority = 0
	Run      Priority = 1
)

type RunCommand struct {
	ID   string
	User string
	Env  []string
	Path string
	Args []string
}

func NewContainerInterop(handle string, logger lager.Logger) ContainerInterop {
	return &containerInterop{
		handle: handle,
		logger: logger,
	}
}

type ContainerInteropInfo struct {
	Cmd          []string
	VolumeMounts []aci.VolumeMount
	Volumes      []aci.Volume
}

type ContainerInterop interface {
	// prepare the container interop, return the entry point commands, and the volume/volume mount
	Prepare() (*ContainerInteropInfo, error)
	// prepare the swap root for the container
	Open() error
	// unmount the swap root folder in the host.
	Close() error
	// dispatch one command to the container.
	DispatchRunCommand(cmd RunCommand) (string, error)
	// dispatch one task containing a run command array.
	// DispatchRunTask(task RunTask) (string, error)
	// copy the src folder to the prepared swap root. and write one task into it.
	DiskpatchFolderTask(src, dest string) (string, error)
	// prepare an file opened for writing, so the extract task can extract it to the dest folder in the container.
	PrepareExtractFile(dest string) (string, *os.File, error)
	// dispatch an extract file task.
	DispatchExtractFileTask(fileToExtract, dest, user string) (string, error)
	// wait for the task with the taskId exit.
	WaitForTaskExit(taskId string) error
}

type containerInterop struct {
	logger      lager.Logger
	handle      string
	mountedPath string
}

func (c *containerInterop) Prepare() (*ContainerInteropInfo, error) {
	interopInfo := &ContainerInteropInfo{}

	var volumeMounts []aci.VolumeMount
	var volumes []aci.Volume
	shareName := c.getContainerSwapRootShareFolder(c.handle)

	azureFile := &aci.AzureFileVolume{
		ReadOnly:           false,
		ShareName:          shareName,
		StorageAccountName: config.GetVContainerEnvInstance().ACIConfig.StorageId,
		StorageAccountKey:  config.GetVContainerEnvInstance().ACIConfig.StorageSecret,
	}

	volume := aci.Volume{
		Name:      shareName,
		AzureFile: azureFile,
	}

	volumes = append(volumes, volume)

	volumeMount := aci.VolumeMount{
		Name:      shareName,
		MountPath: common.GetSwapRoot(),
		ReadOnly:  false,
	}
	volumeMounts = append(volumeMounts, volumeMount)
	interopInfo.VolumeMounts = volumeMounts
	interopInfo.Volumes = volumes

	f, err := os.Create(filepath.Join(c.mountedPath, c.getEntryScript()))
	f.WriteString(c.getEntryScriptContent())
	if err != nil {
		return nil, verrors.New("failed to create entry script.")
	}

	if f != nil {
		f.Close()
	}

	var cmd []string
	cmd = append(cmd, "/bin/bash")
	cmd = append(cmd, "-c")

	var prepareScript = fmt.Sprintf(`
cat %s/%s
%s/%s
`, c.getSwapRoot(), c.getEntryScript(), c.getSwapRoot(), c.getEntryScript())

	cmd = append(cmd, prepareScript)
	interopInfo.Cmd = cmd
	return interopInfo, nil
}

func (c *containerInterop) getSwapInFolder() string {
	return "./in"
}

// out is the root folder of the output.
func (c *containerInterop) getSwapOutFolder() string {
	return "./out"
}

func (c *containerInterop) getTaskOutputFolder() string {
	return "tasks"
}

func (c *containerInterop) getSwapRoot() string {
	return "/swaproot"
}

func (c *containerInterop) getVCapScript() string {
	return "vcap_task.sh"
}

func (c *containerInterop) getEntryScript() string {
	return "root_task.sh"
}

func (c *containerInterop) getOneOffTaskFolder() string {
	return "one_off_tasks"
}

func (c *containerInterop) getConstantTaskFolder() string {
	return "constant_tasks"
}

func (c *containerInterop) getEntryScriptContent() string {
	entryScript := fmt.Sprintf(`#!/bin/bash
	constant_task_executor() {
		FILES=%s/%s/%d/*
		for f in $FILES
		do
			echo "Executing constant task file: $f ...\n"
			cat $f
			bash -c $f
		done

		FILES=%s/%s/%d/*
		for f in $FILES
		do
			echo "Executing constant task file: $f ...\n"
			cat $f
			bash -c $f
		done
	}
	one_off_task_daemon() {
		while true; do
			sleep 5
			# execute the high priority jobs first
			echo "i am alive"
			FILES=%s/%s/%d/*.sh
			for f in $FILES
			do
				if [ -f $f ]
				then
					echo "Executing one off task file $f ...\n"
					cat $f
					bash -c $f
					sleep 1 # give it one second to be executed.
					echo "Removing $f.\n"
					mv $f $f.executed
					echo "Removed $f.\n"
				fi
			done

			FILES=%s/%s/%d/*.sh
			for f in $FILES
			do
				if [ -f $f ]
				then
					echo "Executing one off task file $f ...\n"
					cat $f
					bash -c $f &
					sleep 1 # give it one second to be executed.
					echo "Removing $f.\n"
					mv $f $f.executed
					echo "Removed $f.\n"
				fi
			done
		done
	}
	constant_task_executor
	one_off_task_daemon
	`, c.getSwapRoot(), c.getConstantTaskFolder(), StreamIn,
		c.getSwapRoot(), c.getConstantTaskFolder(), Run,
		c.getSwapRoot(), c.getOneOffTaskFolder(), StreamIn,
		c.getSwapRoot(), c.getOneOffTaskFolder(), Run,
	)
	return entryScript
}

func (c *containerInterop) WaitForTaskExit(taskId string) error {
	time.Sleep(time.Second * 30) // mock for 30 seconds now.
	return nil                   //verrors.New("not implemented.")
}

func (c *containerInterop) Open() error {
	mountedRootFolder, err := c.mountContainerRoot(c.handle)
	if err != nil {
		c.logger.Error("container-interop-open-failed", err)
		return verrors.New("failed to mount container root.")
	}
	c.mountedPath = mountedRootFolder
	return nil
}

func (c *containerInterop) Close() error {
	mounter := mount.NewMounter()
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

func (c *containerInterop) DispatchRunCommand(cmd RunCommand) (string, error) {
	if err := c.scheduleCommand(c.getOneOffTaskFolder(), &cmd, Run); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return cmd.ID, nil
}

func (c *containerInterop) DiskpatchFolderTask(src, dst string) (string, error) {
	fsync := fsync.NewFSync(c.logger)
	relativePath := fmt.Sprintf("%s/%s", c.getSwapInFolder(), dst)
	targetFolder := filepath.Join(c.mountedPath, relativePath)
	err := fsync.CopyFolder(src, targetFolder)
	if err != nil {
		c.logger.Error("container-interop-copy-folder-failed", err, lager.Data{"src": src, "dest": dst})
		return "", verrors.New("failed to copy folder.")
	}
	srcFolderPath := filepath.Join(common.GetSwapRoot(), relativePath)
	destFolderPath := dst

	mkdirCommand := RunCommand{
		Path: "mkdir",
		Args: []string{"-p", destFolderPath},
	}

	if err = c.scheduleCommand(c.getConstantTaskFolder(), &mkdirCommand, StreamIn); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	err = c.WaitForTaskExit(mkdirCommand.ID)
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err)
		return "", verrors.New("failed to dispatch folder task.")
	}

	syncCommand := RunCommand{
		Path: "rsync",
		Args: []string{"-a", fmt.Sprintf("%s/", srcFolderPath), destFolderPath},
	}

	if err = c.scheduleCommand(c.getConstantTaskFolder(), &syncCommand, StreamIn); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return syncCommand.ID, nil
}

// prepare the task
func (c *containerInterop) PrepareExtractFile(dest string) (string, *os.File, error) {
	id, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return "", nil, err
	}
	fileToExtractName := fmt.Sprintf("extract_%s", id.String())
	filePath := filepath.Join(c.mountedPath, c.getSwapInFolder(), fileToExtractName)
	file, err := os.Create(filePath)

	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err)
		return "", nil, err
	}
	return fileToExtractName, file, nil
}

func (c *containerInterop) DispatchExtractFileTask(fileToExtractName, dest, user string) (string, error) {
	c.logger.Info("container-interop-dispatch-extract-file-task")
	extractCmd := RunCommand{
		User: user,
		Env:  []string{},
		Path: "tar",
		Args: []string{"-C", dest, "-xf", filepath.Join(c.getSwapRoot(), c.getSwapInFolder(), fileToExtractName)},
	}

	if err := c.scheduleCommand(c.getOneOffTaskFolder(), &extractCmd, StreamIn); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return extractCmd.ID, nil
}

func (c *containerInterop) scheduleCommand(taskFolder string, cmd *RunCommand, prio Priority) error {
	fileId, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return err
	}
	taskId := fileId.String()
	cmd.ID = taskId
	taskFolderFullPath := filepath.Join(c.mountedPath, taskFolder, fmt.Sprintf("%d", prio))
	err = os.MkdirAll(taskFolderFullPath, 0700)
	if err != nil {
		c.logger.Error("container-interop-new-task-mkdir-all-failed", err)
	}

	filePath := filepath.Join(c.mountedPath, taskFolder, fmt.Sprintf("%d", prio), fmt.Sprintf("%s_%d.sh", taskId, time.Now().UnixNano()))
	f, err := os.Create(filePath)
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
	// convert the run commands to the task.

	args := make([]string, len(cmd.Args))
	for i, _ := range cmd.Args {
		if cmd.Args[i] == "" {
			args[i] = "\"\""
		} else {
			args[i] = cmd.Args[i]
		}
	}

	buffer.WriteString(fmt.Sprintf(`su - %s -c 'export HOME=/home/%s/app
			export PORT=8080
			export APP_ROOT=/home/%s/app
			%s %s
			`, cmd.User, cmd.User, cmd.User, cmd.Path, strings.Join(args, " ")))

	buffer.WriteString(c.getTaskOutputScript(cmd))

	_, err = f.WriteString(buffer.String())
	if err != nil {
		c.logger.Error("container-interop-new-task-write-failed", err)
		return verrors.New("failed to create entry script.")
	}
	return nil
}

func (c *containerInterop) getTaskOutputScript(cmd *RunCommand) string {
	taskOutputPath := filepath.Join(c.getSwapOutFolder(), c.getTaskOutputFolder(), cmd.ID+".out")
	return fmt.Sprintf("echo $? > %s", taskOutputPath)
}

func (c *containerInterop) mountContainerRoot(handle string) (string, error) {
	shareName := c.getContainerSwapRootShareFolder(handle)
	vs := vstore.NewVStore()
	// 1. prepare the volumes.
	// create share folder
	err := vs.CreateShareFolder(shareName)
	if err != nil {
		c.logger.Error("container-interop-mount-container-root-create-share-folder-failed", err)
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

	mounter := mount.NewMounter()
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
