package handlers

import (
	"io"

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

func (v *vprocessHandler) Wait(server vcontainermodels.VProcess_WaitServer) error {
	// see whether there's one file.
	for {
		_, err := server.Recv()
		if err != nil {
			if err != io.EOF {
				v.logger.Error("vprocess-wait-recv-failed", err)
				return verrors.New("recv failed.")
			} else {
				break
			}
		}

	}
	return verrors.New("Not implemented")
}

func (v *vcontainerHandler) getProcessId(ctx context.Context) (string, error) {
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
