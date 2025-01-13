package filesystem

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
