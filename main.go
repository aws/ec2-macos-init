package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/aws/ec2-macos-init/internal/paths"
	"github.com/aws/ec2-macos-init/lib/ec2macosinit"
)

const (
	loggingTag = "ec2-macOS-init"
)

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
	if !runningAsRoot() {
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

// printUsage prints the help text for this program.
func printUsage(baseDir string) {
	fmt.Println("Usage: ec2-macos-init <command> <arguments>")
	fmt.Println("Commands are:")
	fmt.Println("    run - Run init using configuration located in " + filepath.Join(baseDir, paths.InitTOML))
	fmt.Println("    clean - Remove instance history from disk")
	fmt.Println("    version - Print version information")
	fmt.Println("For more help: ec2-macos-init <command> -h")
}

// runningAsRoot checks to see if the init application is being run as
// root.
func runningAsRoot() bool {
	// must effectively be root
	return os.Geteuid() == 0
}
