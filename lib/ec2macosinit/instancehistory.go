package ec2macosinit

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"
)

// This is unused for now but will allow us to modify the version of this history in the future.
const historyVersion = 1

// History contains an instance ID, run time and a slice of individual module histories.
type History struct {
	InstanceID      string          `json:"instanceID"`
	RunTime         time.Time       `json:"runTime"`
	ModuleHistories []ModuleHistory `json:"moduleHistory"`
	Version         int             `json:"version"`
}

// ModuleHistory contains a key of the configuration struct for future comparison and whether that run was successful.
type ModuleHistory struct {
	Key     string `json:"key"`
	Success bool   `json:"success"`
}

// GetInstanceHistory takes a path to instance history directory and a file name for history files and searches for
// any files that match. Then, for each file, it calls readHistoryFile() to read the file and add it to the
// InstanceHistory struct.
func (c *InitConfig) GetInstanceHistory() (err error) {
	// Read instance history directory
	dirs, err := ioutil.ReadDir(c.HistoryPath)
	if err != nil {
		return fmt.Errorf("ec2macosinit: unable to read instance history directory: %s\n", err)
	}
	// For each directory, check for a history file and call readHistoryFile()
	for _, dir := range dirs {
		if dir.IsDir() {
			historyFile := path.Join(c.HistoryPath, dir.Name(), c.HistoryFilename)
			if _, err := os.Stat(historyFile); err == nil {
				history, err := readHistoryFile(historyFile)
				if err != nil {
					return fmt.Errorf("ec2macosinit: error while reading history file at %s: %s\n", historyFile, err)
				}
				// Append the returned History struct to the InstanceHistory slice
				c.InstanceHistory = append(c.InstanceHistory, history)
			}
		}
	}

	return nil
}

// readHistoryFile takes an instance history file and returns a History struct containing the same information.
func readHistoryFile(file string) (history History, err error) {
	// Read file
	historyBytes, err := ioutil.ReadFile(file)
	if err != nil {
		return History{}, fmt.Errorf("ec2macosinit: error reading config file located at %s: %s\n", file, err)
	}

	// Unmarshal to struct
	err = json.Unmarshal(historyBytes, &history)
	if err != nil {
		return History{}, fmt.Errorf("ec2macosinit: error unmarshaling history from JSON: %s\n", err)
	}

	return history, nil
}

// WriteHistoryFile takes ModulesByPriority and writes it to a given history path and filename as JSON.
func (c *InitConfig) WriteHistoryFile() (err error) {
	history := History{
		InstanceID: c.IMDS.InstanceID,
		RunTime:    time.Now(),
		Version:    historyVersion,
	}
	// Copy relevant fields from InitConfig to History struct
	for _, p := range c.ModulesByPriority {
		for _, m := range p {
			history.ModuleHistories = append(
				history.ModuleHistories,
				ModuleHistory{
					Key:     m.generateHistoryKey(),
					Success: m.Success,
				},
			)
		}
	}

	// Marshal to JSON
	historyBytes, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("ec2macosinit: unable to write history file: %s\n", err)
	}

	// Write history JSON file
	err = ioutil.WriteFile(path.Join(c.HistoryPath, c.IMDS.InstanceID, c.HistoryFilename), historyBytes, 0644)
	if err != nil {
		return fmt.Errorf("ec2macosinit: unable to write history file: %s\n", err)
	}

	return nil
}

// CreateDirectories creates the instance directory, if it doesn't exist and a directory for the running instance.
func (c *InitConfig) CreateDirectories() (err error) {
	if _, err := os.Stat(path.Join(c.HistoryPath, c.IMDS.InstanceID)); os.IsNotExist(err) {
		err := os.MkdirAll(path.Join(c.HistoryPath, c.IMDS.InstanceID), 0755)
		if err != nil {
			return fmt.Errorf("ec2macosinit: unable to create directory: %s\n", err)
		}
	}
	return nil
}
