package vstream

import (
	"fmt"
	"io/ioutil"

	"code.cloudfoundry.org/lager"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/helpers/mount"
	"github.com/virtualcloudfoundry/vcontainer/vstore"
)

type VStreamV2 struct {
	handle      string
	logger      lager.Logger
	mountedPath string
}

func NewVStreamV2(handle string, logger lager.Logger) *VStreamV2 {
	return &VStreamV2{
		handle: handle,
		logger: logger,
	}
}

func (c *VStreamV2) Open() error {
	mountedPath, err := c.mountContainerRoot()
	if err != nil {
		return err
	}
	c.mountedPath = mountedPath
	return nil
}

func (c *VStreamV2) Close() error {
	mounter := mount.NewMounter()
	err := mounter.Umount(c.mountedPath)
	return err
}

func (c *VStreamV2) getContainerSwapRootShareFolder(handle string) string {
	shareName := fmt.Sprintf("root-%s", handle)
	return shareName
}

func (c *VStreamV2) mountContainerRoot() (string, error) {
	shareName := c.getContainerSwapRootShareFolder(c.handle)
	vs := vstore.NewVStore()
	// 1. prepare the volumes.
	// create share folder
	err := vs.CreateShareFolder(shareName)
	c.logger.Info("#########(andliu) create share folder, will fail second time.")
	mountedRootFolder, err := ioutil.TempDir("/tmp", "folder_to_azure_")
	options := []string{
		"vers=3.0",
		fmt.Sprintf("username=%s", config.GetVContainerEnvInstance().ACIConfig.StorageId),
		fmt.Sprintf("password=%s", config.GetVContainerEnvInstance().ACIConfig.StorageSecret),
		"dir_mode=0777,file_mode=0777,serverino",
	}
	// TODO because 445 port is blocked in microsoft, so we use the proxy to do it...
	options = append(options, "port=444")
	// TODO add test hook for the proxy.
	azureFilePath := fmt.Sprintf("//40.65.190.119/%s", shareName) //fmt.Sprintf("//%s.file.core.windows.net/%s", storageID, shareName)
	mounter := mount.NewMounter()
	err = mounter.Mount(azureFilePath, mountedRootFolder, "cifs", options)
	if err != nil {
		c.logger.Info("#######(andliu) PrepareSwapVolumeMount mount failed.", lager.Data{
			"src":  azureFilePath,
			"dest": mountedRootFolder,
			"err":  err.Error()})
		return "", err
	}
	return mountedRootFolder, nil
}
