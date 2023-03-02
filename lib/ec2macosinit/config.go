package ec2macosinit

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// InitConfig contains all fields expected from an init.toml file as well as things shared by all parts
// of the application.
type InitConfig struct {
	HistoryFilename   string
	HistoryPath       string
	IMDS              IMDSConfig
	InstanceHistory   []History
	Log               *Logger
	Modules           []Module `toml:"Module"`
	ModulesByPriority [][]Module
	FatalCounts       FatalCount
}

// Number of runs resulting in fatal exits in a single boot before giving up
const PerBootFatalLimit = 100

// ReadConfig reads the configuration file and decodes it into the InitConfig struct.
func (c *InitConfig) ReadConfig(fileLocation string) (err error) {
	// Read file
	rawConfig, err := os.ReadFile(fileLocation)
	if err != nil {
		return fmt.Errorf("ec2macosinit: error reading config file located at %s: %s\n", fileLocation, err)
	}

	// Decode from TOML to InitConfig struct
	_, err = toml.Decode(string(rawConfig), c)
	if err != nil {
		return fmt.Errorf("ec2macosinit: error decoding config: %s\n", err)
	}

	return nil
}

// ValidateConfig validates all modules and identifies type.
func (c *InitConfig) ValidateAndIdentify() (err error) {
	// Create keySet to store used keys
	keySet := map[string]struct{}{}

	// Loop through every module and check a few things...
	for i := 0; i < len(c.Modules); i++ {
		// Identify module type
		err := c.Modules[i].identifyModule()
		if err != nil {
			return fmt.Errorf("ec2macosinit: error while identifying module: %s\n", err)
		}

		// Validate individual module
		err = c.Modules[i].validateModule()
		if err != nil {
			return fmt.Errorf("ec2macosinit: error found in module (type: %s, priority: %d): %s\n", c.Modules[i].Type, c.Modules[i].PriorityGroup, err)
		}

		// Check that key name is unique for the current configuration
		if _, ok := keySet[c.Modules[i].Name]; !ok {
			// Key hasn't been used yet - add key to the set
			keySet[c.Modules[i].Name] = struct{}{}
		} else {
			return fmt.Errorf("ec2macosinit: duplicate name found in config:%s\n", c.Modules[i].Name)
		}
	}

	return nil
}

// PrepareModules takes all modules and sorts them according to priority into the ModulesByPriority slice.
func (c *InitConfig) PrioritizeModules() (err error) {
	for _, m := range c.Modules {
		// Expand capacity of ModulesByPriority, as needed
		for m.PriorityGroup > cap(c.ModulesByPriority) {
			c.ModulesByPriority = append(c.ModulesByPriority, []Module{})
		}
		// If needed, expand ModulesByPriority to needed length
		if m.PriorityGroup > len(c.ModulesByPriority) {
			c.ModulesByPriority = c.ModulesByPriority[:m.PriorityGroup]
		}
		// Append module at correct priority level
		c.ModulesByPriority[m.PriorityGroup-1] = append(c.ModulesByPriority[m.PriorityGroup-1], m)
	}

	return nil
}

// RetriesExceeded checks if the number of previous fatal exits exceeds the limit.
func (c *InitConfig) RetriesExceeded() (exceeded bool, err error) {
	// Check for the existence of the temporary file and get the current fatal count
	err = c.FatalCounts.readFatalCount()
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: unable to read fatal counts: %s", err)
	}
	// If there have been more than the limit of fatal exits, return true
	if c.FatalCounts.Count > PerBootFatalLimit {
		return true, nil
	}
	// Otherwise, continue
	return false, nil
}
