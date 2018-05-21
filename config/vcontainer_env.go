package config

import "sync"

type VContainerEnv struct {
	ACIConfig ACIConfig
	SMBProxy  SMBProxy
}

var instance *VContainerEnv
var once sync.Once

func GetVContainerEnvInstance() *VContainerEnv {
	once.Do(func() {
		instance = &VContainerEnv{}
	})
	return instance
}
