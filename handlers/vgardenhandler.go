package handlers

import (
	"errors"
	"fmt"

	"code.cloudfoundry.org/lager"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/virtualcloudfoundry/goaci/aci"
	"github.com/virtualcloudfoundry/vcontainer"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/interop"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
	context "golang.org/x/net/context"
)

type vgardenHandler struct {
	logger lager.Logger
}

func NewVGardenHandler(logger lager.Logger) *vgardenHandler {
	return &vgardenHandler{
		logger: logger,
	}
}

func (v *vgardenHandler) Ping(context.Context, *google_protobuf.Empty) (*google_protobuf.Empty, error) {
	v.logger.Info("vgarden-ping")
	// NOOP for vgarden.
	return nil, nil // verrors.New("not implemented")
}

func (v *vgardenHandler) Capacity(context.Context, *google_protobuf.Empty) (*vcontainermodels.Capacity, error) {
	v.logger.Info("vgarden-capacity")
	capacity := vcontainermodels.Capacity{
		MemoryInBytes: 3 * 1024 * 1024 * 1024,
		DiskInBytes:   500 * 1024 * 1024 * 1024,
		MaxContainers: 5,
	}
	return &capacity, nil
}

func (v *vgardenHandler) Create(ctx context.Context, spec *vcontainermodels.ContainerSpec) (*google_protobuf.Empty, error) {
	v.logger.Info("vgarden-create-spec", lager.Data{"spec": spec})
	containerInterop := interop.NewContainerInterop(spec.Handle, v.logger)
	var err error

	if err = containerInterop.Open(); err != nil {
		v.logger.Error("vgarden-create-open-interop-failed", err)
		return nil, errors.New("open container interop failed.")
	}

	defer func() {
		if err = containerInterop.Close(); err != nil {
			v.logger.Error("vgarden-create-close-interop-failed", err)
		}
	}()

	var interopInfo *interop.ContainerInteropInfo
	if interopInfo, err = containerInterop.Prepare(); err != nil {
		v.logger.Error("vgarden-create-prepare-interop-failed", err)
		return nil, errors.New("prepare container interop failed.")
	}

	if err = v.create(interopInfo.Cmd, interopInfo, spec); err != nil {
		v.logger.Error("vgarden-create-create-aci-failed", err)
		return nil, verrors.New("create container failed.")
	}

	for _, bindMount := range spec.BindMounts {
		// put the folder
		if _, err = containerInterop.DispatchFolderTask(bindMount.SrcPath, bindMount.DstPath); err != nil {
			v.logger.Error("vgarden-create-dispatch-folder-task-failed", err)
			return nil, errors.New("open container interop failed.")
		}
	}
	return nil, nil
}

func (v *vgardenHandler) create(cmd []string, interopInfo *interop.ContainerInteropInfo, spec *vcontainermodels.ContainerSpec) error {
	var containerGroup aci.ContainerGroup
	containerGroup.Location = config.GetVContainerEnvInstance().ACIConfig.Location
	containerGroup.ContainerGroupProperties.OsType = aci.Linux
	containerGroup.Tags = spec.Properties.Properties
	// TODO add the ports.
	var containerProperties aci.ContainerProperties
	containerProperties.Image = "cloudfoundry/cflinuxfs2"

	containerGroup.ContainerGroupProperties.RestartPolicy = aci.Never
	containerProperties.Command = cmd

	if len(spec.NetIn) > 0 {
		containerGroup.IPAddress = &aci.IPAddress{
			Type:  aci.Public,
			Ports: make([]aci.Port, 0),
		}
	}

	for _, p := range spec.NetIn {
		containerGroup.IPAddress.Ports =
			append(containerGroup.IPAddress.Ports, aci.Port{
				Protocol: aci.TCP,
				Port:     int32(p.ContainerPort), // TODO use the ContainerPort for all now...
			})
		containerPort := aci.ContainerPort{
			Port:     int32(p.ContainerPort),
			Protocol: aci.ContainerNetworkProtocolTCP,
		}
		containerProperties.Ports = append(containerProperties.Ports, containerPort)
	}

	container := aci.Container{
		Name: spec.Handle,
	}

	containerGroup.ContainerGroupProperties.Volumes = interopInfo.Volumes
	containerProperties.VolumeMounts = interopInfo.VolumeMounts

	containerProperties.Resources.Requests.CPU = 1 //float64(spec.Limits.CPU.LimitInShares)

	containerProperties.Resources.Requests.MemoryInGB = 0.3 //float64(spec.Limits.Memory.LimitInBytes) / (1024 * 1024 * 1024)

	containerProperties.Resources.Limits.CPU = 1          //float64(spec.Limits.CPU.LimitInShares)
	containerProperties.Resources.Limits.MemoryInGB = 0.3 //float64(spec.Limits.Memory.LimitInBytes) / (1024 * 1024 * 1024)
	container.ContainerProperties = containerProperties
	containerGroup.Containers = append(containerGroup.Containers, container)

	client, err := vcontainer.NewACIClient()

	if err != nil {
		v.logger.Error("vgarden-create-new-aci-client-failed", err)
		return verrors.New("failed to create aci client.")
	}

	if _, err = client.CreateContainerGroup(config.GetVContainerEnvInstance().ACIConfig.ResourceGroup, spec.Handle, containerGroup); err != nil {
		v.logger.Error("vgarden-create-create-container-failed", err)
		return verrors.New("not implemented")
	}
	return nil
}

func (v *vgardenHandler) Destroy(ctx context.Context, handle *google_protobuf.StringValue) (*google_protobuf.Empty, error) {
	v.logger.Info("vgarden-destroy", lager.Data{"handle": handle})
	client, err := vcontainer.NewACIClient()

	if err != nil {
		v.logger.Error("vgarden-destroy-new-aci-client-failed", err)
		return nil, err
	}
	err = client.DeleteContainerGroup(config.GetVContainerEnvInstance().ACIConfig.ResourceGroup, handle.Value)
	if err != nil {
		v.logger.Error("vgarden-destroy-delete-container-group-failed", err)
		return nil, verrors.New("destroy contaier failed.")
	} else {
		return nil, nil
	}
}

func (v *vgardenHandler) Containers(ctx context.Context, properties *vcontainermodels.Properties) (*vcontainermodels.ContainersResponse, error) {
	v.logger.Info("vgarden-containers", lager.Data{"properties": properties})
	client, err := vcontainer.NewACIClient()

	if err != nil {
		v.logger.Error("vgarden-containers-new-aci-client-failed", err)
		return nil, verrors.New("new aci client failed.")
	}

	containerGroups, err := client.ListContainerGroups(config.GetVContainerEnvInstance().ACIConfig.ResourceGroup)
	if err != nil {
		return nil, verrors.New("list contaier groups failed.")
	}

	containersResponse := vcontainermodels.ContainersResponse{}

	for _, c := range containerGroups.Value {
		// find the matched properties.
		matchesAll := true
		for key, val := range properties.Properties {
			if c.Tags[key] != val {
				matchesAll = false
			}
		}
		if matchesAll {
			containersResponse.Handle = append(containersResponse.Handle, c.Name)
		} else {
			//skip
		}
	}

	v.logger.Info("vgarden-containers-got-containers", lager.Data{
		"properties": properties,
		"containers": containersResponse.Handle})

	return &containersResponse, nil
}

func (v *vgardenHandler) BulkInfo(ctx context.Context, req *vcontainermodels.BulkInfoRequest) (*vcontainermodels.BulkInfoResponse, error) {
	v.logger.Info("vgarden-bulkinfo")
	return nil, verrors.New("not implemented")
}

func (v *vgardenHandler) BulkMetrics(ctx context.Context, req *vcontainermodels.BulkMetricsRequest) (*vcontainermodels.BulkMetricsResponse, error) {
	v.logger.Info("vgarden-bulkmetrics", lager.Data{"req": req})
	return nil, verrors.New("not implemented")
}

func (v *vgardenHandler) Lookup(ctx context.Context, handle *google_protobuf.StringValue) (*google_protobuf.Empty, error) {
	v.logger.Info("vgarden-lookup")
	// find the container in the resource group, if found, return the empty, or return the error.
	client, err := vcontainer.NewACIClient()

	if err != nil {
		v.logger.Error("vgarden-containers-failed", err)
		return nil, err
	}

	containerGroups, err := client.ListContainerGroups(config.GetVContainerEnvInstance().ACIConfig.ResourceGroup)
	if err != nil {
		return nil, verrors.New("list container groups failed.")
	}
	found := false
	for _, c := range containerGroups.Value {
		if c.Name == handle.Value {
			found = true
			break
		}
	}
	if found {
		return nil, nil
	} else {
		return nil, verrors.New(fmt.Sprintf("container %s not found", handle.Value))
	}
}
