package helpers // import "github.com/virtualcloudfoundry/vcontainer/helpers"

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"path/filepath"

	"code.cloudfoundry.org/archiver/extractor"
	"github.com/virtualcloudfoundry/goaci/aci"

	"code.cloudfoundry.org/lager"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/helpers/fsync"
	"github.com/virtualcloudfoundry/vcontainer/helpers/mount"
)

// copy one folder to one azure share.

type VSync struct {
	logger lager.Logger
}

func NewVSync(logger lager.Logger) *VSync {
	return &VSync{
		logger: logger,
	}
}

func (v *VSync) ExtractToAzureShare(reader io.ReadCloser, vol *aci.Volume, vm *aci.VolumeMount, parentExists bool, destinationInContainer string) error {
	v.logger.Info("#########(andliu) extracting to azure share.", lager.Data{
		"destinationInContainer": destinationInContainer,
	})
	// extract to a folder first, then copy to the target first.
	extractedFolder, err := ioutil.TempDir("/tmp", "folder_extracted")
	extra := extractor.NewTar()
	err = extra.ExtractStream(extractedFolder, reader)
	if err == nil {
		mountFolder, err := ioutil.TempDir("/tmp", "folder_to_azure_")

		mounter := mount.NewMounter()
		options := []string{
			"vers=3.0",
			fmt.Sprintf("username=%s", config.GetVContainerEnvInstance().ACIConfig.StorageId),
			fmt.Sprintf("password=%s", config.GetVContainerEnvInstance().ACIConfig.StorageSecret),
			"dir_mode=0777,file_mode=0777,serverino", //  for noserverino https://linux.die.net/man/8/mount.cifs
		}
		// TODO because 445 port is blocked in microsoft, so we use the proxy to do it...
		options = append(options, "port=444")
		//40.112.190.242
		azureFilePath := fmt.Sprintf("//40.65.190.119/%s", vol.AzureFile.ShareName) //fmt.Sprintf("//%s.file.core.windows.net/%s", storageID, shareName)
		err = mounter.Mount(azureFilePath, mountFolder, "cifs", options)
		// err = mounter.Mount(azureFilePath, tempFolder, "cifs", options)
		if err == nil {
			var targetFolder string
			if parentExists {
				// make sub folder.
				// bindMount.DstPath  /tmp/lifecycle
				// vm.MountPath	 /tmp
				rel, err := filepath.Rel(vm.MountPath, destinationInContainer)
				if err != nil {
					v.logger.Info("##########(andliu) filepath.Rel failed.", lager.Data{"err": err.Error()})
				}
				targetFolder = filepath.Join(mountFolder, rel)
				err = os.MkdirAll(targetFolder, os.ModeDir)
				if err != nil {
					v.logger.Info("##########(andliu) MkdirAll failed.", lager.Data{"err": err.Error()})
				}
			} else {
				targetFolder = mountFolder
			}
			fsync := fsync.NewFSync(v.logger)
			err = fsync.CopyFolder(extractedFolder, targetFolder)
			if err != nil {
				v.logger.Info("##########(andliu) copy folder failed.", lager.Data{
					"parentExists":    parentExists,
					"err":             err.Error(),
					"extractedFolder": extractedFolder,
					"targetFolder":    targetFolder})
				return err
			}
			err = mounter.Umount(mountFolder)
			if err != nil {
				v.logger.Info("########(andliu) umount failed.", lager.Data{"err": err.Error()})
			}
			return nil
		} else {
			v.logger.Info("##########(andliu) mount temp folder failed.", lager.Data{
				"azureFilePath": azureFilePath,
				// "tempFolder":    tempFolder,
			})
			return err
		}
	} else {
		v.logger.Info("#########(andliu) ExtractToAzureShare extract to temp folder failed.", lager.Data{"err": err.Error()})
		return err
	}
}

func (v *VSync) CopyFolderToAzureShare(src, storageID, storageSecret, shareName string) error {
	v.logger.Info("##########(andliu) copy folder to azure share.", lager.Data{"src": src, "shareName": shareName})
	mounter := mount.NewMounter()
	tempFolder, err := v.MountToTempFolder(storageID, storageSecret, shareName)
	if err == nil {
		fsync := fsync.NewFSync(v.logger)
		err = fsync.CopyFolder(src, tempFolder)
		if err == nil {
			err = mounter.Umount(tempFolder)
			if err != nil {
				v.logger.Info("########(andliu) umount failed.", lager.Data{"err": err.Error()})
			}
			return nil
		} else {
			return err
		}
	} else {
		return err
	}
}

func (v *VSync) MountToTempFolder(storageID, storageSecret, shareName string) (string, error) {
	options := []string{
		"vers=3.0",
		fmt.Sprintf("username=%s", storageID),
		fmt.Sprintf("password=%s", storageSecret),
		"dir_mode=0777,file_mode=0777,serverino",
	}

	mounter := mount.NewMounter()
	tempFolder, err := ioutil.TempDir("/tmp", "folder_to_azure")
	if err == nil {
		// TODO because 445 port is blocked in microsoft, so we use the proxy to do it...
		options = append(options, "port=444")
		azureFilePath := fmt.Sprintf("//40.65.190.119/%s", shareName) //fmt.Sprintf("//%s.file.core.windows.net/%s", storageID, shareName)
		err = mounter.Mount(azureFilePath, tempFolder, "cifs", options)
		return tempFolder, err
	}
	return "Failed to create temp folder.", err
}
