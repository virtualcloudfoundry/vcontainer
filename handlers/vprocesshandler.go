package handlers

import (
	"github.com/virtualcloudfoundry/vcontainer/interop"
	"github.com/virtualcloudfoundry/vcontainercommon"
	"google.golang.org/grpc/metadata"

	"code.cloudfoundry.org/lager"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
	"github.com/virtualcloudfoundry/vcontainercommon/verrors"
	context "golang.org/x/net/context"
)

type vprocessHandler struct {
	logger lager.Logger
}

func NewVProcessHandler(logger lager.Logger) *vprocessHandler {
	return &vprocessHandler{
		logger: logger,
	}
}

func (v *vprocessHandler) Signal(context.Context, *vcontainermodels.SignalRequest) (*google_protobuf.Empty, error) {
	return nil, verrors.New("Not implemented")
}

func (v *vprocessHandler) Wait(empty *google_protobuf.Empty, server vcontainermodels.VProcess_WaitServer) error {
	// see whether there's one file.
	// mount the container

	var containerInterop interop.ContainerInterop

	containerId, err := v.getContainerId(server.Context())
	if err != nil {
		return err
	}

	containerInterop = interop.NewContainerInterop(containerId, v.logger)
	if err = containerInterop.Open(); err != nil {
		v.logger.Error("vcontainer-stream-in-container-interop-failed-to-open", err)
		return verrors.New("failed to open container interop.")
	}
	defer containerInterop.Close()

	for {
		err := server.Send(&vcontainermodels.WaitResponse{
			Exited:   false,
			ExitCode: -1,
		})
		if err != nil {
			v.logger.Error("vprocess-wait-recv-failed", err)
			break
			// if err != io.EOF {
			// 	v.logger.Error("vprocess-wait-recv-failed", err)
			// 	return verrors.New("recv failed.")
			// } else {
			// 	break
			// }
		}

	}
	return verrors.New("Not implemented")
}

func (v *vprocessHandler) getContainerId(ctx context.Context) (string, error) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return "", verrors.New("context not correct.")
	}
	containerID := md[vcontainercommon.ContainerIDKey]
	if containerID == nil || len(containerID) == 0 {
		return "", verrors.New("no container id in context.")
	}
	return containerID[0], nil
}

func (v *vprocessHandler) getProcessId(ctx context.Context) (string, error) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return "", verrors.New("context not correct.")
	}
	processID := md[vcontainercommon.ProcessIDKey]
	if processID == nil || len(processID) == 0 {
		return "", verrors.New("no container id in context.")
	}
	return processID[0], nil
}
