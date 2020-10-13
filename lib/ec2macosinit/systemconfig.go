package ec2macosinit

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	// DefaultsCmd is the path to the script edit macOS defaults
	DefaultsCmd = "/usr/bin/defaults"
	// DefaultsRead is the command to read from a plist
	DefaultsRead = "read"
	// DefaultsReadType is the command to read the type of a parameter from a plist
	DefaultsReadType = "read-type"
	// DefaultsWrite is the command to write a value of a parameter to a plist
	DefaultsWrite = "write"
)

// ModifySysctl contains sysctl values we want to modify
type ModifySysctl struct {
	Value string `toml:"value"`
}

// ModifyDefaults contains the necessary values to change a parameter in a given plist
type ModifyDefaults struct {
	Plist     string `toml:"plist"`
	Parameter string `toml:"parameter"`
	Type      string `toml:"type"`
	Value     string `toml:"value"`
}

// SystemConfigModule contains all necessary configuration fields for running a System Configuration module.
type SystemConfigModule struct {
	SecureSSHDConfig bool             `toml:"secureSSHDConfig"`
	ModifySysctl     []ModifySysctl   `toml:"Sysctl"`
	ModifyDefaults   []ModifyDefaults `toml:"Defaults"`
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
	var defaultsChanged, defaultsUnchanged, defaultsErrors int32
	for _, m := range c.ModifyDefaults {
		wg.Add(1)

		go func(modifyDefault *ModifyDefaults) {
			changed, err := modifyDefaults(modifyDefault)
			if err != nil {
				atomic.AddInt32(&defaultsErrors, 1)
				ctx.Logger.Errorf("Error while attempting to modify default [%s]: %s", modifyDefault.Parameter, err)
			}
			if changed { // changed a property
				atomic.AddInt32(&defaultsChanged, 1)
				ctx.Logger.Infof("Modified default [%s]", modifyDefault.Parameter)
			} else { // did not change a property
				atomic.AddInt32(&defaultsUnchanged, 1)
				ctx.Logger.Infof("Did not modify default [%s]", modifyDefault.Parameter)
			}
			wg.Done()
		}(&m)
	}

	wg.Wait()

	// Craft output message
	totalChanged := sysctlChanged + defaultsChanged
	totalUnchanged := sysctlUnchanged + defaultsUnchanged
	totalErrors := sysctlErrors + defaultsErrors

	message = fmt.Sprintf("system configuration completed with [%d changed / %d unchanged / %d error(s)] out of %d requested changes",
		totalChanged, totalUnchanged, totalErrors, totalChanged+totalUnchanged)

	if totalErrors > 0 {
		return message, fmt.Errorf("ec2macosinit: one or more system configuration changes were unsuccessful")
	}

	return message, nil
}

// modifySysctl modifies a sysctl parameter, if necessary.
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

// modifyDefaults modifies a default, if necessary.
func modifyDefaults(modifyDefault *ModifyDefaults) (changed bool, err error) {
	// Check current type for parameter we're looking to set
	err = checkDefaultsType(modifyDefault)
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: error while checking default type: %s", err)
	}

	// Check to ensure that the value matches the type
	err = checkValueMatchesType(modifyDefault)
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: value %s did not match type %s for plist %s, parameter %s", modifyDefault.Value, modifyDefault.Type, modifyDefault.Plist, modifyDefault.Parameter)
	}

	// Check to see if current value already matches
	err = checkDefaultsValue(modifyDefault)
	if err == nil {
		return false, err // Exit early if value is already set correctly, otherwise attempt to update value
	}

	// If the values did not match, update value in the plist
	err = updateDefaultsValue(modifyDefault)
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: unable to update value for plist %s, parameter %s to value %s", modifyDefault.Plist, modifyDefault.Parameter, modifyDefault.Value)
	}

	// Validate new value
	err = checkDefaultsValue(modifyDefault)
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: verification failed for updating value for plist %s, parameter %s", modifyDefault.Plist, modifyDefault.Parameter)
	}

	return true, nil
}

// checkDefaultsType checks the type of the parameter in the plist. For reference, the valid types
// can be found here: https://ss64.com/osx/defaults.html.
func checkDefaultsType(modifyDefault *ModifyDefaults) (err error) {
	// Check type of current parameter in plist
	readTypeCmd := []string{DefaultsCmd, DefaultsReadType, modifyDefault.Plist, modifyDefault.Parameter}
	out, err := executeCommand(readTypeCmd, "", []string{})
	if err != nil {
		return err
	}

	// Extract the type by removing "Type is" in front and removing whitespace
	currentType := strings.TrimSpace(strings.Replace(out.stdout, "Type is", "", 1))
	switch modifyDefault.Type {
	// Only implemented for bool[ean] now, more types to be implemented later
	case "bool", "boolean":
		if currentType != "boolean" {
			fmt.Errorf("ec2macosinit: parameter types did not match - expected: (bool, boolean), actual: %s", currentType)
		}
	}

	return nil
}

// checkValueMatchesType checks the the value passed in matches the type.
func checkValueMatchesType(modifyDefault *ModifyDefaults) (err error) {
	// Check to ensure that the value can be converted to the correct type
	switch modifyDefault.Type {
	// Only implemented for bool[ean] now, more types to be implemented later
	case "bool", "boolean":
		_, err = strconv.ParseBool(modifyDefault.Value)
	}
	return err
}

// checkDefaultValue checks the value for a given parameter in a plist.
func checkDefaultsValue(modifyDefault *ModifyDefaults) (err error) {
	// Check value of current parameter in plist
	readCmd := []string{DefaultsCmd, DefaultsRead, modifyDefault.Plist, modifyDefault.Parameter}
	out, err := executeCommand(readCmd, "", []string{})
	if err != nil {
		return err
	}

	// Get value by trimming whitespace
	actualValue := strings.TrimSpace(out.stdout)

	// Run comparisons depending on the parameter's type
	switch modifyDefault.Type {
	// Only implemented for bool[ean] now, more types to be implemented later
	case "bool", "boolean":
		return checkBoolean(modifyDefault.Value, actualValue)
	}

	return nil
}

// updateDefaultsValue updates the value of a parameter in a given plist.
func updateDefaultsValue(modifyDefault *ModifyDefaults) (err error) {
	// Update the value, specifying its type
	writeCmd := []string{DefaultsCmd, DefaultsWrite, modifyDefault.Plist, modifyDefault.Parameter, "-" + modifyDefault.Type, modifyDefault.Value}
	_, err = executeCommand(writeCmd, "", []string{})
	return err
}

// checkBoolean is designed to convert both inputs into a boolean and compare.
func checkBoolean(expectedValue, actualValue string) (err error) {
	// Convert our expected value into a boolean
	expectedOutput, err := strconv.ParseBool(expectedValue)
	if err != nil {
		return err
	}

	// Convert our actual value into a boolean
	actualOutput, err := strconv.ParseBool(actualValue)
	if err != nil {
		return err
	}

	if expectedOutput != actualOutput {
		return fmt.Errorf("ec2macosinit: boolean values did not match - expected: %v, actual: %v", expectedOutput, actualOutput)
	} else {
		return nil
	}
}
