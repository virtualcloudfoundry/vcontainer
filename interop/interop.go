package interop

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/marstr/guid"
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

type Task struct {
	Script string
}

type Priority int32

const (
	StreamIn Priority = 0
	Run      Priority = 1
)

type RunCommand struct {
	Path string
	Args []string
	Env  []string
	User string
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
	// dispatch one task to the container, online.
	DispatchRunTask(cmd RunCommand) error
	// copy the src folder to the prepared swap root. and write one task into it.
	DiskpatchFolderTask(src, dest string) error
	// copy the file: src
	PrepareExtractFile(dest string) (string, *os.File, error)
	DispatchExtractFileTask(fileToExtract, dest, user string) error
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

func (c *containerInterop) getSwapOutFolder() string {
	return "./out"
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

func (c *containerInterop) newTask(taskFolder string, task Task, prio Priority) error {
	fileId := guid.NewGUID()
	taskFolderFullPath := filepath.Join(c.mountedPath, taskFolder, fmt.Sprintf("%d", prio))
	os.MkdirAll(taskFolderFullPath, 0700)

	filePath := filepath.Join(c.mountedPath, taskFolder, fmt.Sprintf("%d", prio), fmt.Sprintf("%s_%d.sh", fileId.String(), time.Now().UnixNano()))
	f, err := os.Create(filePath)
	// TODO better error handling.
	f.WriteString(task.Script)
	if err != nil {
		c.logger.Error("container-interop-new-task-write-failed", err)
		return verrors.New("failed to create entry script.")
	}
	if f != nil {
		f.Close()
	}
	return nil
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

func (c *containerInterop) DispatchRunTask(cmd RunCommand) error {
	task, err := c.convertCommandToTask(cmd)

	if err != nil {
		c.logger.Error("container-interop-convert-command-to-task-failed", err)
		return verrors.New("failed to dispatch run task.")
	}

	if err = c.newTask(c.getOneOffTaskFolder(), task, Run); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return verrors.New("failed to create task.")
	}

	return verrors.New("not implemented")
}

func (c *containerInterop) convertCommandToTask(cmd RunCommand) (Task, error) {
	// use the "" instead of space
	args := make([]string, len(cmd.Args))
	for i, _ := range cmd.Args {
		if cmd.Args[i] == "" {
			args[i] = "\"\""
		} else {
			args[i] = cmd.Args[i]
		}
	}
	script := fmt.Sprintf("#!/bin/bash\nsu - %s -c 'export HOME=/home/%s/app\nexport PORT=8080\nexport APP_ROOT=/home/%s/app\n%s %s'", cmd.User, cmd.User, cmd.User, cmd.Path, strings.Join(args, " "))
	return Task{
		Script: script,
	}, nil
}

func (c *containerInterop) DiskpatchFolderTask(src, dst string) error {
	fsync := fsync.NewFSync(c.logger)
	relativePath := fmt.Sprintf("%s/%s", c.getSwapInFolder(), dst) // ./tmp/app
	targetFolder := filepath.Join(c.mountedPath, relativePath)
	err := fsync.CopyFolder(src, targetFolder)
	if err != nil {
		c.logger.Error("container-interop-copy-folder-failed", err, lager.Data{"src": src, "dest": dst})
		return verrors.New("failed to copy folder.")
	}
	srcFolderPath := filepath.Join(common.GetSwapRoot(), relativePath)
	destFolderPath := dst

	postCopyTask := fmt.Sprintf("mkdir -p %s\nrsync -a %s/ %s\n", destFolderPath, srcFolderPath, destFolderPath)
	if err = c.newTask(c.getConstantTaskFolder(), Task{
		Script: postCopyTask,
	}, StreamIn); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return verrors.New("failed to create task.")
	}

	return nil
}

// prepare the task
func (c *containerInterop) PrepareExtractFile(dest string) (string, *os.File, error) {
	id, _ := uuid.NewV4()
	fileToExtractName := fmt.Sprintf("extract_%s", id.String())
	filePath := filepath.Join(c.mountedPath, c.getSwapInFolder(), fileToExtractName)
	file, err := os.Create(filePath)

	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err)
		return "", nil, err
	}
	return fileToExtractName, file, nil
}

func (c *containerInterop) DispatchExtractFileTask(fileToExtractName, dest, user string) error {
	c.logger.Info("container-interop-dispatch-extract-file-task")

	postExtractTask := fmt.Sprintf("#!/bin/bash\n su - %s -c 'tar -C %s -xf %s'\n", user, dest, filepath.Join(c.getSwapRoot(), c.getSwapInFolder(), fileToExtractName))
	c.newTask(c.getOneOffTaskFolder(), Task{
		Script: postExtractTask,
	}, StreamIn)

	return nil
}

func (c *containerInterop) mountContainerRoot(handle string) (string, error) {
	shareName := c.getContainerSwapRootShareFolder(handle)
	vs := vstore.NewVStore()
	// 1. prepare the volumes.
	// create share folder
	err := vs.CreateShareFolder(shareName)
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
		return "", err
	}
	return mountedRootFolder, nil
}

func (c *containerInterop) getContainerSwapRootShareFolder(handle string) string {
	shareName := fmt.Sprintf("root-%s", handle)
	return shareName
}
