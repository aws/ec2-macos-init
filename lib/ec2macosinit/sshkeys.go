package ec2macosinit

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"strings"
)

// SSHKeysModule contains all necessary configuration fields for running an SSH Keys module.
type SSHKeysModule struct {
	DedupKeys               bool     `toml:"DedupKeys"`
	GetIMDSOpenSSHKey       bool     `toml:"GetIMDSOpenSSHKey"`
	StaticOpenSSHKeys       []string `toml:"StaticOpenSSHKeys"`
	OverwriteAuthorizedKeys bool     `toml:"OverwriteAuthorizedKeys"`
	User                    string   `toml:"User"`
}

// Do for the SSHKeysModule does some brief validation, gets the IMDS key (if configured), appends static keys (if
// configured), and then writes them to the authorized_keys file for the user.
func (c *SSHKeysModule) Do(ctx *ModuleContext) (message string, err error) {
	// If we're not getting the key from IMDS and there are no keys provided, there's nothing to do here
	if !c.GetIMDSOpenSSHKey && len(c.StaticOpenSSHKeys) == 0 {
		return "nothing to do", nil
	}

	// If user is undefined, default to ec2-user
	if c.User == "" {
		c.User = "ec2-user"
	}

	// Verify that user exists
	exists, err := userExists(c.User)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error while checking if user %s exists: %s\n", c.User, err)
	}
	if !exists { // if the user doesn't exist, error out
		return "", fmt.Errorf("ec2macosinit: user %s does not exist\n", c.User)
	}

	// Set directory and authorized_keys file
	authorizedKeysDir := path.Join("/Users", c.User, ".ssh")
	authorizedKeysFile := path.Join(authorizedKeysDir, "authorized_keys")
	if _, err := os.Stat(authorizedKeysDir); os.IsNotExist(err) { // If directory doesn't exist, create it
		err := os.MkdirAll(authorizedKeysDir, 0700)
		if err != nil {
			return "", fmt.Errorf("ec2macosinit: unable to create directory [%s]: %s\n", authorizedKeysDir, err)
		}
	}

	// Get IMDS key
	keySet := map[string]struct{}{}
	if c.GetIMDSOpenSSHKey {
		// Get IMDS property "meta-data/public-keys/0/openssh-key"
		imdsKey, respCode, err := ctx.IMDS.getIMDSProperty("meta-data/public-keys/0/openssh-key")
		if err != nil {
			return "", fmt.Errorf("ec2macosinit: error getting openSSH key from IMDS: %s\n", err)
		}
		if respCode != 200 && respCode != 404 { // 200 = ok; 404 = no key provided
			return "", fmt.Errorf("ec2macosinit: received an unexpected response code from IMDS: %d - %s\n", respCode, err)
		}
		keySet[strings.TrimSpace(imdsKey)] = struct{}{}
	}

	// Add all unique provided static keys
	if len(c.StaticOpenSSHKeys) > 0 {
		for _, k := range c.StaticOpenSSHKeys {
			keySet[strings.TrimSpace(k)] = struct{}{}
		}
	}

	// If authorized_keys file exists and deduplication is requested, read file and add to set
	if _, err := os.Stat(authorizedKeysFile); err == nil && c.DedupKeys {
		file, err := os.Open(authorizedKeysFile)
		if err != nil {
			return "", fmt.Errorf("ec2macosinit: unable to open %s: %s\n", authorizedKeysFile, err)
		}
		defer file.Close()

		// Read file and add each line to set
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			keySet[strings.TrimSpace(scanner.Text())] = struct{}{}
		}
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("ec2macosinit: error while reading %s: %s\n", authorizedKeysFile, err)
		}

		// Set OverwriteAuthorizedKeys to true so that duplicate keys are overwritten
		c.OverwriteAuthorizedKeys = true
	}

	// Add all keys to a slice
	var keys []string
	for k := range keySet {
		keys = append(keys, k)
	}

	// Write to authorized_keys file
	var f *os.File
	if !c.OverwriteAuthorizedKeys {
		// Append to authorized_keys
		f, err = os.OpenFile(authorizedKeysFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	} else {
		// Overwrite (truncate) authorized_keys
		f, err = os.OpenFile(authorizedKeysFile, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0600)
	}
	if err != nil {
		f.Close()
		return "", fmt.Errorf("ec2macosinit: error while opening authorized_keys file: %s\n", err)
	}
	if _, err := f.WriteString(strings.Join(keys, "\n") + "\n"); err != nil {
		return "", fmt.Errorf("ec2macosinit: error while writing to authorized_keys file: %s\n", err)
	}
	f.Close()

	// Get UID and GID for user
	uid, gid, err := getUIDandGID(c.User)
	if err != nil && c.User == "ec2-user" {
		// Use default values for ec2-user
		uid = 501
		gid = 20
	} else if err != nil {
		return "", fmt.Errorf("ec2macosinit: error while getting user info: %s\n", err)
	}

	// Fix file ownership and directory permissions
	err = os.Chown(authorizedKeysDir, uid, gid)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to change ownership of .ssh directory: %s\n", err)
	}
	err = os.Chown(authorizedKeysFile, uid, gid)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to change ownership of authorized_keys file: %s\n", err)
	}

	return fmt.Sprintf("successfully added %d keys to authorized_users", len(keys)), nil
}
