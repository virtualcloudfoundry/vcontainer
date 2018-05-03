package interop

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"code.cloudfoundry.org/lager"
	uuid "github.com/satori/go.uuid"
	"github.com/virtualcloudfoundry/goaci/aci"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/helpers/fsync"
	"github.com/virtualcloudfoundry/vcontainer/helpers/mount"
	"github.com/virtualcloudfoundry/vcontainer/vstore"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

// components to do interop with the container.
// 1. one interop root folder to interop with the container.
// 2. one components managing the tasks dispatched to the the containers
// 	  a. one tasks folder ends with .task
// 3. one daemon to interate with the tasks.
type Priority int32

const (
	StreamIn  Priority = 0
	StreamOut Priority = 1
	Run       Priority = 1
)

type RunCommand struct {
	ID   string
	User string
	Env  []string
	Path string
	Args []string
	Tag  string // for debugging
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
	// copy the src folder to the prepared swap root. and write one task into it.
	DispatchStreamOutTask(outSpec *vcontainermodels.StreamOutSpec) (string, string, error)
	OpenStreamOutFile(fileId string) (*os.File, error)
	DispatchFolderTask(src, dest string) (string, error)
	// prepare an file opened for writing, so the extract task can extract it to the dest folder in the container.
	PrepareExtractFile(dest string) (string, *os.File, error)
	// dispatch an extract file task.
	DispatchExtractFileTask(fileToExtract, dest, user string) (string, error)
	// wait for the task with the taskId exit.
	WaitForTaskExit(taskId string) error
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

func (c *containerInterop) Prepare() (*ContainerInteropInfo, error) {
	c.logger.Info("container-interop-prepare")
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
		MountPath: c.getSwapRoot(),
		ReadOnly:  false,
	}
	volumeMounts = append(volumeMounts, volumeMount)
	interopInfo.VolumeMounts = volumeMounts
	interopInfo.Volumes = volumes

	// prepare the task in/out folders
	taskFolderFullPath := filepath.Join(c.mountedPath, c.getConstantTaskFolder(), fmt.Sprintf("%d", StreamIn))
	err := os.MkdirAll(taskFolderFullPath, 0700)
	if err != nil {
		return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskFolderFullPath))
	}

	taskFolderFullPath = filepath.Join(c.mountedPath, c.getConstantTaskFolder(), fmt.Sprintf("%d", Run))
	err = os.MkdirAll(taskFolderFullPath, 0700)
	if err != nil {
		return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskFolderFullPath))
	}

	taskFolderFullPath = filepath.Join(c.mountedPath, c.getOneOffTaskFolder(), fmt.Sprintf("%d", StreamIn))
	err = os.MkdirAll(taskFolderFullPath, 0700)
	if err != nil {
		return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskFolderFullPath))
	}

	taskFolderFullPath = filepath.Join(c.mountedPath, c.getOneOffTaskFolder(), fmt.Sprintf("%d", Run))
	err = os.MkdirAll(taskFolderFullPath, 0700)
	if err != nil {
		return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskFolderFullPath))
	}

	taskOutputPath := filepath.Join(c.mountedPath, c.getSwapOutFolder(), c.getTaskOutputFolder())
	err = os.MkdirAll(taskOutputPath, 0700)
	if err != nil {
		return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskOutputPath))
	}

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

func (c *containerInterop) getStreamOutFolder() string {
	return "streamout"
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
		FILES=%s/%s/%d/*.sh
		for f in $FILES
		do
			echo "Executing constant task file: $f ...\n"
			cat $f
			bash -c $f
		done

		FILES=%s/%s/%d/*.sh
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

	return nil //verrors.New("not implemented.")
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

func (c *containerInterop) DispatchRunCommand(cmd RunCommand) (string, error) {
	c.logger.Info("container-interop-dispatch-run-command", lager.Data{"handle": c.handle})
	if err := c.scheduleCommand(c.getOneOffTaskFolder(), &cmd, Run, cmd.Tag); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return cmd.ID, nil
}

func (c *containerInterop) DispatchStreamOutTask(outSpec *vcontainermodels.StreamOutSpec) (string, string, error) {
	c.logger.Info("container-interop-dispatch-stream-out-task", lager.Data{"handle": c.handle, "spec": outSpec})
	id, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return "", "", err
	}

	destFolderPath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getStreamOutFolder())
	mkdirCommand := RunCommand{
		User: outSpec.User,
		Env:  []string{},
		Path: "mkdir",
		Args: []string{"-p", destFolderPath},
	}

	err = c.scheduleAndWait(c.getOneOffTaskFolder(), &mkdirCommand, StreamOut, "mkdir")
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err, lager.Data{"cmd": mkdirCommand})
		return "", "", verrors.New("failed to schedule or wait for task exit.")
	}

	destFilePath := fmt.Sprintf("%s/%s", destFolderPath, id.String())
	syncCommand := RunCommand{
		User: outSpec.User,
		Env:  []string{},
		Path: "rsync",
		Args: []string{"-a", outSpec.Path, destFilePath},
	}
	c.logger.Info("container-interop-sync-command", lager.Data{"cmd": syncCommand})
	if err = c.scheduleCommand(c.getOneOffTaskFolder(), &syncCommand, Run, "rsync"); err != nil {
		c.logger.Error("container-interop-new-task-failed", err, lager.Data{"cmd": syncCommand})
		return "", "", verrors.New("failed to create task.")
	}
	//err = c.WaitForTaskExit(syncCommand.ID)
	return syncCommand.ID, id.String(), nil
}

func (c *containerInterop) OpenStreamOutFile(fileId string) (*os.File, error) {
	c.logger.Info("container-interop-open-stream-out-file", lager.Data{"handle": c.handle})
	filePath := fmt.Sprintf("%s/%s/%s", c.mountedPath, c.getStreamOutFolder(), fileId)

	file, err := os.Open(filePath)
	if err != nil {
		c.logger.Error("container-interop-open-stream-out-file-failed", err, lager.Data{"file_path": filePath})
		return nil, verrors.New("open-file-failed")
	}
	return file, nil
}

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

	mkdirCommand := RunCommand{
		User: "root",
		Env:  []string{},
		Path: "mkdir",
		Args: []string{"-p", destFolderPath},
	}

	err = c.scheduleAndWait(c.getConstantTaskFolder(), &mkdirCommand, StreamIn, "mkdir")
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err)
		return "", verrors.New("failed to schedule or wait for task exit.")
	}

	// chmod
	chmodCommand := RunCommand{
		User: "root",
		Env:  []string{},
		Path: "chmod",
		Args: []string{"777", destFolderPath},
	}

	err = c.scheduleAndWait(c.getOneOffTaskFolder(), &chmodCommand, StreamIn, "chmod")
	if err != nil {
		c.logger.Error("container-interop-dispatch-folder-task-prepare-failed", err)
		return "", verrors.New("failed to change permission.")
	}

	syncCommand := RunCommand{
		User: "root",
		Env:  []string{},
		Path: "rsync",
		Args: []string{"-a", fmt.Sprintf("%s/", srcFolderPath), destFolderPath},
	}

	if err = c.scheduleCommand(c.getConstantTaskFolder(), &syncCommand, StreamIn, "rsync"); err != nil {
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
	mkdirCommand := RunCommand{
		User: "root",
		Env:  []string{},
		Path: "mkdir",
		Args: []string{"-p", dest},
	}

	err := c.scheduleAndWait(c.getOneOffTaskFolder(), &mkdirCommand, StreamIn, "mkdir")
	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err)
		return "", verrors.New("failed to schedule or wait for task exit.")
	}

	// chmod
	chmodCommand := RunCommand{
		User: "root",
		Env:  []string{},
		Path: "chmod",
		Args: []string{"777", dest},
	}

	err = c.scheduleAndWait(c.getOneOffTaskFolder(), &chmodCommand, StreamIn, "chmod")
	if err != nil {
		c.logger.Error("container-interop-dispatch-extract-file-task-failed", err)
		return "", verrors.New("failed to change permission.")
	}

	extractCmd := RunCommand{
		User: user,
		Env:  []string{},
		Path: "tar",
		Args: []string{"-C", dest, "-xf", filepath.Join(c.getSwapRoot(), c.getSwapInFolder(), fileToExtractName)},
	}

	if err := c.scheduleCommand(c.getOneOffTaskFolder(), &extractCmd, StreamIn, "tar"); err != nil {
		c.logger.Error("container-interop-new-task-failed", err)
		return "", verrors.New("failed to create task.")
	}

	return extractCmd.ID, nil
}

func (c *containerInterop) scheduleCommand(taskFolder string, cmd *RunCommand, prio Priority, tag string) error {
	c.logger.Info("container-interop-schedule-command", lager.Data{"handle": c.handle, "cmd": cmd, "prio": prio, "task_folder": taskFolder})
	fileId, err := uuid.NewV4()
	if err != nil {
		c.logger.Fatal("Couldn't generate uuid", err)
		return err
	}
	taskId := fileId.String()
	cmd.ID = fmt.Sprintf("%s_%s", tag, taskId)
	taskFolderFullPath := filepath.Join(c.mountedPath, taskFolder, fmt.Sprintf("%d", prio))

	filePath := filepath.Join(taskFolderFullPath, fmt.Sprintf("%s_%d.sh", taskId, time.Now().UnixNano()))
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
	pidFilePath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.pid", taskId))

	// pre-processing the envs and the args.
	processedEnvs := c.getProcessedEnvs(cmd.Env)
	processedArgs := c.getProcessedArgs(cmd.Args)
	errorFilePath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getTaskOutputFolder(), fmt.Sprintf("%s.err", taskId))
	buffer.WriteString(fmt.Sprintf(`su - %s -c 'export HOME=/home/%s/app
%s
export APP_ROOT=/home/%s/app
%s %s 2>%s & export CMD_PID=$!
echo $CMD_PID > %s
wait $CMD_PID
`, cmd.User, cmd.User, strings.Join(processedEnvs, "\n"), cmd.User, cmd.Path,
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
	return nil
}

func (c *containerInterop) scheduleAndWait(taskFolder string, cmd *RunCommand, prio Priority, tag string) error {
	err := c.scheduleCommand(taskFolder, cmd, prio, tag)
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

func (c *containerInterop) getProcessedEnvs(envs []string) []string {
	processedEnvs := make([]string, len(envs))
	for i, _ := range envs {
		processedEnvs[i] = fmt.Sprintf("export %s", envs[i])
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

func (c *containerInterop) getTaskExitFilePath(taskId string) string {
	taskOutputPath := filepath.Join(c.mountedPath, c.getSwapOutFolder(), c.getTaskOutputFolder(), taskId+".exit")
	return taskOutputPath
}

func (c *containerInterop) getTaskOutputScript(cmd *RunCommand) string {
	taskOutputPath := filepath.Join(c.getSwapRoot(), c.getSwapOutFolder(), c.getTaskOutputFolder(), cmd.ID+".exit")
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

// judge whether the task exited.
// this would be running in the vcontainer process.
func (c *containerInterop) TaskExited(taskId string) (vcontainermodels.WaitResponse, error) {
	exitFilePath := c.getTaskExitFilePath(taskId)
	c.logger.Info("container-interop-task-exited-check-file", lager.Data{"file": exitFilePath})
	if _, err := os.Stat(exitFilePath); os.IsNotExist(err) {
		return vcontainermodels.WaitResponse{
			Exited:   false,
			ExitCode: -1,
		}, nil
	} else {
		// TODO racing issue, should lock the file.
		content, err := ioutil.ReadFile(exitFilePath)
		if err != nil {
			c.logger.Error("container-interop-task-exited-read-file-failed", err, lager.Data{
				"exit_file_path": exitFilePath,
				"task_id":        taskId,
			})
			return vcontainermodels.WaitResponse{
				Exited:   false,
				ExitCode: -1,
			}, err
		}
		contentStr := string(content)
		contentStr = strings.Trim(contentStr, "\n ")
		exitCode, err := strconv.ParseInt(contentStr, 10, 32)
		if err != nil {
			c.logger.Error("container-interop-task-exited-parse-int-failed", err, lager.Data{
				"exit_file_path": exitFilePath,
				"task_id":        taskId,
				"content":        contentStr,
			})
			return vcontainermodels.WaitResponse{
				Exited:   false,
				ExitCode: -1,
			}, err
		}
		return vcontainermodels.WaitResponse{
			Exited:   true,
			ExitCode: int32(exitCode),
		}, nil
	}
}

// clear the task related files.
func (c *containerInterop) CleanTask(taskId string) error {
	return nil
}
