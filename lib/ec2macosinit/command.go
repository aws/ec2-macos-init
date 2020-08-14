package ec2macosinit

import (
	"fmt"
	"strings"
)

// CommandModule contains contains all necessary configuration fields for running a Command module.
type CommandModule struct {
	Cmd             []string `toml:"Cmd"`
	RunAsUser       string   `toml:"RunAsUser"`
	EnvironmentVars []string `toml:"EnvironmentVars"`
}

// Do for CommandModule runs a command with the values set in the config file.
func (c *CommandModule) Do() (message string, err error) {
	out, err := executeCommand(c.Cmd, c.RunAsUser, c.EnvironmentVars)
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error executing command [%s] with stderr [%s]: %s",
			c.Cmd, strings.TrimSuffix(out.stderr, "\n"), err)
	}
	return fmt.Sprintf("successfully ran command [%s] with stdout [%s] and stderr [%s]",
		c.Cmd, strings.TrimSuffix(out.stdout, "\n"), strings.TrimSuffix(out.stderr, "\n")), nil
}
