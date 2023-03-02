package main

import "fmt"

var (
	// CommitDate is the date of the commit used at build time.
	CommitDate string
	// Version is the ec2-macos-init release version for this build.
	Version string = "0.0.0-dev"
)

// printVersion prints the output for the version command.
func printVersion() {
	const gitHubLink = "https://github.com/aws/ec2-macos-init"

	fmt.Printf("\nEC2 macOS Init\n"+
		"Version: %s [%s]\n"+
		"%s\n"+
		"Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.\n\n",
		Version, CommitDate, gitHubLink,
	)
}
