package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path"

	"github.com/aws/ec2-macos-init/lib/ec2macosinit"
)

// clean removes old instance history. It has two options:
// current - This is the option when -all isn't provided. It only removes the current instance's history.
// all - When -all is provided, all instance history is removed.
func clean(c *ec2macosinit.InitConfig) {
	// Define flags
	cleanFlags := flag.NewFlagSet("clean", flag.ExitOnError)
	cleanAll := cleanFlags.Bool("all", false, "Optional; Remove all instance history.  Default is false.")

	// Parse flags
	err := cleanFlags.Parse(os.Args[2:])
	if err != nil {
		c.Log.Fatalf(64, "Unable to parse arguments: %s", err)
	}

	// Clean all or clean the current instance
	historyPath := path.Join(baseDir, instanceHistoryDir)
	if *cleanAll {
		c.Log.Info("Removing all instance history")
		// Read instance history directory
		dir, err := ioutil.ReadDir(historyPath)
		if err != nil {
			c.Log.Fatalf(66, "Unable to read instance history located at %s: %s", historyPath, err)
		}
		for _, d := range dir {
			// Remove everything
			err := os.RemoveAll(path.Join([]string{historyPath, d.Name()}...))
			if err != nil {
				c.Log.Fatalf(1, "Unable to remove instance history: %s", err)
			}
		}
	} else {
		c.Log.Infof("Getting current instance ID from IMDS")
		// Instance ID is needed, run setup
		err = SetupInstanceID(c)
		if err != nil {
			c.Log.Fatalf(75, "Unable to get instance ID: %s", err)
		}
		c.Log.Infof("Removing history for the current instance [%s]", c.IMDS.InstanceID)

		// Remove current instance history
		err := os.RemoveAll(path.Join(historyPath, c.IMDS.InstanceID))
		if err != nil {
			c.Log.Fatalf(1, "Unable to remove instance history: %s", err)
		}
	}
	c.Log.Info("Clean complete")
}
