package paths

import "path/filepath"

const (
	// DefaultBaseDirectory is the root directory in which other paths are based upon.
	DefaultBaseDirectory = "/usr/local/aws/ec2-macos-init"
)

const (
	// InitTOML is the filename of the configuration for ec2-macos-init.
	InitTOML = "init.toml"
	// HistoryJSON is the filename of the per-instance persisted history state,
	// used to store on disk.
	HistoryJSON = "history.json"
)

const (
	// instancesHistoryDirname is the name of the directory under which history
	// files are stored. See path builders below for usages.
	instancesHistoryDirname = "instances"
)

// AllInstancesHistory returns the path where all instances' history is,
// relative to given base directory.
func AllInstancesHistory(base string) string {
	return filepath.Join(base, instancesHistoryDirname)
}

// InstanceHistory returns the path where the *specified* instance (given by its
// instance ID) is.
func InstanceHistory(base string, instanceID string) string {
	return filepath.Join(base, instancesHistoryDirname, instanceID)
}
