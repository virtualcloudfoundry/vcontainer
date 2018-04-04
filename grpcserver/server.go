package grpcserver

import (
	"crypto/tls"
	"net"
	"os"

	"github.com/virtualcloudfoundry/vcontainer/handlers"
	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"

	"code.cloudfoundry.org/lager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type vcontainerServerRunner struct {
	listenAddress string
	vgarden       vcontainermodels.VGardenServer
	vcontainer    vcontainermodels.VContainerServer
	vprocess      vcontainermodels.VProcessServer
	logger        lager.Logger
	tlsConfig     *tls.Config
}

func NewVContainerServer(logger lager.Logger, listenAddress string, tlsConfig *tls.Config) vcontainerServerRunner {
	vgardenHandler := handlers.NewVGardenHandler(logger)

	vcontainerHandler := handlers.NewVContainerHandler(logger)

	vprocessHandler := handlers.NewVProcessHandler(logger)

	return vcontainerServerRunner{
		listenAddress: listenAddress,
		vgarden:       vgardenHandler,
		vcontainer:    vcontainerHandler,
		vprocess:      vprocessHandler,
		logger:        logger,
		tlsConfig:     tlsConfig,
	}
}

func (s vcontainerServerRunner) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := s.logger.Session("grpc-server")

	logger.Info("started")
	defer logger.Info("complete")

	lis, err := net.Listen("tcp", s.listenAddress)
	if err != nil {
		logger.Error("failed-to-listen", err)
		return err
	}

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(s.tlsConfig)))
	vcontainermodels.RegisterVGardenServer(server, s.vgarden)
	vcontainermodels.RegisterVContainerServer(server, s.vcontainer)
	vcontainermodels.RegisterVProcessServer(server, s.vprocess)
	errCh := make(chan error)
	go func() {
		errCh <- server.Serve(lis)
	}()

	close(ready)

	select {
	case sig := <-signals:
		logger.Info("signalled", lager.Data{"signal": sig})
		break
	case err = <-errCh:
		logger.Error("failed-to-serve", err)
		break
	}

	server.GracefulStop()
	return err
}
