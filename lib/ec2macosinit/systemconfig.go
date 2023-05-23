package ec2macosinit

import (
	"bufio"
	_ "embed"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// ConfigurationManagementWarning is a header warning for sshd_config
	ConfigurationManagementWarning = "### This file is managed by EC2 macOS Init, changes will be applied on every boot. To disable set secureSSHDConfig = false in /usr/local/aws/ec2-macos-init/init.toml ###"
	// InlineWarning is a warning line for each entry to help encourage users to avoid doing the risky configuration change
	InlineWarning = "# EC2 Configuration: The follow setting is recommended by EC2 and set on boot. Set secureSSHDConfig = false in /usr/local/aws/ec2-macos-init/init.toml to disable.\n"
	// DefaultsCmd is the path to the script edit macOS defaults
	DefaultsCmd = "/usr/bin/defaults"
	// DefaultsRead is the command to read from a plist
	DefaultsRead = "read"
	// DefaultsReadType is the command to read the type of a parameter from a plist
	DefaultsReadType = "read-type"
	// DefaultsWrite is the command to write a value of a parameter to a plist
	DefaultsWrite = "write"
	// sshdConfigFile is the default path for the SSHD configuration file
	sshdConfigFile = "/etc/ssh/sshd_config"
	// ec2SSHDConfigFile is the ssh configs file path
	ec2SSHDConfigFile = "/etc/ssh/sshd_config.d/050-ec2-macos.conf"
	// macOSSSHDConfigDir is Apple's custom ssh configs
	macOSSSHDConfigDir = "/etc/ssh/sshd_config.d"
)

//go:embed assets/ec2-macos-ssh.txt
var ec2SSHData string

var (
	// numberOfBytesInCustomSSHFile is the number of bytes in assets/ec2-macos-ssh.txt
	numberOfBytesInCustomSSHFile = len(ec2SSHData)
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
	SecureSSHDConfig *bool            `toml:"secureSSHDConfig"`
	ModifySysctl     []ModifySysctl   `toml:"Sysctl"`
	ModifyDefaults   []ModifyDefaults `toml:"Defaults"`
}

// Do for the SystemConfigModule modifies system configuration such as sysctl, plist defaults, and secures the SSHD
// configuration file.
func (c *SystemConfigModule) Do(ctx *ModuleContext) (message string, err error) {
	wg := sync.WaitGroup{}

	// Secure SSHD configuration
	var sshdConfigChanges, sshdUnchanged, sshdErrors int32
	if c.SecureSSHDConfig != nil && *c.SecureSSHDConfig {
		wg.Add(1)
		go func() {
			err := writeEC2SSHConfigs()
			if err != nil {
				ctx.Logger.Errorf("Error writing ec2 custom ssh configs: %s", err)
			}
			wg.Done()
		}()
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
		go func(modifyDefault ModifyDefaults) {
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
		}(m)
	}

	// Wait for everything to finish
	wg.Wait()

	// Craft output message
	totalChanged := sysctlChanged + defaultsChanged + sshdConfigChanges
	totalUnchanged := sysctlUnchanged + defaultsUnchanged + sshdUnchanged
	totalErrors := sysctlErrors + defaultsErrors + sshdErrors
	baseMessage := fmt.Sprintf("[%d changed / %d unchanged / %d error(s)] out of %d requested changes",
		totalChanged, totalUnchanged, totalErrors, totalChanged+totalUnchanged)

	if totalErrors > 0 {
		return "", fmt.Errorf("one or more system configuration changes were unsuccessful: %s", baseMessage)
	}

	return "system configuration completed with " + baseMessage, nil
}

// writeEC2SSHConfigs writes custom ec2 ssh configs file
func writeEC2SSHConfigs() (err error) {
	err = os.MkdirAll(macOSSSHDConfigDir, 0755)
	if err != nil {
		return fmt.Errorf("error while attempting to create %s dir: %s", macOSSSHDConfigDir, err)
	}
	f, err := os.OpenFile(ec2SSHDConfigFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error while attempting to create %s file: %s", ec2SSHDConfigFile, err)
	}
	defer f.Close()
	n, err := f.WriteString(ec2SSHData)
	if err != nil {
		return fmt.Errorf("error while writing ec2-macos ssh data on file: %s. %s", ec2SSHDConfigFile, err)
	}
	if n != numberOfBytesInCustomSSHFile {
		return fmt.Errorf("error while writing ec2-macos ssh data on file: %s. %d should equal %d", ec2SSHDConfigFile, n, numberOfBytesInCustomSSHFile)
	}
	return nil
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

	// Attempt to set the value five times, with 100ms in between each attempt
	err = retry(5, 100*time.Millisecond, func() (err error) {
		// Set value
		_, err = executeCommand([]string{"sysctl", value}, "", []string{})
		if err != nil {
			return fmt.Errorf("ec2macosinit: unable to set desired value using sysctl: %s", err)
		}

		// Validate new value
		output, err = executeCommand([]string{"sysctl", "-e", param}, "", []string{})
		if err != nil {
			return fmt.Errorf("ec2macosinit: unable to get current value from sysctl: %s", err)
		}
		if strings.TrimSpace(output.stdout) != value {
			return fmt.Errorf("ec2macosinit: error setting new value using sysctl: %s", output.stdout)
		}

		return nil
	})
	if err != nil {
		return false, err
	}

	return true, nil
}

// modifyDefaults modifies a default, if necessary.
func modifyDefaults(modifyDefault ModifyDefaults) (changed bool, err error) {
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

// checkDefaultsValue checks the value for a given parameter in a plist.
func checkDefaultsValue(modifyDefault ModifyDefaults) (err error) {
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
func updateDefaultsValue(modifyDefault ModifyDefaults) (err error) {
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
	// Launchd can provide status on processes running, this gets that output to be parsed
	out, _ := executeCommand([]string{"launchctl", "list"}, "", []string{})
	// Start a line by line scanner
	scanner := bufio.NewScanner(strings.NewReader(out.stdout))
	for scanner.Scan() {
		// Fetch the next line
		line := scanner.Text()
		// If the line contains "sshd." then the real SSHD is started, not just the dummy sshd wrapper
		if strings.Contains(line, "sshd.") {
			// Strip the newline, then split on tabs to get fields
			launchctlFields := strings.Split(strings.Replace(line, "\n", "", -1), "\t")
			// Take the second field which is the process exit code on start
			retValue, err := strconv.ParseBool(launchctlFields[1])
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: failed to get sshd exit code: %s", err)
			}
			// Return true for zero (good exit) otherwise false
			return !retValue, nil
		}
	}
	// If all of "launchctl list" output doesn't have a status, simply return false since its not running
	return false, nil
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
	sshdFile, err := os.Open(sshdConfigFile)
	if err != nil {
		log.Fatal(err)
	}
	defer sshdFile.Close()

	// Create scanner for the SSHD file
	scanner := bufio.NewScanner(sshdFile)

	// Create a new temporary file, if changes are detected, it will be moved over the existing file
	tempSSHDFile, err := os.CreateTemp("", "sshd_config_fixed.*")
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
			_, err = tempSSHDFile.WriteString("PasswordAuthentication no\n")
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
			_, err = tempSSHDFile.WriteString("UsePAM no\n")
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
			_, err = tempSSHDFile.WriteString("ChallengeResponseAuthentication no\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
			// Changes detected so this will enforce updating the file later
			configChanges = true

		default:
			// Otherwise write the line as is to the temp file without modification
			_, err = tempSSHDFile.WriteString(currentLine + "\n")
			if err != nil {
				return false, fmt.Errorf("ec2macosinit: error writing to %s", tempSSHDFile.Name())
			}
		}
		// Rotate the current line to the last line so that comments can be inserted above rewritten lines
		lastLine = currentLine
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("ec2macosinit: error reading %s: %s", sshdConfigFile, err)
	}

	// If there was a change detected, then copy the file and restart sshd
	if configChanges {
		// Get the current status of SSHD, if its not running, then it should not be started
		sshdRunning, err := c.checkSSHDReturn()
		if err != nil {
			ctx.Logger.Errorf("ec2macosinit: unable to get SSHD status: %s", err)
		}

		// Move the temporary file to the SSHDConfigFile
		err = os.Rename(tempSSHDFile.Name(), sshdConfigFile)
		if err != nil {
			return false, fmt.Errorf("ec2macosinit: unable to save updated configuration to %s", sshdConfigFile)
		}
		// Temporary files have different permissions by design, correct the permissions for SSHDConfigFile
		err = os.Chmod(sshdConfigFile, 0644)
		if err != nil {
			return false, fmt.Errorf("ec2macosinit: unable to set correct permssions of %s", sshdConfigFile)
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
