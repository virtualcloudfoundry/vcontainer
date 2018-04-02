package handlers

import (
	"io"
	"os"

	"github.com/virtualcloudfoundry/goaci/aci"

	"code.cloudfoundry.org/lager"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/virtualcloudfoundry/vcontainer"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/interop"
	"github.com/virtualcloudfoundry/vcontainercommon"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
	context "golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type vcontainerHandler struct {
	logger lager.Logger
}

func NewVContainerHandler(logger lager.Logger) *vcontainerHandler {
	return &vcontainerHandler{
		logger: logger,
	}
}

func (v *vcontainerHandler) Run(ctx context.Context, spec *vcontainermodels.ProcessSpec) (*google_protobuf.Empty, error) {
	v.logger.Info("vcontainer-run")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-run-container-id", lager.Data{"containerid": containerId})
	containerInterop := interop.NewContainerInterop(containerId, v.logger)
	if err = containerInterop.Open(); err != nil {
		v.logger.Error("vcontainer-run-container-interop-open-failed", err)
		return nil, verrors.New("failed to run.")
	}

	defer containerInterop.Close()
	v.logger.Info("vcontainer-run-spec", lager.Data{"spec": spec})

	_, err = containerInterop.DispatchRunTask(interop.RunCommand{
		Path: spec.Path,
		Args: spec.Args,
		Env:  spec.Env,
		User: spec.User,
	})

	if err != nil {
		v.logger.Error("vcontainer-run-dispatch-run-task-failed", err)
		return nil, verrors.New("failed to dispatch run task.")
	}

	return nil, nil
}

func (v *vcontainerHandler) Stop(ctx context.Context, stop *vcontainermodels.StopMessage) (*google_protobuf.Empty, error) {
	v.logger.Info("vcontainer-stop")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-stop-container-id", lager.Data{"containerid": containerId})
	return nil, verrors.New("not implemented")
}

func (v *vcontainerHandler) Metrics(ctx context.Context, empty *google_protobuf.Empty) (*vcontainermodels.Metrics, error) {
	v.logger.Info("vcontainer-metrics")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-metrics-container-id", lager.Data{"containerid": containerId})
	return nil, verrors.New("not implemented")
}

// Sets the grace time.
func (v *vcontainerHandler) SetGraceTime(ctx context.Context, duration *google_protobuf.Duration) (*google_protobuf.Empty, error) {
	v.logger.Info("vcontainer-set-grace-time")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-set-grace-time-container-id", lager.Data{"containerid": containerId})
	return nil, verrors.New("not implemented")
}

func (v *vcontainerHandler) NetIn(ctx context.Context, req *vcontainermodels.NetInRequest) (*vcontainermodels.NetInResponse, error) {
	v.logger.Info("vcontainer-net-in")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-net-in-container-id", lager.Data{"containerid": containerId})
	return nil, verrors.New("not implemented")
}

func (v *vcontainerHandler) NetOut(ctx context.Context, req *vcontainermodels.NetOutRuleRequest) (*google_protobuf.Empty, error) {
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-net-out-container-id", lager.Data{"containerid": containerId})
	return nil, verrors.New("not implemented")
}

func (v *vcontainerHandler) BulkNetOut(ctx context.Context, req *vcontainermodels.BulkNetOutRuleRequest) (*google_protobuf.Empty, error) {
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-net-in-container-id", lager.Data{"containerid": containerId})
	return nil, verrors.New("not implemented")
}

// Properties returns the current set of properties
func (v *vcontainerHandler) Properties(ctx context.Context, empty *google_protobuf.Empty) (*vcontainermodels.Properties, error) {
	v.logger.Info("vcontainer-properties")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-properties-container-id", lager.Data{"containerid": containerId})

	client, err := vcontainer.NewACIClient()
	containerGroup, err := client.GetContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId)
	if err != nil {
		v.logger.Error("vcontainer-properties-get-container-group-failed", err)
		return nil, verrors.New("failed to get container group.")
	}

	properties := &vcontainermodels.Properties{
		Properties: containerGroup.Tags,
	}

	return properties, nil
}

// Property returns the value of the property with the specified name.
//
// Errors:
// * When the property does not exist on the container.
func (v *vcontainerHandler) Property(ctx context.Context, empty *google_protobuf.StringValue) (*google_protobuf.StringValue, error) {
	v.logger.Info("vcontainer-property")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-property-container-id", lager.Data{"containerid": containerId})

	client, err := vcontainer.NewACIClient()
	containerGroup, err := client.GetContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId)
	if err != nil {
		v.logger.Error("vcontainer-property-get-container-group-failed", err)
		return nil, verrors.New("failed to get container group.")
	}

	value := &google_protobuf.StringValue{
		Value: containerGroup.Tags[empty.Value],
	}
	return value, nil
}

// Set a named property on a container to a specified value.
//
// Errors:
// * None.
func (v *vcontainerHandler) SetProperty(ctx context.Context, kv *vcontainermodels.KeyValueMessage) (*google_protobuf.Empty, error) {
	v.logger.Info("vcontainer-set-property")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-set-property-container-id", lager.Data{"containerid": containerId})

	client, err := vcontainer.NewACIClient()
	containerGroup, err := client.GetContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId)
	if err != nil {
		v.logger.Error("vcontainer-property-get-container-group-failed", err)
		return nil, verrors.New("failed to get container group.")
	}

	containerGroup.Tags[kv.Key] = kv.Value
	containerGroupToUpdate := aci.ContainerGroup{}
	containerGroupToUpdate.Tags = containerGroup.Tags
	_, err = client.UpdateContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId, containerGroupToUpdate)
	if err != nil {
		return nil, verrors.New("failed to update container group.")
	}
	return nil, nil
}

// Remove a property with the specified name from a container.
//
// Errors:
// * None.
func (v *vcontainerHandler) RemoveProperty(ctx context.Context, name *google_protobuf.StringValue) (*google_protobuf.Empty, error) {
	v.logger.Info("vcontainer-remove-property")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-remove-property-container-id", lager.Data{"containerid": containerId})

	client, err := vcontainer.NewACIClient()
	containerGroup, err := client.GetContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId)
	if err != nil {
		v.logger.Error("vcontainer-property-get-container-group-failed", err)
		return nil, verrors.New("failed to get container group.")
	}

	delete(containerGroup.Tags, name.Value)
	containerGroupToUpdate := aci.ContainerGroup{}
	containerGroupToUpdate.Tags = containerGroup.Tags
	_, err = client.UpdateContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId, containerGroupToUpdate)
	if err != nil {
		return nil, verrors.New("failed to remove property.")
	}
	return nil, nil
}

// TODO give out a checksum mechanism for the full content.
func (v *vcontainerHandler) StreamIn(server vcontainermodels.VContainer_StreamInServer) error {
	v.logger.Info("vcontainer-stream-in")

	containerId, err := v.getContainerId(server.Context())
	if err != nil {
		return err
	}
	v.logger.Info("vcontainer-stream-in-container-id", lager.Data{"containerid": containerId})
	// read
	var path, user string
	started := false
	var fileToExtract *os.File
	var filePath string
	var containerInterop interop.ContainerInterop
	for {
		streamInSpec, err := server.Recv()
		if err != nil {
			if err != io.EOF {
				return verrors.New("recv failed.")
			} else {
				v.logger.Error("vcontainer-stream-in-recv-failed", err)
				break
			}
		}

		if p, ok := streamInSpec.Part.(*vcontainermodels.StreamInSpec_Path); ok {
			path = p.Path
			v.logger.Info("vcontainer-stream-in-path", lager.Data{"path": path})
		}

		if u, ok := streamInSpec.Part.(*vcontainermodels.StreamInSpec_User); ok {
			user = u.User
			v.logger.Info("vcontainer-stream-in-user", lager.Data{"user": user})
		}

		if !started && path != "" && user != "" {
			started = true
			v.logger.Info("vcontainer-stream-in-started")
			containerInterop = interop.NewContainerInterop(containerId, v.logger)
			if err = containerInterop.Open(); err != nil {
				v.logger.Error("vcontainer-stream-in-container-interop-failed-to-open", err)
				return verrors.New("failed to open container interop.")
			}
			defer containerInterop.Close()
			filePath, fileToExtract, err = containerInterop.PrepareExtractFile(path)

			if fileToExtract != nil {
				defer fileToExtract.Close()
			}
			v.logger.Info("vcontainer-stream-in-container-interop-prepared-file", lager.Data{"filename": filePath})
		}

		// write a file
		if content, ok := streamInSpec.Part.(*vcontainermodels.StreamInSpec_Content); ok {
			v.logger.Info("vcontainer-stream-in-got", lager.Data{"length": len(content.Content)})
			if fileToExtract == nil {
				return verrors.New("no file prepared for interop.")
			}
			_, err := fileToExtract.Write(content.Content)
			if err != nil {
				return verrors.New("write file failed.")
			}
		}
	}
	if containerInterop != nil && filePath != "" {
		err := containerInterop.DispatchExtractFileTask(filePath, path, user)
		if err != nil {
			return verrors.New("dispatch extract file task failed.")
		}
	}
	// wait for the exit
	server.SendAndClose(&vcontainermodels.StreamInResponse{
		Message: "ok",
	})
	return nil
}

func (v *vcontainerHandler) StreamOut(in *vcontainermodels.StreamOutSpec, server vcontainermodels.VContainer_StreamOutServer) error {
	v.logger.Info("vcontainer-stream-out")
	containerId, err := v.getContainerId(server.Context())
	if err != nil {
		return err
	}
	v.logger.Info("vcontainer-stream-out-container-id", lager.Data{"containerid": containerId})
	return verrors.New("not implemented")
}

func (v *vcontainerHandler) Info(ctx context.Context, empty *google_protobuf.Empty) (*vcontainermodels.ContainerInfo, error) {
	v.logger.Info("vcontainer-info")
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}
	v.logger.Info("vcontainer-info-container-id", lager.Data{"containerid": containerId})

	client, err := vcontainer.NewACIClient()
	containerGroup, err := client.GetContainerGroup(config.GetVContainerEnvInstance().ResourceGroup, containerId)
	if err != nil {
		v.logger.Error("vcontainer-property-get-container-group-failed", err)
		return nil, verrors.New("failed to get container group.")
	}

	containerInfo := &vcontainermodels.ContainerInfo{}
	if containerGroup.IPAddress != nil {
		containerInfo.HostIP = containerGroup.IPAddress.IP
		containerInfo.ContainerIP = containerGroup.IPAddress.IP
		for _, port := range containerGroup.IPAddress.Ports {
			containerInfo.MappedPorts = append(containerInfo.MappedPorts, vcontainermodels.PortMapping{
				HostPort:      uint32(port.Port),
				ContainerPort: uint32(port.Port),
			})
		}
	}
	// TODO check whether the State can be mapped.
	containerInfo.State = containerGroup.InstanceView.State

	containerInfo.Properties = &vcontainermodels.Properties{
		Properties: containerGroup.Tags,
	}

	return containerInfo, nil
}

func (v *vcontainerHandler) getContainerId(ctx context.Context) (string, error) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return "", verrors.New("context not correct.")
	}
	containerId := md[vcontainercommon.ContainerIdKey]
	if containerId == nil || len(containerId) == 0 {
		return "", verrors.New("no container id in context.")
	}
	return containerId[0], nil
}
