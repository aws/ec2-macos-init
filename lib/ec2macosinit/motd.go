package ec2macosinit

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	motdFile = "/etc/motd"
)

// MOTDModule contains all necessary configuration fields for running a MOTD module.
type MOTDModule struct {
	UpdateName bool `toml:"UpdateName"` // UpdateName specifies if the MOTDModule should run or not
}

// Do for MOTDModule gets the OS's current product version and maps the name of the OS to that version. It then writes
// a string with the OS name and product version to /etc/motd.
func (c *MOTDModule) Do(ctx *ModuleContext) (message string, err error) {
	if !c.UpdateName {
		return "Not requested to update MOTD", nil
	}

	// Create the macOS string
	macosStr := "macOS"

	// Create regex pattern to be replaced in the motd file
	motdMacOSExpression, err := regexp.Compile("macOS.*")
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error compiling motd regex pattern: %s", err)
	}

	// Get the os product version number
	osProductVersion, err := getOSProductVersion()
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error while getting product version: %s", err)
	}

	// Get the version name using the os product version number
	versionName := getVersionName(osProductVersion)

	// Create the version string to be written to the motd file
	var motdString string
	if versionName != "" {
		motdString = fmt.Sprintf("%s %s %s", macosStr, versionName, osProductVersion)
	} else {
		motdString = fmt.Sprintf("%s %s", macosStr, osProductVersion)
	}

	// Read in the raw contents of the motd file
	rawFileContents, err := os.ReadFile(motdFile)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error reading motd file: %s", err)
	}

	// Use the regexp object to replace all instances of the pattern with the updated motd version string
	replacedContents := motdMacOSExpression.ReplaceAll(rawFileContents, []byte(motdString))

	// Write the updated contents back to the motd file
	err = os.WriteFile(motdFile, replacedContents, 0644)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error writing updated motd back to file: %s", err)
	}

	return fmt.Sprintf("successfully updated motd file [%s] with version string [%s]", motdFile, motdString), nil
}

// getVersionName maps os product version numbers to version names. A version name will be returned if the mapping is
// known, otherwise it returns an empty string.
func getVersionName(osProductVersion string) (versionName string) {
	// Map product version number to version name
	switch {
	case strings.HasPrefix(osProductVersion, "10.14"):
		versionName = "Mojave"
	case strings.HasPrefix(osProductVersion, "10.15"):
		versionName = "Catalina"
	case strings.HasPrefix(osProductVersion, "11"):
		versionName = "Big Sur"
	case strings.HasPrefix(osProductVersion, "12"):
		versionName = "Monterey"
	case strings.HasPrefix(osProductVersion, "13"):
		versionName = "Ventura"
	case strings.HasPrefix(osProductVersion, "14"):
		versionName = "Sonoma"
	case strings.HasPrefix(osProductVersion, "15"):
		versionName = "Sequoia"
	case strings.HasPrefix(osProductVersion, "26"):
		versionName = "Tahoe"
	}

	return versionName
}
