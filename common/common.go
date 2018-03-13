package common

func GetVCapScript() string {
	return "vcap_task.sh"
}

func GetRootScript() string {
	return "root_task.sh"
}

func GetSwapRoot() string {
	return "/swaproot"
}

func GetBuldInFolders() []string {
	return []string{
		"/tmp"}
}
