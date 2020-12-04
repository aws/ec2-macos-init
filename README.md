# EC2 macOS Init

## Overview
**EC2 macOS Init** is the launch daemon used to initialize Mac instances within EC2. It runs many tasks quickly and 
in parallel through the use of Priority Groups. Priority Groups are logical groupings of tasks which can be run 
at the same time without impacting each other. EC2 macOS Init will wait for all modules in a priority group to 
complete before moving on to the next group.

Important files for EC2 macOS Init are located in the following locations:

* `/usr/local/aws/ec2-macos-init/init.toml` - The configuration file used when EC2 macOS Init is run.
* `/usr/local/aws/ec2-macos-init/instances/<instance-id>/` - The location of all instance history (previous runs).
* `/usr/local/bin/ec2-macos-init` - The EC2 macOS Init binary file.
* `/Library/LaunchDaemons/com.amazon.ec2.macos-init.plist` - The `launchd` plist file used to trigger EC2 macOS Init to 
run on boot.

## Usage
Most of the time, no interaction with EC2 macOS Init will be needed. It is automatically run on every boot by `launchd` 
using the included `com.amazon.ec2.macos-init.plist` file. However, it can also be used interactively with the 
following options:

### Run
```
sudo ec2-macos-init run
```

The `run` flag runs EC2 macOS Init using the current configuration located at `/usr/local/aws/ec2-macos-init/init.toml`. 
If EC2 macOS Init has been previously run on the current instance, the instance history will be read and the current 
run will be treated as a second boot (things may be skipped depending on their run type).

### Clean
```
sudo ec2-macos-init clean (-all)
```

The `clean` flag removes instance history located in the `/usr/local/aws/ec2-macos-init/instances/` directory. With no 
arguments, it will only remove any history matching the current instance ID. If provided `-all`, it will remove all 
instance history. This easily allows EC2 macOS Init to be re-run as though it were the first boot, something which is 
recommended as a part of the process to generate a custom AMI from a currently running instance resulting in a 
clean history for the new AMI.

### Version
```
sudo ec2-macos-init version
```

The `version` flag returns the current version of EC2 macOS init as well as the date of the commit used to build the 
executable.

## Init.toml Configuration Options
EC2 macOS Init uses a single [TOML](https://toml.io/) file to configure boot options. These are divided into modules 
which can be added to any launch group and run in any order. Current modules and options include:

### Common Options
The following options are available for all modules:

* `Name` (`string`) - Required; This is a unique string used to identify the module both in logging and instance history.
* `PriorityGroup` (`int`) - Required; An integer defining the priority group. Modules with the same Priority Group 
number will run in parallel. 
* `FatalOnError` (`bool`) - Optional; Fatal on error will halt the run at the current group and not continue to later 
Priority Groups. Defaults to `false`.

Additionally, all module configurations must contain exactly one of the following, set to `true`:

* `RunOnce` (`bool`) - Required; Run this module only once, ever. Any history of a module with this set will prevent it 
from running again. Defaults to `false`.
* `RunPerBoot` (`bool`) - Required; Run this module on every boot. Defaults to `false`.
* `RunPerInstance` (`bool`) - Required; Run this module once per instance ID. Defaults to `false`.

### Command
The `Command` module runs a single command. This can be used for a wide variety of tasks on launch. It should be noted 
that any shell redirection will not work as anticipated as this is intended only for simple commands. In more complex 
cases, it's suggested to use this module to execute a shell script containing the required commands.

* `Cmd` (`string array`) - Required; This is the command to be run. The first element should be the name of the 
executable and all following elements are arguments.
* `RunAsUser` (`string`) - Optional; The user the command should be run as. Default is `root`.
* `EnvironmentVars` (`[]string`) - Optional; A slice of environment variables in the form `key=value`. Default is 
empty.
	
#### Example
```toml
[[Module]]
  Name = "Important-Init-Command"
  PriorityGroup = 4 # Fourth group
  RunOnce = true # Run once, ever
  FatalOnError = true # Stop running Init if there is an error 
  [Module.Command]
    Cmd = ["touch", "/tmp/file.txt"] # A simple command
    RunAsUser = "ec2-user" # Run as ec2-user
    EnvironmentVars = ["MY_KEY=myValue"] # One environment variable named MY_KEY
```

### Network Check
The `NetworkCheck` module gets the default gateway and pings it to check if the network is up. This is useful as a 
way to gate subsequent modules which require network access (internet or IMDS).

* `PingCount` (`int`) - Optional; The number of ping attempts to try against the default gateway. Default is `3`.

#### Example
```toml
[[Module]]
  Name = "Network-Check"
  PriorityGroup = 1 # First group
  RunPerBoot = true # Run every boot
  FatalOnError = true # Fatal if there's an error - this must succeed
  [Module.NetworkCheck]
    PingCount = 6 # Six attempts
```

### SSH Keys
The `SSHKeys` module manages the `.ssh/authorized_keys` file on boot.  There are many options here, but it is primarily 
used to pull OpenSSH keys from IMDS on first launch.

* `DedupKeys` (`bool`) - Optional; Enable deduplication of keys. This option will cause the entire `authorized_keys` 
file for the user (default is `ec2-user`) to be read and all keys will be deduplicated. This is useful in preventing 
the user's keys file from having many of the same key after multiple launches. Default is `false`.
* `GetIMDSOpenSSHKey` (`bool`) - Optional; Get the OpenSSH key from IMDS, if provided. On launch of an EC2 instance, 
users are offered the option to provide an EC2 Key Pair. This option will add that OpenSSH key to `authorized_keys`. 
Default is `false`.
* `StaticOpenSSHKeys` (`[]string`) - Optional; This option takes a string array of keys in SSH RSA public key 
format (`ssh-rsa <material> <comment>`) and adds them to `authorized_keys`. Default is empty.
* `OverwriteAuthorizedKeys` (`bool`) - Optional; Overwrite the `authorized_keys` file each time this module runs. 
This can be useful in ensuring that old keys are removed every launch and replaced by new ones through either of the 
IMDS or static key options. Default is `false`.
* `User` (`string`) - Optional; The owner of the `authorized_keys` file. Default is `ec2-user`.

#### Example
```toml
[[Module]]
  Name = "Get-SSH-Keys"
  PriorityGroup = 3 # Third group
  FatalOnError = true # Exit on failure - this is required to log in
  RunPerInstance = true # Run only once per instance
  [Module.SSHKeys]
    GetIMDSOpenSSHKey = true # Get the key from IMDS
    User = "ec2-user" # Apply the key to ec2-user
    DedupKeys = true # Remove duplicate keys
    OverwriteAuthorizedKeys = false # Append to authorized_keys to avoid erasing any additional keys on future instances
```

### Userdata
The `UserData` module pulls User Data from IMDS and provides the option to execute it. This is stored in a file at 
`/usr/local/aws/ec2-macos-init/instances/<instance-id>/userdata`. This can be useful for non-executables (like JSON) 
 as well, by pulling the data from IMDS and making it immediately available without having to retrieve it directly.

* `ExecuteUserData` (`bool`) - Optional; If set to `true`, Init will treat the userdata file as an executable and 
attempt to run it. Default is `false`.

#### Example
```toml
[[Module]]
  Name = "Execute-User-Data"
  PriorityGroup = 4 # Fourth group
  RunPerInstance = true # Run once per instance
  FatalOnError = false # Best effort, don't fatal on error
  [Module.UserData]
    ExecuteUserData = true # Execute the userdata
```

### System Configuration
The `SystemConfig` module provides a few interfaces for setting system configuration parameters, primarily through 
the use of `sysctl` and `defaults`.

* `[Module.SystemConfig.Sysctl]` - Optional; Contains the value to be set by `sysctl`.
    * `value` (`string`) - Required; The value in the form: `"parameter=value"`.
* `[Module.SystemConfig.Defaults]` - Optional; Contains a parameter and value to be set by `defaults`.
    * `plist` (`string`) - Required; The plist to containing the parameter to be set.
    * `parameter` (`string`) - Required; The parameter to be updated.
    * `type` (`string`) - Required; The type of parameter to be set. Currently, this can only be `"bool"`.
    * `value` (`string`) - Required; The value to assign to the plist parameter.
* `secureSSHDConfig` (`bool`) - Optional; Reapply the default SSHD config security settings after an OS update.

#### Example
```toml
[[Module]]
  Name = "System-Configuration"
  PriorityGroup = 2 # Second group
  RunPerBoot = true # Run every boot to enforce these parameters
  FatalOnError = false # Best effort, don't fatal on error
  [Module.SystemConfig]
    secureSSHDConfig = true # secure sshd_config on OS update
    [[Module.SystemConfig.Sysctl]]
      value = "my.favorite.parameter=42" # use sysctl to set my.favorite.parameter
    [[Module.SystemConfig.Defaults]]
      plist = "/Library/Preferences/com.amazon.ec2.plist" # use defaults to set a parameter in this plist
      parameter = "PlistParameter"
      type = "bool"
      value = "false"
```

## Building

The `build.sh` script has been provided for easy builds.  This script sets build-time variables, gets dependencies, 
and then builds the binary for `darwin/amd64`.  Once complete, the binary, launchd plist, and `init.toml` configuration 
file need to be copied to the locations described in the Overview section of this README before testing.

## Contributing

Please feel free to submit issues, fork the repository and send pull requests! 
See [CONTRIBUTING](CONTRIBUTING.md) for more information.

## Security

See the Security section of [CONTRIBUTING](CONTRIBUTING.md#security-issue-notifications) for more information.

## License

This project is licensed under the Apache-2.0 License.