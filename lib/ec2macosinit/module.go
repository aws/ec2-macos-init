package ec2macosinit

import (
	"fmt"
	"strconv"

	"github.com/google/go-cmp/cmp"
)

// Module contains a few fields common to all Module types and containers for the configuration of any
// potential module type.
type Module struct {
	Type               string
	Success            bool
	Name               string             `toml:"Name"`
	PriorityGroup      int                `toml:"PriorityGroup"`
	FatalOnError       bool               `toml:"FatalOnError"`
	RunOnce            bool               `toml:"RunOnce"`
	RunPerBoot         bool               `toml:"RunPerBoot"`
	RunPerInstance     bool               `toml:"RunPerInstance"`
	CommandModule      CommandModule      `toml:"Command"`
	SSHKeysModule      SSHKeysModule      `toml:"SSHKeys"`
	UserDataModule     UserDataModule     `toml:"UserData"`
	NetworkCheckModule NetworkCheckModule `toml:"NetworkCheck"`
}

// validateModule performs the following checks:
//   1. Check that there is exactly one Run type set
//   2. Check that Priority is set and is not less than 1
func (m *Module) validateModule() (err error) {
	// Check that there is exactly one Run type set
	var runs int8
	if m.RunOnce {
		runs++
	}
	if m.RunPerBoot {
		runs++
	}
	if m.RunPerInstance {
		runs++
	}
	if runs != 1 {
		return fmt.Errorf("ec2macosinit: incorrect number of run types\n")
	}

	// Check that Priority is set and not 0 or negative (must be 1 or greater)
	if m.PriorityGroup < 1 {
		return fmt.Errorf("ec2macosinit: module priority is unset or less than 1\n")
	}

	return nil
}

// identifyModule assigns a type to a module by comparing the empty struct for that module with the value provided.
// This approach requires that a given module only have a single Type.
func (m *Module) identifyModule() (err error) {
	if !cmp.Equal(m.CommandModule, CommandModule{}) {
		m.Type = "command"
		return nil
	}
	if !cmp.Equal(m.SSHKeysModule, SSHKeysModule{}) {
		m.Type = "sshkeys"
		return nil
	}
	if !cmp.Equal(m.UserDataModule, UserDataModule{}) {
		m.Type = "userdata"
		return nil
	}
	if !cmp.Equal(m.NetworkCheckModule, NetworkCheckModule{}) {
		m.Type = "networkcheck"
		return nil
	}

	return fmt.Errorf("ec2macosinit: unable to identify module type\n")
}

// generateHistoryKey takes a module and generates a key to be used in the instance history for that module.
// History Key Format: key = m.PriorityLevel_RunType_m.Type_m.Name
func (m *Module) generateHistoryKey() (key string) {
	// Generate key
	var runType string
	if m.RunOnce {
		runType = "RunOnce"
	}
	if m.RunPerInstance {
		runType = "RunPerInstance"
	}
	if m.RunPerBoot {
		runType = "RunPerBoot"
	}
	return strconv.Itoa(m.PriorityGroup) + "_" + runType + "_" + m.Type + "_" + m.Name
}

// ShouldRun determines if a module should be run, given a current instance ID and history. There are three cases:
// 1. RunPerBoot - The module should run every boot, no matter what. The simplest case.
// 2. RunPerInstance - The module should run once on every instance. Here we must look for the current instance ID
//    in the instance history and if found, compare the current module's key with all successfully run keys. If
//    not found, run the module. If found and unsuccessful, run the module. If found and successful, skip.
// 3. RunOnce - The module should run once, ever. The process here is similar to RunPerInstance except the key must
//    be searched for in every instance history. If not found, run the module. If found and unsuccessful, run the
//    module. If found and successful, skip.
func (m *Module) ShouldRun(instanceID string, history []History) (shouldRun bool) {
	// RunPerBoot runs every time
	if m.RunPerBoot {
		return true
	}

	// The rest will use the history key
	key := m.generateHistoryKey()

	// RunPerInstance only runs if the module's key doesn't exist in the current instance history and has
	// not run successfully.
	if m.RunPerInstance {
		// Check each instance in the instance history
		for _, instance := range history {
			if instanceID == instance.InstanceID {
				// If the current instance matches an ID in the history, check every module history for that instance
				for _, moduleHistory := range instance.ModuleHistories {
					if key == moduleHistory.Key && moduleHistory.Success {
						// If there is a matching key and it completed successfully, it doesn't need to be run
						return false
					}
				}
				// If there is an instance that matches and no keys match, run the module
				return true
			}
		}
		// If no instances match the instance history, run the module
		return true
	}

	// RunOnce only runs if the module's key doesn't exist in any instance history
	if m.RunOnce {
		for _, instance := range history {
			// Check every module history for that instance
			for _, moduleHistory := range instance.ModuleHistories {
				if key == moduleHistory.Key && moduleHistory.Success {
					// If there is a matching key and it completed successfully, it doesn't need to be run
					return false
				}
			}
		}
		// If no instances match the instance history, run the module
		return true
	}

	// Default here is false, though this position should never be reached. Preference is to not run actions which
	// may be potentially mutating but are misconfigured.
	return false
}
