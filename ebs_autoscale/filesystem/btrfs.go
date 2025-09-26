package filesystem

import (
	"bytes"
	"fmt"
	"golang.org/x/sys/unix"
	"log/slog"
	"os"
	"os/exec"
)

func init() {
	RegisterBackend("btrfs", func(mountPoint string, options map[string]interface{}) (FileSystem, error) {
		return &BtrfsFileSystem{
			MountPoint: mountPoint,
		}, nil
	})
}

// BtrfsFileSystem implements the FileSystem interface
type BtrfsFileSystem struct {
	MountPoint string
}

// GetMountPoint getter for the FileSystem interface
func (fs BtrfsFileSystem) GetMountPoint() string {
	return fs.MountPoint
}

// CreateFileSystem creates a btrfs file system on the given device
func (fs BtrfsFileSystem) CreateFileSystem(device string) error {

	if err := runCommand("mkfs.btrfs", "-f", "-d", "single", device); err != nil {
		return err
	}

	if err := runCommand("mount", device, fs.MountPoint); err != nil {
		return err
	}

	slog.Info("CreateFileSystem: writing to fstab")
	f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	fsTabLine := fmt.Sprintf("%s\t%s\tbtrfs\tdefaults\t0\t0\n", device, fs.MountPoint)

	if _, err = f.WriteString(fsTabLine); err != nil {
		return err
	}

	return nil
}

// runCommand is a convenience method that wraps a system call
func runCommand(prog string, arg ...string) error {

	cmd := exec.Command(prog, arg...)

	slog.Debug(fmt.Sprintf("runCommand:  %s", cmd.String()))

	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runCommand: %s: %w: %s: %s", cmd.String(), err, outb.String(), errb.String())
	}
	return nil
}

// GrowFileSystem adds a device to the existing btrfs file system and grows the underlying partition
func (fs BtrfsFileSystem) GrowFileSystem(device string) error {

	if err := runCommand("btrfs", "device", "add", device, fs.MountPoint); err != nil {
		return err
	}

	if err := runCommand("btrfs", "balance", "start", "-m", fs.MountPoint); err != nil {
		return err
	}

	return nil
}

// Stat stats the underlying file system. Returns total_space, used_space, free_space in bytes
func (fs BtrfsFileSystem) Stat() (uint64, uint64, uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(fs.GetMountPoint(), &stat)
	if err != nil {
		return 0, 0, 0, err
	}
	freeSpace := stat.Bfree * uint64(stat.Bsize)
	totalSpace := stat.Blocks * uint64(stat.Bsize)
	usage := totalSpace - freeSpace
	return totalSpace, usage, freeSpace, nil
}
