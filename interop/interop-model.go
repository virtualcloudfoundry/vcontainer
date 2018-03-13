package interop

import "github.com/virtualcloudfoundry/goaci/aci"

// components to do interop with the container.
// 1. one interop root folder to interop with the container.
// 2. one components managing the tasks dispatched to the the containers
// 	  a. one tasks folder ends with .task
// 3. one daemon to interate with the tasks.
type TaskStep string

const (
	StreamIn  TaskStep = "stream_in"
	Run       TaskStep = "run"
	StreamOut TaskStep = "stream_out"
)

const (
	OneOffTask string = "one_off_tasks"
)

type RunTask struct {
	ID   string
	User string
	Env  []string
	Path string
	Args []string
	Tag  string // for debugging
}

type ContainerInteropInfo struct {
	Cmd          []string
	VolumeMounts []aci.VolumeMount
	Volumes      []aci.Volume
}

func (c *containerInterop) getSwapInFolder() string {
	return "./in"
}

// out is the root folder of the output.
func (c *containerInterop) getSwapOutFolder() string {
	return "./out"
}

func (c *containerInterop) getTaskOutputFolder() string {
	return "tasks"
}

func (c *containerInterop) getStreamOutFolder() string {
	return "streamout"
}

func (c *containerInterop) getSwapRoot() string {
	return "/swaproot"
}

func (c *containerInterop) getVCapScript() string {
	return "vcap_task.sh"
}

func (c *containerInterop) getEntryScript() string {
	return "root_task.sh"
}
