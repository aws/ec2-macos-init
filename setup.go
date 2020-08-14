package main

import (
	"fmt"
	"math"
	"time"

	"github.com/aws/ec2-macos-init/lib/ec2macosinit"
)

const (
	attemptInterval  = 1    // every 1s
	logInterval      = 10.0 // every 10s
	setupMaxAttempts = 600  // fail after 10m
)

// SetupInstanceID is used to setup the instance ID (and IMDSv2 token) the first time.  It retries at a fixed interval
// up to the maximum number of attempts.  This is expected to fail many times on first boot when this runs before
// networking is fully up.
func SetupInstanceID(c *ec2macosinit.InitConfig) (err error) {
	var attempt int
	// While instance ID is empty
	for c.IMDS.InstanceID == "" {
		// Attempt to get the instance ID
		err = c.IMDS.UpdateInstanceID()
		if err != nil {
			// Fail out if attempts exceeds maximum
			if attempt > setupMaxAttempts {
				return fmt.Errorf("error getting instance ID from IMDS: %s\n", err)
			}

			// Log according to the log interval
			if math.Mod(float64(attempt), logInterval) == 0.0 {
				c.Log.Warnf("Unable to get instance ID - IMDS may not be available yet...retrying every %ds [%d/%d]", attemptInterval, attempt, setupMaxAttempts)
			}

			attempt++ // increment attempts

			// Sleep for attempt interval
			time.Sleep(attemptInterval * time.Second)
		}
	}

	return nil
}
