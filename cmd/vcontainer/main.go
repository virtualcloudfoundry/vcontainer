package main

import (
	"flag"
	"net"
	"os"

	"code.cloudfoundry.org/cfhttp"
	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/consuladapter"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagerflags"
	"code.cloudfoundry.org/locket"
	"github.com/hashicorp/consul/api"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/virtualcloudfoundry/vcontainer/config"
	"github.com/virtualcloudfoundry/vcontainer/grpcserver"
)

var configFilePath = flag.String(
	"config",
	"",
	"The path to the JSON configuration file.",
)

func main() {
	flag.Parse()

	cfg, err := config.NewVContainerConfig(*configFilePath)
	if err != nil {
		panic(err.Error())
	}

	logger, _ := lagerflags.NewFromConfig(cfg.SessionName, cfg.LagerConfig)

	clock := clock.NewClock()

	consulClient, err := consuladapter.NewClientFromUrl(cfg.ConsulCluster)
	if err != nil {
		logger.Fatal("new-consul-client-failed", err)
	}

	_, portString, err := net.SplitHostPort(cfg.ListenAddress)
	if err != nil {
		logger.Fatal("failed-invalid-listen-address", err)
	}

	portNum, err := net.LookupPort("tcp", portString)
	if err != nil {
		logger.Fatal("failed-invalid-listen-port", err)
	}

	tlsConfig, err := cfhttp.NewTLSConfig(cfg.CertFile, cfg.KeyFile, cfg.CaFile)
	if err != nil {
		logger.Fatal("invalid-tls-config", err)
	}

	config.GetVContainerEnvInstance().ACIConfig = cfg.ACIConfig

	config.GetVContainerEnvInstance().SMBProxy = cfg.SMBProxy

	vcontainerServer := grpcserver.NewVContainerServer(logger, cfg.ListenAddress, tlsConfig)
	members := grouper.Members{
		{"vcontainerserver", vcontainerServer},
	}

	if cfg.EnableConsulServiceRegistration {
		registrationRunner := initializeRegistrationRunner(logger, consulClient, portNum, clock)
		members = append(members, grouper.Member{"registration-runner", registrationRunner})
	}

	group := grouper.NewOrdered(os.Interrupt, members)
	monitor := ifrit.Invoke(sigmon.New(group))

	logger.Info("started", lager.Data{"cell-id": cfg.CellID})

	err = <-monitor.Wait()
	if err != nil {
		logger.Error("exited-with-failure", err)
		os.Exit(1)
	}

	logger.Info("exited")
}

func initializeRegistrationRunner(
	logger lager.Logger,
	consulClient consuladapter.Client,
	port int,
	clock clock.Clock) ifrit.Runner {
	registration := &api.AgentServiceRegistration{
		Name: "vcontainer",
		Port: port,
		Check: &api.AgentServiceCheck{
			TTL: "20s",
		},
	}
	return locket.NewRegistrationRunner(logger, registration, consulClient, locket.RetryInterval, clock)
}
