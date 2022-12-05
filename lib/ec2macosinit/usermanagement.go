package ec2macosinit

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// PasswordLength is the default number of characters that the auto-generated password should be
	PasswordLength = 25
	// DsclPath is the default path for the dscl utility needed for the functions in this file
	DsclPath = "/usr/bin/dscl"
)

// UserManagementModule contains the necessary values to run a User Management Module
type UserManagementModule struct {
	RandomizePassword bool   `toml:"RandomizePassword"`
	User              string `toml:"User"`
}

// Do for the UserManagementModule is the primary entry point for the User Management Module.
func (c *UserManagementModule) Do(ctx *ModuleContext) (message string, err error) {
	// Check if randomizing password is requested. If so, then perform action, otherwise return with no work to do
	if c.RandomizePassword {
		message, err = c.randomizePassword()
		if err != nil {
			return "", fmt.Errorf("ec2macosinit: failed to randomize password: %s", err)
		}
	} else {
		return fmt.Sprint("randomizing password disabled, skipping"), nil
	}

	// For now, `message` will only be set if RandomizePassword is true. Instead of returning above, it is returned here
	// for readability and future additions to the module
	return message, nil
}

// isSecureTokenSet wraps the sysadminctl call to provide a bool for checking if its enabled
// The way to detect if the Secure Token is set for a user is `sysadminctl`, here is an example for ec2-user:
//     /usr/sbin/sysadminctl -secureTokenStatus ec2-user
//     2021-01-14 18:17:47.414 sysadminctl[96836:904874] Secure token is DISABLED for user ec2-user
// When enabled it shows:
//     2021-01-14 19:21:55.854 sysadminctl[14193:181530] Secure token is ENABLED for user ec2-user
func (c *UserManagementModule) isSecureTokenSet() (enabled bool, err error) {
	// Fetch the text from the built-in tool sysadminctl
	statusText, err := executeCommand([]string{"/usr/sbin/sysadminctl", "-secureTokenStatus", c.User}, "", []string{})
	if err != nil {
		return false, fmt.Errorf("ec2macosinit: unable to get Secure Token status for %s: %s", c.User, err)
	}
	// If the text has "ENABLED" then return true, otherwise return false
	if strings.Contains(statusText.stdout, "Secure token is ENABLED") {
		return true, nil
	}
	return false, nil
}

// disableSecureTokenCreation disables the default behavior to enable the Secure Token on the next user password change.
// From https://support.apple.com/guide/deployment-reference-macos/using-secure-and-bootstrap-tokens-apdff2cf769b/web
// This is the command used to avoid setting the SecureToken when changing the password
//     /usr/bin/dscl . append /Users/ec2-user AuthenticationAuthority ";DisabledTags;SecureToken"
func (c *UserManagementModule) disableSecureTokenCreation() (err error) {
	_, err = executeCommand([]string{DsclPath, ".", "append", filepath.Join("Users", c.User), "AuthenticationAuthority", ";DisabledTags;SecureToken"}, "", []string{})
	if err != nil {
		return fmt.Errorf("ec2macosinit: failed disable Secure Token creation: %s", err)
	}
	return nil
}

// enableSecureTokenCreation enables the default behavior to enable the Secure Token on the next user password change.
// From https://support.apple.com/guide/deployment-reference-macos/using-secure-and-bootstrap-tokens-apdff2cf769b/web
// This is the command used to remove the setting for the SecureToken when changing the password
//     /usr/bin/dscl . delete /Users/ec2-user AuthenticationAuthority ";DisabledTags;SecureToken"
func (c *UserManagementModule) enableSecureTokenCreation() (err error) {
	_, err = executeCommand([]string{DsclPath, ".", "delete", filepath.Join("Users", c.User), "AuthenticationAuthority", ";DisabledTags;SecureToken"}, "", []string{})
	if err != nil {
		return fmt.Errorf("ec2macosinit: failed to disable Secure Token creation: %s", err)
	}
	return nil
}

// changePassword changes the password to a provided string.
func (c *UserManagementModule) changePassword(password string) (err error) {
	_, err = executeCommand([]string{DsclPath, ".", "-passwd", filepath.Join("Users", c.User), password}, "", []string{})
	if err != nil {
		return fmt.Errorf("ec2macosinit: failed to set %s's password: %s", c.User, err)
	}
	return nil
}

// randomizePassword confirms if the Secure Token is set and randomizes the user password.
// The password change functionality, at its core, is simply detecting if the user password can be randomized for
// the default "ec2-user" user. The complexity comes in when dealing with the Secure Token. From Big Sur onward, the
// Secure Token is set on all initial password changes, this is not ideal since future password changes would require
// knowing this random password. This process is built to avoid the Secure Token being set on this first randomization.
// The basic flow is:
//   1. Check for the Secure Token already being set which would prevent changing the password
//   2. Add a special property to avoid the Secure Token from being set
//   3. Change the password to a random string
//   4. Undo the special property so that the next password change will set the Secure Token
func (c *UserManagementModule) randomizePassword() (message string, err error) {
	// This detection of the user probably needs to move into the Do() function when there is more to do, but since this
	// is the first place the c.User is used, its handled here
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

	// Check for Secure Token, if its already set then attempting to change the password will fail
	secureTokenSet, err := c.isSecureTokenSet()
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to confirm Secure Token is DISABLED: %s", err)
	}

	// Only proceed if user doesn't have Secure Token enabled
	if secureTokenSet {
		return "", fmt.Errorf("ec2macosinit: unable to change password, Secure Token Set for %s", c.User)
	}

	// Change Secure Token behavior if needed
	err = c.disableSecureTokenCreation()
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to disable Secure Token generation: %s", err)
	}
	defer func() {
		// Set Secure Token behavior back if needed
		deferErr := c.enableSecureTokenCreation()
		if deferErr != nil {
			// Catch a failure and change status returns to represent an error condition
			message = "" // Overwrite new message to indicate error
			err = fmt.Errorf("ec2macosinit: unable to enable Secure Token generation: %s %s", deferErr, err)
		}
	}()

	// Generate random password
	password, err := generateSecurePassword(PasswordLength)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to generate secure password: %s", err)
	}

	// Change the password
	err = c.changePassword(password)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to set secure password: %s", err)
	}

	return fmt.Sprintf("successfully set secure password for %s", c.User), nil
}

// generateRandomBytes returns securely generated random bytes for use in generating a password
// It will return an error if the system's secure random number generator fails to function correctly
func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, fmt.Errorf("ec2macosinit: unable to read random bytes from OS: %s", err)
	}

	return b, nil
}

// generateSecurePassword generates a password securely for use in randomizePassword using the crypto/rand library
func generateSecurePassword(length int) (password string, err error) {
	// Fetch the requested number of bytes, this ensures at least that much entropy
	randomBytes, err := generateRandomBytes(length)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: unable to generate secure password %s", err)
	}
	// URLEncode it to have safe characters for passwords
	source := base64.URLEncoding.EncodeToString(randomBytes)

	// Return only the length requested since URL Encoding can result in longer strings
	return source[0:length], nil
}
