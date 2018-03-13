package interop

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/virtualcloudfoundry/goaci/aci"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
)

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

	// prepare the task in folders.
	taskFolders := []string{OneOffTask}
	taskSteps := []TaskStep{StreamIn, Run, StreamOut}

	for _, taskFolder := range taskFolders {
		for _, taskStep := range taskSteps {
			taskFolderFullPath := filepath.Join(c.mountedPath, taskFolder, string(taskStep))
			err := os.MkdirAll(taskFolderFullPath, 0700)
			if err != nil {
				return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskFolderFullPath))
			}
		}
	}

	// prepare the task out folder.
	taskOutputPath := filepath.Join(c.mountedPath, c.getSwapOutFolder(), c.getTaskOutputFolder())
	err := os.MkdirAll(taskOutputPath, 0700)
	if err != nil {
		return nil, verrors.New(fmt.Sprintf("failed to prepare folder %s.", taskOutputPath))
	}

	// prepare the root daemon script.
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
