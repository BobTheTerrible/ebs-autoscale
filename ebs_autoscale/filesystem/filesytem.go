package filesystem

import "fmt"

type FileSystem interface {
	// CreateFileSystem physically creates the file system on the device
	CreateFileSystem(device string) error
	// GrowFileSystem grows the file system across an additional device
	GrowFileSystem(device string) error
	// GetMountPoint returns the file system mount point
	GetMountPoint() string
	// Stat stats the underlying file system. Returns total_size, used_space, free_space in bytes
	Stat() (uint64, uint64, uint64, error)
}

var backends = map[string]func(mountPoint string, options map[string]interface{}) (FileSystem, error){}


// RegisterBackend allows adding a new filesystem type to the registry
func RegisterBackend(name string, fsConstructor func(mountPoint string, options map[string]interface{}) (FileSystem, error)) {
	backends[name] = fsConstructor
}

// GetFileSystem returns the configured filesystem backend
func GetFileSystem(fsType string, mountPoint string, options map[string]interface{}) (FileSystem, error) {
	if constructor, exists := backends[fsType]; exists {
		return constructor(mountPoint, options)
	}
	return nil, fmt.Errorf("unsupported filesystem type: %s", fsType)
}
