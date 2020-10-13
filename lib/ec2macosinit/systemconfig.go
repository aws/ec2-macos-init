package ec2macosinit

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
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

// ConfigurationManagementWarning is a header warning for sshd_config
const ConfigurationManagementWarning = "### This file is managed by EC2 macOS Init, changes will be applied on every boot. To disable set secureSSHDConfig = false in /usr/local/aws/ec2-macos-init/init.toml ###"

// InlineWarning is a warning line for each entry to help encourage users to avoid doing the risky configuration change
const InlineWarning = "# EC2 Configuration: The follow setting is recommended by EC2 and set on boot. Set secureSSHDConfig = false in /usr/local/aws/ec2-macos-init/init.toml to disable.\n"

// SSHDConfigFile is the default path for the SSHD configuration file
const SSHDConfigFile = "/etc/ssh/sshd_config"

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
	var sshdConfigChanges, sshdUnchanged, sshdErrors int32
	if c.SecureSSHDConfig {
		wg.Add(1)
		go func() {
			changes, err := c.configureSSHD(ctx)
			if err != nil {
				atomic.AddInt32(&sshdErrors, 1)
				ctx.Logger.Errorf("Error while attempting to correct SSHD configuration: %s", err)
			}
			if changes {
				// Add change for messaging
				atomic.AddInt32(&sshdConfigChanges, 1)
			} else {
				// No changes made
				atomic.AddInt32(&sshdUnchanged, 1)
			}
			wg.Done()
		}()
	}

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
	totalChanged := sysctlChanged + defaultsChanged + sshdConfigChanges
	totalUnchanged := sysctlUnchanged + defaultsUnchanged + sshdUnchanged
	totalErrors := sysctlErrors + defaultsErrors + sshdErrors

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

// checkDefaultsValue checks the value for a given parameter in a plist.
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

// checkSSHDReturn uses launchctl to find the exit code for ssh.plist and returns if it was successful
func (c *SystemConfigModule) checkSSHDReturn() (success bool, err error) {
	// This zsh call allows a fast and small number of lines to parse, it doesn't appear to work without it
	// the grep sshd. strictly looks for a UUID launch result since sshd is special on macOS
	out, err := executeCommand([]string{"/bin/zsh", "-c", "/bin/launchctl list | /usr/bin/grep sshd."}, "", []string{})
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: failed to get sshd status: %s", err)
	}
	// Trim out the newlines since it causes strange results
	launchctlFields := strings.Split(strings.Replace(out.stdout, "\n", "", -1), "\t")
	if len(launchctlFields) < 2 {
		return false, fmt.Errorf("ec2macosinit: failed to parse launchctl list [#%d fields]: %s", len(launchctlFields), err)
	}
	// Trust that the first result is useful enough, more than one running agent happens but still confirms that its running
	retValue, err := strconv.ParseBool(launchctlFields[1])
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: failed to parse sshd status: %s", err)
	}
	// Return the inverse of a bool, since 0 is good, non-zero is bad
	return !retValue, nil

}

// checkAndWriteWarning is a helper function to write out the warning if not present
func checkAndWriteWarning(lastLine string, tempSSHDFile *os.File) (err error) {
	if !strings.Contains(lastLine, "EC2 Configuration") && lastLine != InlineWarning {
		_, err := tempSSHDFile.WriteString(InlineWarning)
		if err != nil {
			return fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
		}
	}
	return nil
}

// configureSSHD scans the SSHConfigFile and writes to a temporary file if changes are detected. If changes are detected
// it replaces the SSHConfigFile. If SSHD is detected as running, it restarts it.
func (c *SystemConfigModule) configureSSHD(ctx *ModuleContext) (configChanges bool, err error) {
	// Look for each thing and fix them if found
	sshdFile, err := os.Open(SSHDConfigFile)
	if err != nil {
		log.Fatal(err)
	}
	defer sshdFile.Close()

	// Create scanner for the SSHD file
	scanner := bufio.NewScanner(sshdFile)

	// Create a new temporary file, if changes are detected, it will be moved over the existing file
	tempSSHDFile, err := ioutil.TempFile("", "sshd_config_fixed.*")
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: error creating %s", tempSSHDFile.Name())
	}
	defer tempSSHDFile.Close()

	// Keep track of line number simply for confirming warning header
	var lineNumber int
	// Track the last line for adding in warning when needed
	var lastLine string
	// Iterate over every line in the file
	for scanner.Scan() {
		lineNumber++
		currentLine := scanner.Text()
		// If this is the first line in the file, look for the warning header and add if missing
		if lineNumber == 1 && currentLine != ConfigurationManagementWarning {
			_, err = tempSSHDFile.WriteString(ConfigurationManagementWarning + "\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			configChanges = true
			lastLine = ConfigurationManagementWarning
		}

		switch {
		// Check if PasswordAuthentication is enabled, if so put in warning and change the config
		// PasswordAuthentication allows SSHD to respond to user password brute force attacks and can result in lowered
		// security, especially if a simple password is set. In EC2, this is undesired and therefore turned off by default
		case strings.Contains(currentLine, "PasswordAuthentication yes"):
			err = checkAndWriteWarning(lastLine, tempSSHDFile)
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Overwrite with desired configuration line
			tempSSHDFile.WriteString("PasswordAuthentication no\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Changes detected so this will enforce updating the file later
			configChanges = true

			// Check if PAM is enabled, if so, put in warning and change the config
			// PAM authentication enables challenge-response authentication which can allow brute force attacks on SSHD
			// In EC2, this is undesired and therefore turned off by default
		case strings.TrimSpace(currentLine) == "UsePAM yes":
			err = checkAndWriteWarning(lastLine, tempSSHDFile)
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Overwrite with desired configuration line
			tempSSHDFile.WriteString("UsePAM no\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Changes detected so this will enforce updating the file later
			configChanges = true

			// Check if Challenge-response is enabled, if so put in warning and change the config
			// Challenge-response authentication via SSHD can allow brute force attacks for SSHD. In EC2, this is undesired
			// and therefore turned off by default
		case strings.Contains(currentLine, "ChallengeResponseAuthentication yes"):
			err = checkAndWriteWarning(lastLine, tempSSHDFile)
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Overwrite with desired configuration line
			tempSSHDFile.WriteString("ChallengeResponseAuthentication no\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Changes detected so this will enforce updating the file later
			configChanges = true

		default:
			// Otherwise write the line as is to the temp file without modification
			tempSSHDFile.WriteString(currentLine + "\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
		}
		// Rotate the current line to the last line so that comments can be inserted above rewritten lines
		lastLine = currentLine
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("ec2macosinit: error reading %s: %s", SSHDConfigFile, err)
	}

	// If there was a change detected, then copy the file and restart sshd
	if configChanges {
		// Get the current status of SSHD, if its not running, then it should not be started
		sshdRunning, err := c.checkSSHDReturn()
		if err != nil {
			ctx.Logger.Errorf("ec2macosinit: unable to get SSHD status %s: %s", SSHDConfigFile, err)
		}

		// Move the temporary file to the SSHDConfigFile
		err = os.Rename(tempSSHDFile.Name(), SSHDConfigFile)
		if err != nil {
			return false, fmt.Errorf("ec2macosinit: unable to save updated configuration to %s", SSHDConfigFile)
		}
		// Temporary files have different permissions by design, correct the permissions for SSHDConfigFile
		err = os.Chmod(SSHDConfigFile, 0644)
		if err != nil {
			return false, fmt.Errorf("ec2macosinit: unable to set correct permssions of %s", SSHDConfigFile)
		}
		// If SSHD was detected as running, then a restart must happen, if it was not running, the work is complete
		if sshdRunning {
			// Unload and load SSHD, the launchctl method for re-loading SSHD with new configuration
			_, err = executeCommand([]string{"/bin/zsh", "-c", "launchctl unload /System/Library/LaunchDaemons/ssh.plist"}, "", []string{})
			if err != nil {
				ctx.Logger.Errorf("ec2macosinit: unable to stop SSHD %s", err)
				return false, fmt.Errorf("ec2macosinit: unable to stop SSHD %s", err)
			}
			_, err = executeCommand([]string{"/bin/zsh", "-c", "launchctl load -w /System/Library/LaunchDaemons/ssh.plist"}, "", []string{})
			if err != nil {
				ctx.Logger.Errorf("ec2macosinit: unable to restart SSHD %s", err)
				return false, fmt.Errorf("ec2macosinit: unable to restart SSHD %s", err)
			}
			// Add the message to state that config was modified and SSHD was correctly restarted
			ctx.Logger.Info("Modified SSHD configuration and restarted SSHD for new configuration")
		} else {
			// Since SSHD was not running, only change the configuration but no restarting is desired
			ctx.Logger.Info("Modified SSHD configuration, did not restart SSHD since it was not running")
		}
	} else {
		// There were no changes detected from desired state, simply exit and let the temp file be
		ctx.Logger.Info("Did not modify SSHD configuration")
	}
	// Return the message to caller for logging
	return configChanges, nil
}
