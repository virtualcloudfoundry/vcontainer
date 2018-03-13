package mount

const (
	// Default mount command if mounter path is not specified
	defaultMountCommand = "mount"
)

type Interface interface {
	// Mount mounts source to target as fstype with given options.
	Mount(source string, target string, fstype string, options []string) error
	// Unmount unmounts given target.
	Umount(target string) error
}
