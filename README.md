# Use the container service as the backend of diego cell
The cloud providers, say the Azure, Amazon provide a hosted environment for running containers in the cloud environment.

When using the container service in the cloud environment, there is no need to manage the underlying compute infrastructure, the solution provider handles this management for you. And the containers are charged by the second for each running container. this would be more efficient in the infrastructure cost.

This  allows you to take advantage of both the capabilities of Cloudfoundry and the management value and cost benefit of the container service.

So we propose a solution for using the container service in the diego cell, and let's call this solution Virtual Container Infrastructure(VCI)

## Components
The VCI consists of the VContainer Adapter running in the rep process, and the VContainer Adapter would call apis exposed by one GPRC Service running in a seperate process called VContainer.

## VGarden&VContainer
The vgarden and the vcontainer shares the same api as the garden.Client and the garden.Container.
And the implementation are based on the GRPC service.
When one call to the original garden.Client and garden.Container happens when the vcontainer=true,
the rpc would be called to do the real job.

## VContainer Adapter
The vcontainer are the client side of the VContainer GRPC service, and it's running on the rep process.
This adapter is created based on the configuration "diego.rep.use_vcontainer", if this value is true. then the vcontainer would be constructed and use as the garden.Client and the garden.Container in the initialization step of rep.

## Daemons in the container
The daemon in the container is actually a bash shell script. The script will iterate the constant task folder and the one off task folder to execute them.

```bash
#!/bin/bash
get_task_folder_path(){
    echo "/swaproot/one_off_tasks/$1"
}

get_first_sh_file(){
    unset current_file
    current_file=$(ls -tcr $1/*.sh 2>/dev/null | head -1)
}

get_stream_in_task_to_execute(){
    local folder=$(get_task_folder_path "stream_in")
    get_first_sh_file $folder
}

get_run_task_to_execute(){
    local folder=$(get_task_folder_path "run")
    get_first_sh_file $folder
}

get_stream_out_task_to_execute(){
    local folder=$(get_task_folder_path "stream_out")
    get_first_sh_file $folder
}

interop_daemon(){
    while true; do
        sleep 1
        
        get_stream_in_task_to_execute
        echo "Executing $current_file."
        local have_done=true
        if [ ! -z "$current_file" ]; then
            have_done=false
            echo "Executing $current_file."
            cat $current_file
            bash -c $current_file
            sleep 1
            echo "Removing $current_file."
            mv $current_file $current_file.executed
            echo "Removed $current_file."
        fi
        
        if $have_done; then
            have_done=true
            get_run_task_to_execute
            if [ ! -z "$current_file" ]; then
                have_done=false
                echo "Executing $current_file."
				cat $current_file
                bash -c $current_file &
                sleep 1
                echo "Removing $current_file."
                mv $current_file $current_file.executed
                echo "Removed $current_file."
            fi
        fi

        if $have_done; then
            have_done=true
            get_stream_out_task_to_execute
            if [ ! -z "$current_file" ]; then
                have_done=false
                echo "Executing $current_file."
				cat $current_file
                bash -c $current_file
                sleep 1
                echo "Removing $current_file."
                mv $current_file $current_file.executed
                echo "Removed $current_file."
            fi
        fi
        
        unset current_file
        unset have_done
    done
}

interop_daemon
```

## Interop Layer
The interop layer is responsible of interacting with the container running in the cloud environment.
```go
type ContainerInterop interface {
	// prepare the container interop, return the entry point commands, and the volume/volume mount
	Prepare() (*ContainerInteropInfo, error)
	// prepare the swap root for the container
	Open() error
	// unmount the swap root folder in the host.
	Close() error
	// dispatch one command to the container.
	DispatchRunTask(cmd RunTask) (string, error)
	// copy the src folder to the prepared swap root. and write one task into it.
	DispatchStreamOutTask(outSpec *vcontainermodels.StreamOutSpec) (string, string, error)
	// open the file for read.
	OpenStreamOutFile(fileId string) (*os.File, error)
	// dispatch a copy folder task.
	DispatchFolderTask(src, dest string) (string, error)
	// prepare an file opened for writing, so the extract task can extract it to the dest folder in the container.
	PrepareExtractFile(dest string) (string, *os.File, error)
	// dispatch an extract file task.
	DispatchExtractFileTask(fileToExtract, dest, user string) (string, error)
	// wait for the task with the taskId exit.
	WaitForTaskExit(taskId string) error
	// get the pid from the task id.
	GetProcessIdFromTaskId(taskId string) (int64, error)
	// judge whether the task exited.
	TaskExited(taskId string) (vcontainermodels.WaitResponse, error)
	// clear the task related files.
	CleanTask(taskId string) error
}
```

Calling to the Prepare() would create one azure share for each container, and this share folder would be mounted to one specific folder in the container.

And the Open() would mount the azure share folder in the host (which the vcontainer process running on) to one temp folder. Then we can store the task scripts and the file need to be streamed in the container there. Close() would umount the folder for clean up.

PrepareExtractFile() would create one file in the share folder, and then the tar stream file would be saved there, and DispatchExtractFileTask() would save a one shell script(/swaproot/one_off_tasks/1/{id}.sh) into the contant tasks folder.

DispatchRunCommand() would save a shell script which contains two parts, one is for executing the command, and the other is save the exit code to one file in (/swaproot/one_off_tasks/1/{id}.sh
) DispatchRunCommand() would save a shell script (which contains two parts, one is for executing the command, and the other is save the exit code to one file in (/swaproot/out/tasks/{id}.exit)

## VProcess
The calling to Run method in the garden.Container api would return a object with this interface
```go
type Process interface {
	ID() string
	Wait() (int, error)
	SetTTY(TTYSpec) error
	Signal(Signal) error
}
```
In the vcontainer environment, this would return a VProcess, the VProcess would interact with the agent runing in the container.
Wait() would wait for the exit file be outputed by the process.
The Signal() would also distribute one task into the one_off_tasks folder. and then the shell script would be picked up by the daemon.

And about the process io, for this stage, we only support the process was suppressed.

# Configurations
A new job named as vcontainer would be scheduled in the rep server.
```yaml
- name: vcontainer
    release: diego
    properties:
      diego:
        vcontainer:
          ca_cert: "((service_cf_internal_ca.certificate))"
          server_cert: "((diego_vcontainer_server.certificate))"
          server_key: "((diego_vcontainer_server.private_key))"
```

```yaml
- type: replace
  path: /instance_groups/name=diego-cell/jobs/name=vcontainer/properties/diego/vcontainer/azure_container_provider_cfg?
  value:
    container_id: "((azure_service_id))"
    container_secret: "((azure_service_secret))"
    optional_param_1: "((azure_tenant_id))"
    location: "((azure_location))"
    subscription_id: "((azure_subscription_id))"
    storage_id: "((azure_storage_id))"
    storage_secret: "((azure_storage_secret))"
```

# SMB Proxy
Since in some place the smb share's ports maybe blocked. so we propose a proxy configuration for the smb share proxy.
```yaml
- type: replace
  path: /instance_groups/name=diego-cell/jobs/name=vcontainer/properties/diego/vcontainer/smb_proxy?
  value:
    ip: "((smb_proxy_ip))"
    port: "((smb_proxy_port))"
```

