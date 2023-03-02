package ec2macosinit

import (
	"encoding/json"
	"fmt"
	"os"
)

// FatalCount contains a Count for tracking the number of Fatal exits for this boot
type FatalCount struct {
	Count int `json:"count"`
}

// fatalCountFile is the file that contains the fatal counter, this is cleared on reboot
const fatalCountFile = "/tmp/.ec2-macos-init-fatal-counts.json"

// readFatalCount reads the file contents into FatalCount or returns an initialized counter.
func (r *FatalCount) readFatalCount() (err error) {
	// Check if fatal count file exists, if not, create it but leave it empty, then return 0, otherwise read and return
	_, err = os.Stat(fatalCountFile)
	if !os.IsNotExist(err) {
		err = r.readFatalFile()
		if err != nil {
			return fmt.Errorf("ec2macosinit: Failed to read %s: %s", fatalCountFile, err)
		}
	} else {
		// Take initial values for first run
		*r = FatalCount{1}
	}

	return nil
}

// IncrementFatalCount takes the current count, increments it, and saves to the temporary file.
func (r *FatalCount) IncrementFatalCount() (err error) {
	// Get the current count
	err = r.readFatalCount()
	if err != nil {
		return fmt.Errorf("ec2macosinit: unable to read run count file: %s", err)
	}

	r.Count++ // Increment the counter in the struct

	// Marshall the FatalCount struct to json
	rcBytes, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("ec2macosinit: failed to save run counts: %s", err)
	}

	// Write the bytes to the counter file
	err = os.WriteFile(fatalCountFile, rcBytes, 0644)
	if err != nil {
		return fmt.Errorf("ec2macosinit: failed to save run counts: %s", err)
	}

	return nil
}

// readFatalFile reads the temporary file for count.
func (r *FatalCount) readFatalFile() (err error) {
	// Read the contents into bytes
	countsBytes, err := os.ReadFile(fatalCountFile)
	if err != nil {
		return fmt.Errorf("ec2macosinit: Failed to read %s: %s", fatalCountFile, err)
	}

	// Unmarshal to the struct
	err = json.Unmarshal(countsBytes, &r)
	if err != nil {
		return fmt.Errorf("ec2macosinit: Failed to parse json: %s", err)
	}

	return nil
}
