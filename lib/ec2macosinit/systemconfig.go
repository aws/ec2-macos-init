package ec2macosinit

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// SystemConfigModule contains all necessary configuration fields for running a System Configuration module.
type SystemConfigModule struct {
	SecureSSHDConfig bool `toml:"secureSSHDConfig"`
	ModifySysctl     []struct {
		Value string `toml:"value"`
	} `toml:"Sysctl"`
	ModifyDefaults []struct {
		Plist     string `toml:"plist"`
		Parameter string `toml:"parameter"`
		Type      string `toml:"type"`
		Value     string `toml:"value"`
	} `toml:"Defaults"`
}

// Do for the SystemConfigModule modifies system configuration such as sysctl, plist defaults, and secures the SSHD
// configuration file.
func (c *SystemConfigModule) Do(ctx *ModuleContext) (message string, err error) {
	wg := sync.WaitGroup{}
	// Secure SSHD configuration
	// TODO implement secure SSHD configuration

	// Modifications using sysctl
	var sysctlChanged, sysctlUnchanged, sysctlErrors int32
	for _, m := range c.ModifySysctl {
		wg.Add(1)
		go func(val string) {
			changed, err := modifySysctl(val)
			if err != nil {
				atomic.AddInt32(&sysctlErrors, 1)
				ctx.Logger.Errorf("Error while attempting to modify sysctl property [%s]: %s", val, err)
			}
			if changed { // changed a property
				atomic.AddInt32(&sysctlChanged, 1)
				ctx.Logger.Infof("Modified sysctl property [%s]", val)
			} else { // did not change a property
				atomic.AddInt32(&sysctlUnchanged, 1)
				ctx.Logger.Infof("Did not modify sysctl property [%s]", val)
			}
			wg.Done()
		}(m.Value)
	}

	// Modifications using defaults
	for i := 0; i < len(c.ModifyDefaults); i++ {
		wg.Add(1)
		go func() {
			// TODO implement modify defaults
			wg.Done()
		}()
	}

	wg.Wait()

	// Craft output message
	message = fmt.Sprintf("system configuration completed with [%d changed / %d unchanged / %d error(s)] out of %d requested changes",
		sysctlChanged, sysctlUnchanged, sysctlErrors, sysctlChanged+sysctlUnchanged)

	if sysctlErrors > 0 {
		return message, fmt.Errorf("ec2macosinit: one or more system configuration changes were unsuccessful")
	}

	return message, nil
}

// modifySysctl modifies a sysctl parameter, if necessary
func modifySysctl(value string) (changed bool, err error) {
	// Separate parameter
	inputSplit := strings.Split(value, "=")
	if len(inputSplit) != 2 {
		return false, fmt.Errorf("ec2macosinit: unable to split input sysctl value: %s", value)
	}
	param := inputSplit[0]

	// Check current value
	output, err := executeCommand([]string{"sysctl", "-e", param}, "", []string{})
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: unable to get current value from sysctl: %s", err)
	}
	if strings.TrimSpace(output.stdout) == value {
		return false, nil // Exit early if value is already set
	}

	// Set value, if necessary
	_, err = executeCommand([]string{"sysctl", value}, "", []string{})
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: unable to set desired value using sysctl: %s", err)
	}

	// Validate new value
	output, err = executeCommand([]string{"sysctl", "-e", param}, "", []string{})
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: unable to get current value from sysctl: %s", err)
	}
	if strings.TrimSpace(output.stdout) != value {
		return false, fmt.Errorf("ec2macosinit: error setting new value using sysctl: %s", output.stdout)
	}

	return true, nil
}
