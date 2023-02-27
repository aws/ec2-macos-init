package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/aws/ec2-macos-init/internal/paths"
	"github.com/aws/ec2-macos-init/lib/ec2macosinit"
)

const (
	loggingTag         = "ec2-macOS-init"
	gitHubLink         = "https://github.com/aws/ec2-macos-init"
)

// Build time variables
var CommitDate string
var Version string

// printUsage prints the help text when invalid arguments are provided.
func printUsage(baseDir string) {
	fmt.Println("Usage: ec2-macos-init <command> <arguments>")
	fmt.Println("Commands are:")
	fmt.Println("    run - Run init using configuration located in " + filepath.Join(baseDir, paths.InitTOML))
	fmt.Println("    clean - Remove instance history from disk")
	fmt.Println("    version - Print version information")
	fmt.Println("For more help: ec2-macos-init <command> -h")
}

// printVersion prints the output for the version command.
func printVersion() {
	fmt.Printf("\nEC2 macOS Init\n"+
		"Version: %s [%s]\n"+
		"%s\n"+
		"Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.\n\n",
		Version, CommitDate, gitHubLink,
	)
}

// checkRootPermissions checks to see if the init application is being run as root.
func checkRootPermissions() (root bool, err error) {
	cmd := exec.Command("id", "-u")
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	// Convert for comparison
	i, err := strconv.Atoi(string(output[:len(output)-1]))
	if err != nil {
		return false, err
	}

	// 0 = root
	// 501 = non-root user
	if i == 0 {
		return true, nil
	}

	return false, nil
}

func main() {
	const baseDir = paths.DefaultBaseDirectory

	// Set up logging
	logger, err := ec2macosinit.NewLogger(loggingTag, true, true)
	if err != nil {
		log.Fatalf("Unable to start logging: %s", err)
	}

	// Check runtime OS
	if !(runtime.GOOS == "darwin") {
		logger.Fatal(1, "Can only be run from macOS!")
	}

	// Check that this is being run by a user with root permissions
	root, err := checkRootPermissions()
	if err != nil {
		logger.Fatalf(71, "Error while checking root permissions: %s", err)
	}
	if !root {
		logger.Fatal(64, "Must be run with root permissions!")
	}

	// Check for no command
	if len(os.Args) < 2 {
		logger.Info("Must provide a command!")
		printUsage(baseDir)
		os.Exit(2)
	}

	// Setup InitConfig
	config := &ec2macosinit.InitConfig{
		HistoryPath:     paths.AllInstancesHistory(baseDir),
		HistoryFilename: paths.HistoryJSON,
		Log:             logger,
	}

	// Command switch
	switch command := os.Args[1]; command {
	case "run":
		run(baseDir, config)
	case "clean":
		clean(baseDir, config)
	case "version":
		printVersion()
		os.Exit(0)
	default:
		logger.Errorf("%s is not a valid command", command)
		printUsage(baseDir)
		os.Exit(2)
	}
}
