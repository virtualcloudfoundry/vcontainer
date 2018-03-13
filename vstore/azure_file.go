package vstore

import (
	"fmt"

	azs "github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/go-autorest/autorest/azure"
	// "github.com/golang/glog"
)

const (
	useHTTPS = true
)

// FileClient is the interface for creating file shares, interface for test
// injection.
// type FileClient interface {
// 	createFileShare(accountName, accountKey, name string, sizeGiB int) error
// 	deleteFileShare(accountName, accountKey, name string) error
// 	resizeFileShare(accountName, accountKey, name string, sizeGiB int) error
// }

type AzureFileClient struct {
	env azure.Environment
}

func (f *AzureFileClient) createFileShare(accountName, accountKey, name string, sizeGiB int) error {
	fileClient, err := f.getFileSvcClient(accountName, accountKey)
	if err != nil {
		return err
	}
	// create a file share and set quota
	// Note. Per https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/Create-Share,
	// setting x-ms-share-quota can set quota on the new share, but in reality, setting quota in CreateShare
	// receives error "The metadata specified is invalid. It has characters that are not permitted."
	// As a result,breaking into two API calls: create share and set quota
	share := fileClient.GetShareReference(name)
	if err = share.Create(nil); err != nil {
		return fmt.Errorf("failed to create file share, err: %v", err)
	}
	share.Properties.Quota = sizeGiB
	if err = share.SetProperties(nil); err != nil {
		if err := share.Delete(nil); err != nil {
			// glog.Errorf("Error deleting share: %v", err)
		}
		return fmt.Errorf("failed to set quota on file share %s, err: %v", name, err)
	}
	return nil
}

// delete a file share
func (f *AzureFileClient) deleteFileShare(accountName, accountKey, name string) error {
	fileClient, err := f.getFileSvcClient(accountName, accountKey)
	if err != nil {
		return err
	}
	return fileClient.GetShareReference(name).Delete(nil)
}

func (f *AzureFileClient) resizeFileShare(accountName, accountKey, name string, sizeGiB int) error {
	fileClient, err := f.getFileSvcClient(accountName, accountKey)
	if err != nil {
		return err
	}
	share := fileClient.GetShareReference(name)
	if share.Properties.Quota >= sizeGiB {
		// glog.Warningf("file share size(%dGi) is already greater or equal than requested size(%dGi), accountName: %s, shareName: %s",
		// 	share.Properties.Quota, sizeGiB, accountName, name)
		return nil
	}
	share.Properties.Quota = sizeGiB
	if err = share.SetProperties(nil); err != nil {
		return fmt.Errorf("failed to set quota on file share %s, err: %v", name, err)
	}
	// glog.V(4).Infof("resize file share completed, accountName: %s, shareName: %s, sizeGiB: %d", accountName, name, sizeGiB)
	return nil
}

func (f *AzureFileClient) getFileSvcClient(accountName, accountKey string) (*azs.FileServiceClient, error) {
	fileClient, err := azs.NewClient(accountName, accountKey, f.env.StorageEndpointSuffix, azs.DefaultAPIVersion, useHTTPS)
	if err != nil {
		return nil, fmt.Errorf("error creating azure client: %v", err)
	}
	fc := fileClient.GetFileService()
	return &fc, nil
}
