package vgarden

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code.cloudfoundry.org/executor/model"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/virtualcloudfoundry/goaci"
	"github.com/virtualcloudfoundry/goaci/aci"
	"github.com/virtualcloudfoundry/vcontainer/helpers/mount"
)

type VContainer struct {
	inner  garden.Container
	logger lager.Logger
}

func NewVContainer(inner garden.Container, logger lager.Logger) garden.Container {
	return &VContainer{
		inner:  inner,
		logger: logger,
	}
}

func (container *VContainer) Handle() string {
	return container.inner.Handle()
	// return container.handle
}

func (container *VContainer) Stop(kill bool) error {
	return container.inner.Stop(kill)
	// return container.connection.Stop(container.handle, kill)
}

func (container *VContainer) Info() (garden.ContainerInfo, error) {
	return container.inner.Info()
	// return garden.ContainerInfo{}, nil
	// return container.connection.Info(container.handle)
}

func (container *VContainer) StreamIn(spec garden.StreamInSpec) error {
	// TODO move share creation logic to here.
	return container.inner.StreamIn(spec)
}

func (container *VContainer) StreamOut(spec garden.StreamOutSpec) (io.ReadCloser, error) {
	return container.inner.StreamOut(spec)
	// return nil, nil
	// return container.connection.StreamOut(container.handle, spec)
}

func (container *VContainer) CurrentBandwidthLimits() (garden.BandwidthLimits, error) {
	// return garden.BandwidthLimits{}, nil
	return container.inner.CurrentBandwidthLimits()
	// return container.connection.CurrentBandwidthLimits(container.handle)
}

func (container *VContainer) CurrentCPULimits() (garden.CPULimits, error) {
	// return container.connection.CurrentCPULimits(container.handle)
	return container.inner.CurrentCPULimits()
}

func (container *VContainer) CurrentDiskLimits() (garden.DiskLimits, error) {
	// return container.connection.CurrentDiskLimits(container.handle)
	return container.inner.CurrentDiskLimits()
}

func (container *VContainer) CurrentMemoryLimits() (garden.MemoryLimits, error) {
	// return container.connection.CurrentMemoryLimits(container.handle)
	return container.inner.CurrentMemoryLimits()
}

func (container *VContainer) Run(spec garden.ProcessSpec, io garden.ProcessIO) (garden.Process, error) {
	if true { // for debug go router.
		return container.inner.Run(spec, io)
	}

	handle := container.Handle()
	if len(handle) == len("3fa79176-be9a-4496-bda2-cdaa06c32480") { // skip for the staging container for now.
		container.logger.Info("#########(andliu) skip for the stage container.")
		return container.inner.Run(spec, io)
	}
	if strings.Contains(spec.Path, "healthcheck") || strings.Contains(spec.Path, "sshd") {
		// skip health check and the sshd.
		// container.logger.Info("#########(andliu) skip for the health check and the sshd.")
		return container.inner.Run(spec, io)
	}

	container.logger.Info("#########(andliu) vcontainer.go run container with spec:", lager.Data{"spec": spec})
	var azAuth *goaci.Authentication

	executorEnv := model.GetExecutorEnvInstance()
	config := executorEnv.Config.ContainerProviderConfig
	azAuth = goaci.NewAuthentication(azure.PublicCloud.Name, config.ContainerId, config.ContainerSecret, config.SubscriptionId, config.OptionalParam1)

	aciClient, err := aci.NewClient(azAuth)
	if err == nil {
		containerGroupGot, err, code := aciClient.GetContainerGroup(executorEnv.ResourceGroup, handle)
		if err == nil {
			for idx, _ := range containerGroupGot.ContainerGroupProperties.Volumes {
				containerGroupGot.ContainerGroupProperties.Volumes[idx].AzureFile.StorageAccountKey =
					executorEnv.Config.ContainerProviderConfig.StorageSecret
			}
			vs := NewVStream(container.logger)
			mountedRootFolder, err := vs.MountContainerRoot(handle)
			options := []string{
				"vers=3.0",
				fmt.Sprintf("username=%s", model.GetExecutorEnvInstance().Config.ContainerProviderConfig.StorageId),
				fmt.Sprintf("password=%s", model.GetExecutorEnvInstance().Config.ContainerProviderConfig.StorageSecret),
				"dir_mode=0777,file_mode=0777,serverino",
			}
			// TODO because 445 port is blocked in microsoft, so we use the proxy to do it...
			options = append(options, "port=444")

			shareName := vs.GetContainerSwapRootShareFolder(handle)
			azureFilePath := fmt.Sprintf("//40.65.190.119/%s", shareName) //fmt.Sprintf("//%s.file.core.windows.net/%s", storageID, shareName)
			mounter := mount.NewMounter()

			err = mounter.Mount(azureFilePath, mountedRootFolder, "cifs", options)
			args := []string{}
			for _, arg := range spec.Args {
				argToUse := arg
				if arg == "" {
					argToUse = "\"\""
				}
				args = append(args, argToUse)
			}
			realCommand := fmt.Sprintf("%s %s", spec.Path, strings.Join(args, " "))
			var vcapScriptFile *os.File
			vscapScriptFilePath := filepath.Join(mountedRootFolder, GetVCapScript())
			if _, err := os.Stat(vscapScriptFilePath); err != nil {
				if os.IsNotExist(err) {
					// 不存在
					vcapScriptFile, err = os.OpenFile(vscapScriptFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0777)
					vcapScriptFile.WriteString("#!/bin/bash\n")
					vcapScriptFile.Close()
				}
			}
			vcapScriptFile, err = os.OpenFile(vscapScriptFilePath, os.O_APPEND|os.O_WRONLY, 0777)

			if err != nil {
				container.logger.Info("#######(andliu) open file failed.", lager.Data{"err": err.Error()})
			}
			_, err = vcapScriptFile.WriteString(realCommand)
			if err != nil {
				container.logger.Info("#######(andliu) WriteString file failed.", lager.Data{"err": err.Error()})
			}
			err = vcapScriptFile.Close()
			if err != nil {
				container.logger.Info("#######(andliu) close file failed.", lager.Data{"err": err.Error()})
			}
			err = mounter.Umount(mountedRootFolder)

			for idx, _ := range containerGroupGot.Containers {
				containerGroupGot.Containers[idx].ContainerProperties.EnvironmentVariables = []aci.EnvironmentVariable{}
				for _, envStr := range spec.Env {
					splits := strings.Split(envStr, "=")
					if splits[1] != "" {
						containerGroupGot.Containers[idx].ContainerProperties.EnvironmentVariables =
							append(containerGroupGot.Containers[idx].ContainerProperties.EnvironmentVariables,
								aci.EnvironmentVariable{Name: splits[0], Value: splits[1]})
					} else {
						container.logger.Info("###########(andliu) value is empty.", lager.Data{"envStr": envStr})
					}
				}
				containerGroupGot.Containers[idx].Command = []string{}
				containerGroupGot.Containers[idx].Command = append(containerGroupGot.Containers[idx].Command, "/bin/bash")
				containerGroupGot.Containers[idx].Command = append(containerGroupGot.Containers[idx].Command, "-c")

				// TODO judge whether it's stage.

				// 这个是跑在container里面的脚本。
				// 				const runnerScript = `
				// 	set -e
				// 	{{if .RSync -}}
				// 	rsync -a /tmp/local/ /home/vcap/app/
				// 	{{end -}}
				// 	if [[ ! -z $(ls -A /home/vcap/app) ]]; then
				// 		exclude='--exclude=./app'
				// 	fi
				// 	tar $exclude -C /home/vcap -xzf /tmp/droplet
				// 	chown -R vcap:vcap /home/vcap
				// 	{{if .RSync -}}
				// 	if [[ -z $(ls -A /tmp/local) ]]; then
				// 		rsync -a /home/vcap/app/ /tmp/local/
				// 	fi
				// 	{{end -}}
				// 	command=$1
				// 	if [[ -z $command ]]; then
				// 		command=$(jq -r .start_command /home/vcap/staging_info.yml)
				// 	fi
				// 	exec /tmp/lifecycle/launcher /home/vcap/app "$command" ''
				// `

				// TODO copy the droplet when doing stage.
				// cp - f/tmp/droplet %s/droplet
				var runScript = fmt.Sprintf(`
	echo "##### run"
	echo "#####show root_task.sh content:"
	cat /swaproot/root_task.sh
	echo "#####executing root_task.sh"
	/swaproot/root_task.sh
	echo "#####root ls /swaproot -all"
	ls /swaproot -all
	echo "#####need to run as vcap now."
	su - vcap -c '
	echo "##### ls /swaproot -all"
	export HOME=/home/vcap/app
	echo "##### echo $PORT"
	echo $PORT
	echo "##### echo $APP_ROOT"
	echo $APP_ROOT
	export PORT=8080
	export APP_ROOT=/home/vcap/app
	ls /swaproot -all
	echo "##### pwd"
	pwd
	echo "##### ls ."
	ls . -all
	echo "##### ls ../ -all"
	ls ../ -all
	echo "##### cat vcap_task.sh"
	cat /swaproot/vcap_task.sh
	echo "##### ls /home/vcap"
	ls /home/vcap
	echo "##### run vcap_task.sh"
	/swaproot/vcap_task.sh
	echo "##### ls /home/vcap"
	ls /home/vcap
	echo "post actions.(TODO,copy the /tmp/droplet to the share folder.)"
	'
`)
				containerGroupGot.Containers[idx].Command = append(containerGroupGot.Containers[idx].Command, runScript)
				container.logger.Info("###########(andliu) final command is.", lager.Data{"command": containerGroupGot.Containers[idx].Command})
			}
			// prepare the commands.
			container.logger.Info("#########(andliu) update container group got.", lager.Data{"containerGroupGot": *containerGroupGot})
			_, err = aciClient.UpdateContainerGroup(executorEnv.ResourceGroup, container.inner.Handle(), *containerGroupGot)
			retry := 0
			for err != nil && retry < 10 {
				container.logger.Info("#########(andliu) update container group failed(Run).", lager.Data{"err": err.Error()})
				time.Sleep(60 * time.Second)
				_, err = aciClient.UpdateContainerGroup(executorEnv.ResourceGroup, container.inner.Handle(), *containerGroupGot)
				retry++
			}
			if err != nil {
				container.logger.Info("##########(andliu) update container failed.", lager.Data{"err": err.Error()})
			}
		} else {
			container.logger.Info("#########(andliu) vcontainer.go L92 got container in vcontainer.",
				lager.Data{"err": err.Error(), "code": code})
		}
	} else {
		container.logger.Info("########(andliu) Run in VContainer failed.", lager.Data{"err": err.Error(), "spec": spec})
	}

	return container.inner.Run(spec, io)
}

func (container *VContainer) Attach(processID string, io garden.ProcessIO) (garden.Process, error) {
	// return container.connection.Attach(container.handle, processID, io)
	return container.inner.Attach(processID, io)
	// return nil, nil
}

func (container *VContainer) NetIn(hostPort, containerPort uint32) (uint32, uint32, error) {
	// return 0, 0, container.connection.NetIn(container.handle, hostPort, containerPort)
	return container.inner.NetIn(hostPort, containerPort)
	// return 0, 0, nil
}

func (container *VContainer) NetOut(netOutRule garden.NetOutRule) error {
	// return container.connection.NetOut(container.handle, netOutRule)
	return container.inner.NetOut(netOutRule)
	// return nil
}

func (container *VContainer) BulkNetOut(netOutRules []garden.NetOutRule) error {
	// return container.connection.BulkNetOut(container.handle, netOutRules)
	return container.inner.BulkNetOut(netOutRules)
	// return nil
}

func (container *VContainer) Metrics() (garden.Metrics, error) {
	// return container.connection.Metrics(container.handle)
	return container.inner.Metrics()
	// return garden.Metrics{}, nil
}

func (container *VContainer) SetGraceTime(graceTime time.Duration) error {
	// return container.connection.SetGraceTime(container.handle, graceTime)
	return container.inner.SetGraceTime(graceTime)
	// return nil
}

func (container *VContainer) Properties() (garden.Properties, error) {
	//return container.connection.Properties(container.handle)
	// return garden.Properties{}, nil
	return container.inner.Properties()
}

func (container *VContainer) Property(name string) (string, error) {
	//return "", container.connection.Property(container.handle, name)
	// return "", nil
	return container.inner.Property(name)
}

func (container *VContainer) SetProperty(name string, value string) error {
	//return container.connection.SetProperty(container.handle, name, value)
	return container.inner.SetProperty(name, value)
}

func (container *VContainer) RemoveProperty(name string) error {
	//return container.connection.RemoveProperty(container.handle, name)
	// return nil
	return container.inner.RemoveProperty(name)
}
