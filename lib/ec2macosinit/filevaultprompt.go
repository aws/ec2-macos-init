package ec2macosinit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	fstabFile = "/etc/fstab"
	// bootedMount is the booted System volume. Its container holds every volume that belongs to the booted OS.
	bootedMount = "/"

	// fstabOptions are written for every selected volume: macOS never mounts it at boot, and a manual mount is read-only
	// and hidden from Finder
	fstabOptions = "ro,noauto,nobrowse"
	// fstabMarker precedes every fstab entry written by this module so the source of the line is clear
	fstabMarker = "# Added by ec2-macos-init: do not automount this volume"

	// diskutilTimeout bounds each diskutil and plutil call. This module runs in the first priority group, so a hung
	// disk arbitration call would otherwise hold up every later module, including SSH key setup.
	diskutilTimeout = 30 * time.Second
)

// fstabWrite is the outcome of ensureNoAutoFstabEntry.
type fstabWrite int

const (
	fstabUnchanged fstabWrite = iota // an entry for the UUID was already present
	fstabAppended                    // the entry was appended to an existing fstab
	fstabCreated                     // fstab did not exist and was created with the entry
)

// apfsUUID matches the UUID format reported by diskutil.
var apfsUUID = regexp.MustCompile(`^[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}$`)

// FileVaultPromptModule contains all necessary configuration fields for running a FileVaultPrompt module.
type FileVaultPromptModule struct {
	NoAutoMountInternalVolumes bool `toml:"NoAutoMountInternalVolumes"` // keep macOS from automounting the host's FileVault volumes
}

// apfsVolume is a volume from `diskutil apfs list -plist`. Container is the ContainerReference of its parent.
type apfsVolume struct {
	DeviceIdentifier string   `json:"DeviceIdentifier"`
	Roles            []string `json:"Roles"`
	UUID             string   `json:"APFSVolumeUUID"`
	FileVault        bool     `json:"FileVault"`
	Container        string
}

// diskInfo is the subset of `diskutil info -plist` used here.
type diskInfo struct {
	Internal  bool   `json:"Internal"`
	Container string `json:"APFSContainerReference"`
}

// internalLookup reports whether an APFS container is on a disk internal to the host.
type internalLookup func(container string) (bool, error)

// Do for FileVaultPromptModule keeps macOS from automounting FileVault-encrypted volumes on a disk internal to the host. Those
// volumes belong to the host's own macOS install: they are locked, and macOS would otherwise prompt at boot for a
// password the instance does not have. Volumes in the booted container are never touched.
func (c *FileVaultPromptModule) Do(ctx *ModuleContext) (message string, err error) {
	if !c.NoAutoMountInternalVolumes {
		return "Not requested to disable automount of internal FileVault volumes", nil
	}

	volumes, err := listVolumes()
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error listing volumes: %w", err)
	}
	var booted diskInfo
	if err := diskutilJSON(&booted, "info", "-plist", bootedMount); err != nil {
		return "", fmt.Errorf("ec2macosinit: error getting booted container: %w", err)
	}
	if booted.Container == "" {
		return "", fmt.Errorf("ec2macosinit: booted container not reported for %s", bootedMount)
	}

	return noAutoMountInternalVolumes(ctx, volumes, booted.Container, cachedInternalLookup(diskutilInternal), fstabFile)
}

// noAutoMountInternalVolumes writes an fstab entry for every FileVault volume outside the booted container whose disk is
// internal to the host. internal is only consulted for FileVault volumes outside the booted container, so most volumes
// cost no extra diskutil call.
func noAutoMountInternalVolumes(ctx *ModuleContext, volumes []apfsVolume, bootedContainer string, internal internalLookup, fstab string) (message string, err error) {
	var added, present []string
	for _, v := range volumes {
		if !v.FileVault || v.Container == bootedContainer {
			continue
		}
		isInternal, err := internal(v.Container)
		if err != nil {
			return "", fmt.Errorf("ec2macosinit: error checking volume %s: %w", v.DeviceIdentifier, err)
		}
		if !isInternal {
			continue
		}
		write, err := ensureNoAutoFstabEntry(fstab, v.UUID)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				ctx.Logger.Errorf("ec2-macos-init needs Full Disk Access to modify %s", fstab)
			}
			return "", fmt.Errorf("ec2macosinit: error updating %s for %s (%s): %w", fstab, v.DeviceIdentifier, v.UUID, err)
		}
		switch write {
		case fstabCreated:
			ctx.Logger.Infof("Created %s with an entry to suppress the FileVault prompt for internal volume %s (%s)", fstab, v.DeviceIdentifier, v.UUID)
			added = append(added, v.DeviceIdentifier)
		case fstabAppended:
			ctx.Logger.Infof("Appended an entry to %s to suppress the FileVault prompt for internal volume %s (%s)", fstab, v.DeviceIdentifier, v.UUID)
			added = append(added, v.DeviceIdentifier)
		default:
			present = append(present, v.DeviceIdentifier)
		}
	}
	if len(added)+len(present) == 0 {
		return "no volumes matched", nil
	}
	return fmt.Sprintf("noauto entries added [%s], already present [%s]", strings.Join(added, " "), strings.Join(present, " ")), nil
}

// ensureNoAutoFstabEntry appends a marked noauto entry for uuid to fstab unless one is already present, and reports
// whether fstab was created, appended to, or left alone. Existing entries are matched on the first field so
// tab-separated and lowercase entries count.
func ensureNoAutoFstabEntry(fstab string, uuid string) (fstabWrite, error) {
	if !apfsUUID.MatchString(uuid) {
		return fstabUnchanged, fmt.Errorf("invalid volume UUID [%s]", uuid)
	}
	spec := "UUID=" + uuid
	existing, err := os.ReadFile(fstab)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fstabUnchanged, err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], spec) {
			return fstabUnchanged, nil
		}
	}

	entry := fstabMarker + "\n" + spec + " none auto " + fstabOptions + "\n"
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		entry = "\n" + entry
	}
	f, err := os.OpenFile(fstab, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fstabUnchanged, err
	}
	if _, err := f.WriteString(entry); err != nil {
		_ = f.Close()
		return fstabUnchanged, err
	}
	if err := f.Close(); err != nil {
		return fstabUnchanged, err
	}
	if exists {
		return fstabAppended, nil
	}
	return fstabCreated, nil
}

// listVolumes returns every APFS volume with Container set to its parent container.
func listVolumes() ([]apfsVolume, error) {
	var apfsList struct {
		Containers []struct {
			ContainerReference string       `json:"ContainerReference"`
			Volumes            []apfsVolume `json:"Volumes"`
		} `json:"Containers"`
	}
	if err := diskutilJSON(&apfsList, "apfs", "list", "-plist"); err != nil {
		return nil, err
	}
	var volumes []apfsVolume
	for _, c := range apfsList.Containers {
		for _, v := range c.Volumes {
			v.Container = c.ContainerReference
			volumes = append(volumes, v)
		}
	}
	return volumes, nil
}

// diskutilInternal reports whether container is on an internal disk, using `diskutil info -plist`.
func diskutilInternal(container string) (bool, error) {
	var info diskInfo
	if err := diskutilJSON(&info, "info", "-plist", container); err != nil {
		return false, err
	}
	return info.Internal, nil
}

// cachedInternalLookup wraps lookup so each container is queried at most once.
func cachedInternalLookup(lookup internalLookup) internalLookup {
	cache := map[string]bool{}
	return func(container string) (bool, error) {
		if v, ok := cache[container]; ok {
			return v, nil
		}
		v, err := lookup(container)
		if err != nil {
			return false, err
		}
		cache[container] = v
		return v, nil
	}
}

// diskutilJSON runs diskutil with args, converts the plist output to JSON with plutil and decodes it into out. The two
// commands run without a shell so each exit status is checked and stderr is kept for the error. diskutil runs to
// completion before plutil starts so a diskutil failure is reported as such and not as a plutil parse error. Each
// command is bounded by diskutilTimeout.
func diskutilJSON(out interface{}, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), diskutilTimeout)
	defer cancel()
	plist, err := exec.CommandContext(ctx, "/usr/sbin/diskutil", args...).Output()
	if err != nil {
		return fmt.Errorf("diskutil %s: %s", strings.Join(args, " "), describeExecError(ctx, err))
	}
	plutilCtx, cancelPlutil := context.WithTimeout(context.Background(), diskutilTimeout)
	defer cancelPlutil()
	plutil := exec.CommandContext(plutilCtx, "/usr/bin/plutil", "-convert", "json", "-o", "-", "-")
	plutil.Stdin = bytes.NewReader(plist)
	data, err := plutil.Output()
	if err != nil {
		return fmt.Errorf("plutil for diskutil %s: %s", strings.Join(args, " "), describeExecError(plutilCtx, err))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decoding diskutil %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// describeExecError appends captured stderr, if any, to an error from exec.Cmd.Output, and names the timeout when the
// command was killed for exceeding it.
func describeExecError(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", diskutilTimeout)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Sprintf("%s: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err.Error()
}
