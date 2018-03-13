package config

import "sync"

type VContainerEnv struct {
	ACIConfig     ACIConfig
	SMBProxy      SMBProxy
	ResourceGroup string
}

var instance *VContainerEnv
var once sync.Once

func GetVContainerEnvInstance() *VContainerEnv {
	once.Do(func() {
		instance = &VContainerEnv{}
		// TODO <HARD CODE NOW>
		instance.ResourceGroup = "andliu"
	})
	return instance
}
