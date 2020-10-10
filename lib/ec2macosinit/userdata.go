package ec2macosinit

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
)

// UserDataModule contains contains all necessary configuration fields for running a User Data module.
type UserDataModule struct {
	ExecuteUserData bool `toml:"ExecuteUserData"`
}

const (
	baseDir  = "/usr/local/aws/ec2-macos-init/instances"
	fileName = "userdata"
)

// Do for UserDataModule gets the userdata from IMDS and writes it to a file in the instance history. If configured,
// it runs the user data as an executable.
func (c *UserDataModule) Do(ctx *ModuleContext) (message string, err error) {
	// Get user data from IMDS
	ud, respCode, err := ctx.IMDS.getIMDSProperty("user-data")
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error getting user data from IMDS: %s\n", err)
	}
	if respCode == 404 { // 404 = no user data provided, exit nicely
		return "no user data provided through IMDS", nil
	}
	if respCode != 200 { // 200 = ok
		return "", fmt.Errorf("ec2macosinit: received an unexpected response code from IMDS: %d - %s\n", respCode, err)
	}

	// Write user data to file
	userDataFile := path.Join(baseDir, ctx.IMDS.InstanceID, fileName)
	f, err := os.OpenFile(userDataFile, os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error while opening user data file: %s\n", err)
	}
	defer f.Close()
	if _, err := f.WriteString(ud); err != nil {
		return "", fmt.Errorf("ec2macosinit: error while writing to user data file: %s\n", err)
	}

	// If we don't want to execute the user data, exit nicely - we're done
	if !c.ExecuteUserData {
		return "successfully handled user data with no execution request", nil
	}

	// Execute user data script
	out, err := executeCommand([]string{userDataFile}, "", []string{})
	if err != nil {
		if strings.Contains(err.Error(), "exec format error") {
			contentType := http.DetectContentType([]byte(ud))
			return fmt.Sprintf("provided user data is not executable (detected type: %s)", contentType), nil
		} else {
			return fmt.Sprintf("error while running user data with stdout: [%s] and stderr: [%s]", out.stdout, out.stderr), err
		}
	}

	return fmt.Sprintf("successfully ran user data with stdout: [%s] and stderr: [%s]", out.stdout, out.stderr), nil
}
