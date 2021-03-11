package ec2macosinit

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ioReadCloserToString converts an io.ReadCloser to a string.
func ioReadCloserToString(iorc io.ReadCloser) (str string, err error) {
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(iorc)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// commandOutput wraps the output from an exec command as strings.
type commandOutput struct {
	stdout string
	stderr string
}

// executeCommand executes the command and returns stdout and stderr as strings.
func executeCommand(c []string, runAsUser string, envVars []string) (output commandOutput, err error) {
	// Separate name and args, plus catch a few error cases
	var name string
	var args []string
	if len(c) > 1 {
		name = c[0]
		args = c[1:]
	} else if len(c) == 1 {
		name = c[0]
		if name == "" { // Empty string case ("")
			return commandOutput{}, fmt.Errorf("ec2macosinit: must provide a command")
		}
	} else { // Empty struct case ([]string{})
		return commandOutput{}, fmt.Errorf("ec2macosinit: must provide a command")
	}

	// Set command and create output buffers
	cmd := exec.Command(name, args...)
	var stdoutb, stderrb bytes.Buffer
	cmd.Stdout = &stdoutb
	cmd.Stderr = &stderrb

	// Set runAsUser, if defined, otherwise will run as root
	if runAsUser != "" {
		uid, gid, err := getUIDandGID(runAsUser)
		if err != nil {
			return commandOutput{}, fmt.Errorf("ec2macosinit: error looking up user: %s\n", err)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

	// Append environment variables
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, envVars...)

	// Run command
	err = cmd.Run()
	if err != nil {
		return commandOutput{}, err
	}

	return commandOutput{stdout: stdoutb.String(), stderr: stderrb.String()}, nil
}

// getUIDandGID takes a username and returns the uid and gid for that user.
// While testing UID/GID lookup for a user, it was found that the user.Lookup() function does not always return
// information for a new user on first boot. In the case that user.Lookup() fails, we try dscacheutil, which has a
// higher success rate. If that fails, we return an error. Any successful case returns the UID and GID as ints.
func getUIDandGID(username string) (uid int, gid int, err error) {
	var uidstr, gidstr string
	// Preference is user.Lookup(), if it works
	u, lookuperr := user.Lookup(username)
	if lookuperr != nil {
		// user.Lookup() has failed, second try by checking the DS cache
		out, cmderr := executeCommand([]string{"dscacheutil", "-q", "user", "-a", "name", username}, "", []string{})
		if cmderr != nil {
			// dscacheutil has failed with an error
			return 0, 0, fmt.Errorf("ec2macosinit: error while looking up user %s: \n"+
				"user.Lookup() error: %s \ndscacheutil error: %s\ndscacheutil stderr: %s\n",
				username, lookuperr, cmderr, out.stderr)
		}
		// Check length of stdout - dscacheutil returns nothing if user is not found
		if len(out.stdout) > 0 { // dscacheutil has returned something
			// Command output from dscacheutil should look like:
			//   name: ec2-user
			//   password: ********
			//   uid: 501
			//   gid: 20
			//   dir: /Users/ec2-user
			//   shell: /bin/bash
			//   gecos: ec2-user
			dsSplit := strings.Split(out.stdout, "\n") // split on newline to separate uid and gid
			for _, e := range dsSplit {
				eSplit := strings.Fields(e) // split into fields to separate tag with id
				// Find UID and GID and set them
				if strings.HasPrefix(e, "uid") {
					if len(eSplit) != 2 {
						// dscacheutil has returned some sort of weird output that can't be split
						return 0, 0, fmt.Errorf("ec2macosinit: error while splitting dscacheutil uid output for user %s: %s\n"+
							"user.Lookup() error: %s \ndscacheutil error: %s\ndscacheutil stderr: %s\n",
							username, out.stdout, lookuperr, cmderr, out.stderr)
					}
					uidstr = eSplit[1]
				} else if strings.HasPrefix(e, "gid") {
					if len(eSplit) != 2 {
						// dscacheutil has returned some sort of weird output that can't be split
						return 0, 0, fmt.Errorf("ec2macosinit: error while splitting dscacheutil gid output for user %s: %s\n"+
							"user.Lookup() error: %s \ndscacheutil error: %s\ndscacheutil stderr: %s\n",
							username, out.stdout, lookuperr, cmderr, out.stderr)
					}
					gidstr = eSplit[1]
				}
			}
		} else {
			// dscacheutil has returned nothing, user is not found
			return 0, 0, fmt.Errorf("ec2macosinit: user %s not found: \n"+
				"user.Lookup() error: %s \ndscacheutil error: %s\ndscacheutil stderr: %s\n",
				username, lookuperr, cmderr, out.stderr)
		}
	} else {
		// user.Lookup() was successful, use the returned UID/GID
		uidstr = u.Uid
		gidstr = u.Gid
	}

	// Convert UID and GID to int
	uid, err = strconv.Atoi(uidstr)
	if err != nil {
		return 0, 0, fmt.Errorf("ec2macosinit: error while converting UID to int: %s\n", err)
	}
	gid, err = strconv.Atoi(gidstr)
	if err != nil {
		return 0, 0, fmt.Errorf("ec2macosinit: error while converting GID to int: %s\n", err)
	}

	return uid, gid, nil
}

// userExists takes a username and returns whether or not the user exists on the system.
func userExists(username string) (exists bool, err error) {
	out, err := executeCommand([]string{"dscacheutil", "-q", "user", "-a", "name", username}, "", []string{})
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: error while checking dscacheutil for user %s: %s\n", username, err)
	}
	// If dscacheutil output containing the username, the user exists
	if strings.Contains(out.stdout, username) {
		return true, nil
	}

	// No output means the user does not exist
	return false, nil
}

// retry is an extremely simple retry function which waits a specified duration on error and retries.
func retry(attempts int, sleep time.Duration, f func() error) (err error) {
	for i := 0; ; i++ {
		err = f()
		if err == nil {
			return
		}
		if i >= (attempts - 1) {
			break
		}

		time.Sleep(sleep)
	}
	return fmt.Errorf("after %d attempts, last error: %s", attempts, err)
}

// getOSProductVersion uses the sysctl command to retrieve the product version number from the kernel
func getOSProductVersion() (version string, err error) {
	cmdGetProductVersion := []string{"sysctl", "-n", "kern.osproductversion"}

	output, err := executeCommand(cmdGetProductVersion, "", []string{})
	if err != nil {
		return version, fmt.Errorf("ec2macosinit: error getting kernel state for product version: %s", err)
	}

	// Remove any extra space characters from the output to leave only the product version number
	version = strings.TrimSpace(output.stdout)

	return version, nil
}

// decodeBase64 attempts to decode base64 data and returns the decoded string if successful
func decodeBase64(base64Data string) (decodedString string, err error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: failed to decode base64 string: %s\n", err)
	}

	return string(decodedBytes), nil
}
