package ec2macosinit

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// UserDataModule contains contains all necessary configuration fields for running a User Data module.
type UserDataModule struct {
	// ExecuteUserData must be set to `true` for the userdata script contents to
	// be executed.
	ExecuteUserData bool `toml:"ExecuteUserData"`
}

// Do fetches userdata and writes it to a file in the instance history. The
// written script is then executed when ExecuteUserData is true.
func (m *UserDataModule) Do(mctx *ModuleContext) (message string, err error) {
	const scriptFileName = "userdata"
	userdataScript := filepath.Join(mctx.InstanceHistoryPath(), scriptFileName)

	// Get user data from IMDS
	ud, respCode, err := mctx.IMDS.getIMDSProperty("user-data")
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error getting user data from IMDS: %s\n", err)
	}
	if respCode == 404 { // 404 = no user data provided, exit nicely
		return "no user data provided through IMDS", nil
	}
	if respCode != 200 { // 200 = ok
		return "", fmt.Errorf("ec2macosinit: received an unexpected response code from IMDS: %d - %s\n", respCode, err)
	}

	err = writeShellScript(userdataScript, userdataReader(ud))
	if err != nil {
		return "", fmt.Errorf("userdata script: %w", err)
	}

	// If we don't want to execute the user data, exit nicely - we're done
	if !m.ExecuteUserData {
		return "successfully handled user data with no execution request", nil
	}

	// Execute user data script
	out, err := executeCommand([]string{userdataScript}, "", []string{})
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

// userdataReader provides a decoded reader for the provided userdata text.
// Userdata text may be encoded either as plain text or as base64 encoded plain
// text, so we detect and prepare a reader depending on what's given.
func userdataReader(text string) io.Reader {
	// Attempt to base64 decode userdata.
	//
	// This maintains consistency alongside Amazon Linux 2's cloud-init, which states:
	//
	//     "Some tools and users will base64 encode their data before handing it to
	//      an API like boto, which will base64 encode it again, so we try to decode."
	//
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err == nil {
		return bytes.NewBuffer(decoded)
	} else {
		return bytes.NewBufferString(text)
	}
}

// writeShellScript writes an executable file to the provided path.
func writeShellScript(path string, rd io.Reader) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, rd)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("write contents: %w", err)
	}

	return f.Close()
}
