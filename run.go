package main

import (
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/aws/ec2-macos-init/lib/ec2macosinit"
)

// run is the main runner for ec2-macOS-init.  It handles orchestration of the following major pieces:
//   1. Setup instance ID - IMDS must be up and provide an instance ID for later parts of run to work.
//   2. Read init config - Read the init.toml configuration file into the application.
//   3. Validate init config and identify modules - The config then undergoes basic validation and modules are identified.
//   4. Prioritize modules - Modules are sorted by priority into a 2D slice of modules to be run in the correct order later.
//   5. Read instance run history - The history of prior runs is read into the application for comparison of Run type settings.
//   6. Process each module by priority level - All modules are run in priority groups. Each module in a priority level
//      is started in its own goroutine and the group waits for everything in that group to finish. If any module in that
//      group fails and has FatalOnError set, the entire application exits early.
//   7. Write history file - After any run, a history.json file is written to the instance history directory for future runs.
func run(c *ec2macosinit.InitConfig) {

	c.Log.Info("Fetching instance ID from IMDS...")
	// An instance ID from IMDS is a prerequisite for run() to be able to check instance history
	err := SetupInstanceID(c)
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 1), "Unable to get instance ID: %s", err)
	}
	c.Log.Infof("Running on instance %s", c.IMDS.InstanceID)

	// Mark start time
	startTime := time.Now()

	// Read init config
	c.Log.Info("Reading init config...")
	err = c.ReadConfig(path.Join(baseDir, configFile))
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 66), "Error while reading init config file: %s", err)
	}
	c.Log.Info("Successfully read init config")

	// Validate init config and identify modules
	c.Log.Info("Validating config...")
	err = c.ValidateAndIdentify()
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 65), "Error found during init config validation: %s", err)
	}
	c.Log.Info("Successfully validated config")

	// Prioritize modules
	c.Log.Info("Prioritizing modules...")
	err = c.PrioritizeModules()
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 1), "Error preparing and identifying modules: %s", err)
	}
	c.Log.Info("Successfully prioritized modules")

	// Create instance history directories
	c.Log.Info("Creating instance history directories for current instance...")
	err = c.CreateDirectories()
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 73), "Error creating instance history directories: %s", err)
	}
	c.Log.Info("Successfully created directories")

	// Read instance run history
	c.Log.Info("Getting instance history...")
	err = c.GetInstanceHistory()
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 1), "Error getting instance history: %s", err)
	}
	c.Log.Info("Successfully gathered instance history")

	// Process each module by priority level
	var aggregateFatal bool
	var aggFatalModuleName string
	for i := 0; i < len(c.ModulesByPriority); i++ {
		c.Log.Infof("Processing priority level %d (%d modules)...\n", i+1, len(c.ModulesByPriority[i]))
		wg := sync.WaitGroup{}
		// Start every module within the priority level group
		for j := 0; j < len(c.ModulesByPriority[i]); j++ {
			wg.Add(1)
			go func(m *ec2macosinit.Module, h *[]ec2macosinit.History) {
				// Run module if it should be run
				if m.ShouldRun(c.IMDS.InstanceID, *h) {
					c.Log.Infof("Running module [%s] (type: %s, group: %d)\n", m.Name, m.Type, m.PriorityGroup)
					var message string
					var err error
					ctx := &ec2macosinit.ModuleContext{
						Logger: c.Log,
						IMDS:   &c.IMDS,
					}
					// Run appropriate module
					switch t := m.Type; t {
					case "command":
						message, err = m.CommandModule.Do(ctx)
					case "motd":
						message, err = m.MOTDModule.Do(ctx)
					case "sshkeys":
						message, err = m.SSHKeysModule.Do(ctx)
					case "userdata":
						message, err = m.UserDataModule.Do(ctx)
					case "networkcheck":
						message, err = m.NetworkCheckModule.Do(ctx)
					case "systemconfig":
						message, err = m.SystemConfigModule.Do(ctx)
					case "usermanagement":
						message, err = m.UserManagementModule.Do(ctx)
					default:
						message = "unknown module type"
						err = fmt.Errorf("unknown module type")
					}
					if err != nil {
						c.Log.Infof("Error while running module [%s] (type: %s, group: %d) with message: %s and err: %s\n", m.Name, m.Type, m.PriorityGroup, message, err)
						if m.FatalOnError {
							aggregateFatal = true
							aggFatalModuleName = m.Name
						}
					} else {
						// Module was successfully completed
						m.Success = true
						c.Log.Infof("Successfully completed module [%s] (type: %s, group: %d) with message: %s\n", m.Name, m.Type, m.PriorityGroup, message)
					}
				} else {
					// In the case that we choose not to run a module, it is because the module has already succeeded
					// in a prior run. For this reason, we need to pass through the success of the module to history.
					m.Success = true
					c.Log.Infof("Skipping module [%s] (type: %s, group: %d) due to Run type setting\n", m.Name, m.Type, m.PriorityGroup)
				}
				wg.Done()
			}(&c.ModulesByPriority[i][j], &c.InstanceHistory)
		}
		wg.Wait()
		c.Log.Infof("Successfully completed processing of priority level %d\n", i+1)
		// If any module failed which had FatalOnError set, trigger an aggregate fail
		if aggregateFatal {
			break
		}
	}

	// Write history file
	c.Log.Infof("Writing instance history for instance %s...", c.IMDS.InstanceID)
	err = c.WriteHistoryFile()
	if err != nil {
		c.Log.Fatalf(computeExitCode(c, 73), "Error writing instance history file: %s", err)
	}
	c.Log.Info("Successfully wrote instance history")

	// If any module triggered an aggregate fatal, exit 1
	if aggregateFatal {
		c.Log.Fatalf(computeExitCode(c, 1), "Exiting after %s due to failure in module [%s] with FatalOnError set", time.Since(startTime).String(), aggFatalModuleName)
	}

	// Log completion and total run time
	c.Log.Infof("EC2 macOS Init completed in %s", time.Since(startTime).String())
}

// computeExitCode checks to see if the number of fatal retries has been exceeded. If not, it increments the counter,
// stored in a temporary file, and returns the requested exit code. If the count is exceeded, it returns 0 to avoid
// launchd restarting forever due to the KeepAlive setting.
func computeExitCode(c *ec2macosinit.InitConfig, e int) (exitCode int) {
	// Check if other runs have happened this boot and return data about them
	exceeded, err := c.RetriesExceeded()
	if err != nil {
		c.Log.Errorf("Error while getting retry information: %s", err)
		return 1
	}

	// If the count has exceed the limit, return 0
	if exceeded {
		c.Log.Errorf("Number of fatal retries (%d) exceeded, exiting 0 to avoid infinite runs",
			c.FatalCounts.Count)
		return 0
	}

	c.Log.Infof("Fatal [%d/%d] of this boot", c.FatalCounts.Count, ec2macosinit.PerBootFatalLimit)
	// Increment the counter in the temporary file before returning
	err = c.FatalCounts.IncrementFatalCount()
	if err != nil {
		c.Log.Errorf("Unable to write fatal counts to file: %s", err)
	}

	// Return the requested exit code
	return e
}
