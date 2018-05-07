package handlers

import (
	"io"
	"strconv"
	"time"

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

func (v *vprocessHandler) Signal(ctx context.Context, req *vcontainermodels.SignalRequest) (*google_protobuf.Empty, error) {
	// dispatch one task and wait for it to exit.
	v.logger.Info("vprocess-signal")
	var containerInterop interop.ContainerInterop

	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return nil, err
	}

	containerInterop = interop.NewContainerInterop(containerId, v.logger)
	if err = containerInterop.Open(); err != nil {
		v.logger.Error("vproces-signal-container-interop-failed-to-open", err)
		return nil, verrors.New("failed to open container interop.")
	}
	defer containerInterop.Close()
	processId, err := v.getTaskId(ctx)
	if err != nil {
		v.logger.Error("vprocess-signal-get-process-id-failed", err)
		return nil, verrors.New("failed to get process id.")
	}

	killCmd := interop.RunTask{
		User: "",
		Path: "kill",
		Args: []string{"-s", strconv.Itoa(int(req.Signal)), processId},
	}

	cmdID, err := containerInterop.DispatchRunTask(killCmd)
	if err != nil {
		v.logger.Error("vprocess-signal-dispatch-run-command-failed", err)
	}
	v.logger.Info("vprocess-signal-dispatch-run-command-cmd-id", lager.Data{"cmd_id": cmdID})
	return nil, verrors.New("Not implemented")
}

func (v *vprocessHandler) Wait(empty *google_protobuf.Empty, server vcontainermodels.VProcess_WaitServer) error {
	v.logger.Info("vprocess-wait")
	var containerInterop interop.ContainerInterop
	ctx := server.Context()
	containerId, err := v.getContainerId(ctx)
	if err != nil {
		return err
	}

	containerInterop = interop.NewContainerInterop(containerId, v.logger)
	if err = containerInterop.Open(); err != nil {
		v.logger.Error("vprocess-wait-container-interop-failed-to-open", err)
		return verrors.New("failed to open container interop.")
	}
	defer containerInterop.Close()
	taskId, err := v.getTaskId(ctx)
	for {
		waitResponse, err := containerInterop.TaskExited(taskId)
		if err != nil {
			v.logger.Error("vprocess-wait-task-exited-failed", err)
			return verrors.New("get task exit info failed.")
		}

		err = server.Send(&waitResponse)
		if err != nil {
			if err != io.EOF {
				v.logger.Error("vprocess-wait-send-failed", err,
					lager.Data{
						"container_id": containerId,
						"task_id":      taskId})
				return verrors.New("send failed.")
			} else {
				v.logger.Info("vprocess-wait-send-eof", lager.Data{
					"container_id": containerId,
					"task_id":      taskId})
				break
			}
		}
		if waitResponse.Exited {
			v.logger.Info("vprocess-wait-process-exited", lager.Data{
				"wait_response": waitResponse,
				"container_id":  containerId,
				"task_id":       taskId})
			break
		} else {
			v.logger.Info("vprocess-wait-still-waiting", lager.Data{
				"wait_response": waitResponse,
				"container_id":  containerId,
				"task_id":       taskId})
		}
		// sleep for 5 seconds.
		time.Sleep(time.Second * 2)
	}
	return nil
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

func (v *vprocessHandler) getTaskId(ctx context.Context) (string, error) {
	md, ok := metadata.FromContext(ctx)
	if !ok {
		return "", verrors.New("context not correct.")
	}
	taskID := md[vcontainercommon.TaskIDKey]
	if taskID == nil || len(taskID) == 0 {
		return "", verrors.New("no container id in context.")
	}
	return taskID[0], nil
}
