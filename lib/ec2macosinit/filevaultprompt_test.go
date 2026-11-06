package ec2macosinit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// layout is one disk shape as seen by the module: `diskutil apfs list -plist` and `diskutil info -plist /`, both
// through `plutil -convert json`, trimmed to the internal container, the booted EBS container and the volumes the
// module has to skip. Captured from macOS 27.0 26A428 guests. The booted container is the same AMI everywhere.
type layout struct {
	name              string
	apfsList          string   // diskutil apfs list
	rootInfo          string   // diskutil info /
	internalContainer string   // the only container `diskutil info` reports as Internal
	fileVaultDevices  []string // what the module must select, in list order
}

var (
	// fileVaultHost: the host has FileVault on and its Data volume is named "Data". Captured on mac-m4.metal.
	fileVaultHost = layout{
		name:              "fileVaultHost",
		internalContainer: "disk3",
		fileVaultDevices:  []string{"disk3s1", "disk3s5"},
		rootInfo:          `{"Internal":false,"APFSContainerReference":"disk7","VolumeName":"Macintosh HD"}`,
		apfsList: `{"Containers":[
			{"ContainerReference":"disk3","Volumes":[
				{"DeviceIdentifier":"disk3s1","Name":"InternalDisk","Roles":["System"],"APFSVolumeUUID":"F880827A-92AC-4324-BADD-4679A4233EAC","FileVault":true},
				{"DeviceIdentifier":"disk3s2","Name":"Preboot","Roles":["Preboot"],"APFSVolumeUUID":"01993A54-223C-4E6B-80CF-9F2A05FBF867","FileVault":false},
				{"DeviceIdentifier":"disk3s5","Name":"Data","Roles":["Data"],"APFSVolumeUUID":"1C134AB4-75B1-45E3-94AD-AE21DAC0B16B","FileVault":true},
				{"DeviceIdentifier":"disk3s6","Name":"VM","Roles":["VM"],"APFSVolumeUUID":"BB3234D3-E594-458B-ACBF-21BA857EDA17","FileVault":false}]},
			{"ContainerReference":"disk6","Volumes":[
				{"DeviceIdentifier":"disk6s1","Name":"ephemeral0","Roles":[],"APFSVolumeUUID":"25AC528A-99A0-4A53-9242-38B97DD81C6A","FileVault":false}]},
			{"ContainerReference":"disk7","Volumes":[
				{"DeviceIdentifier":"disk7s1","Name":"Macintosh HD - Data","Roles":["Data"],"APFSVolumeUUID":"5D0DC6E3-D6E6-4D1E-BBA6-A303F5BAB444","FileVault":false},
				{"DeviceIdentifier":"disk7s2","Name":"Macintosh HD","Roles":["System"],"APFSVolumeUUID":"96EB0F9C-6594-411B-BC84-75D8CFA2B143","FileVault":false},
				{"DeviceIdentifier":"disk7s4","Name":"Preboot","Roles":["Preboot"],"APFSVolumeUUID":"1CA6F0BF-16E5-4424-86BA-1453AC19F1B3","FileVault":false}]}]}`,
	}

	// fileVaultHostRenamed: FileVault on, but the Data volume is "InternalDisk - Data" and a role-less, unlocked
	// volume is named "Data". Captured on mac2.metal.
	fileVaultHostRenamed = layout{
		name:              "fileVaultHostRenamed",
		internalContainer: "disk3",
		fileVaultDevices:  []string{"disk3s1", "disk3s5"},
		rootInfo:          `{"Internal":false,"APFSContainerReference":"disk5","VolumeName":"Macintosh HD"}`,
		apfsList: `{"Containers":[
			{"ContainerReference":"disk3","Volumes":[
				{"DeviceIdentifier":"disk3s1","Name":"InternalDisk","Roles":["System"],"APFSVolumeUUID":"30B19BCA-61B9-4ACB-98A1-165C7B0CDF0F","FileVault":true},
				{"DeviceIdentifier":"disk3s5","Name":"InternalDisk - Data","Roles":["Data"],"APFSVolumeUUID":"E6E73630-9560-4301-8A3A-07865CCD4953","FileVault":true},
				{"DeviceIdentifier":"disk3s6","Name":"VM","Roles":["VM"],"APFSVolumeUUID":"9BC6FAC5-45A7-4777-8BC3-AE49BC623BD8","FileVault":false},
				{"DeviceIdentifier":"disk3s7","Name":"Data","Roles":[],"APFSVolumeUUID":"8EA69414-2EE0-4D7A-B5EE-ADAC19DF28A4","FileVault":false}]},
			{"ContainerReference":"disk5","Volumes":[
				{"DeviceIdentifier":"disk5s1","Name":"Macintosh HD - Data","Roles":["Data"],"APFSVolumeUUID":"5D0DC6E3-D6E6-4D1E-BBA6-A303F5BAB444","FileVault":false},
				{"DeviceIdentifier":"disk5s2","Name":"Macintosh HD","Roles":["System"],"APFSVolumeUUID":"96EB0F9C-6594-411B-BC84-75D8CFA2B143","FileVault":false}]}]}`,
	}

	// noFileVaultHost: the host has FileVault off. Internal volumes are encrypted but unlocked, so nothing matches.
	// Captured on mac2-m2pro.metal.
	noFileVaultHost = layout{
		name:              "noFileVaultHost",
		internalContainer: "disk3",
		rootInfo:          `{"Internal":false,"APFSContainerReference":"disk5","VolumeName":"Macintosh HD"}`,
		apfsList: `{"Containers":[
			{"ContainerReference":"disk3","Volumes":[
				{"DeviceIdentifier":"disk3s1","Name":"InternalDisk","Roles":["System"],"APFSVolumeUUID":"5274A055-5B78-4F13-A1B2-62F0234F4FBC","FileVault":false},
				{"DeviceIdentifier":"disk3s5","Name":"Data","Roles":["Data"],"APFSVolumeUUID":"F47F0BE7-5EF9-47C7-ABF0-77A41E145A2A","FileVault":false}]},
			{"ContainerReference":"disk5","Volumes":[
				{"DeviceIdentifier":"disk5s1","Name":"Macintosh HD - Data","Roles":["Data"],"APFSVolumeUUID":"5D0DC6E3-D6E6-4D1E-BBA6-A303F5BAB444","FileVault":false},
				{"DeviceIdentifier":"disk5s2","Name":"Macintosh HD","Roles":["System"],"APFSVolumeUUID":"96EB0F9C-6594-411B-BC84-75D8CFA2B143","FileVault":false}]}]}`,
	}

	layouts = []layout{fileVaultHost, fileVaultHostRenamed, noFileVaultHost}
)

// volumes decodes apfsList the same way listVolumes does.
func (l layout) volumes(t *testing.T) []apfsVolume {
	var apfsList struct {
		Containers []struct {
			ContainerReference string       `json:"ContainerReference"`
			Volumes            []apfsVolume `json:"Volumes"`
		} `json:"Containers"`
	}
	require.NoError(t, json.Unmarshal([]byte(l.apfsList), &apfsList))
	var volumes []apfsVolume
	for _, c := range apfsList.Containers {
		require.NotEmpty(t, c.ContainerReference)
		for _, v := range c.Volumes {
			v.Container = c.ContainerReference
			volumes = append(volumes, v)
		}
	}
	return volumes
}

// booted decodes rootInfo the same way Do does and returns the booted container.
func (l layout) booted(t *testing.T) string {
	var info diskInfo
	require.NoError(t, json.Unmarshal([]byte(l.rootInfo), &info))
	require.False(t, info.Internal, "the EBS root is never internal")
	require.NotEmpty(t, info.Container)
	return info.Container
}

// internal answers the Internal lookup for the layout, cached as in Do, and counts the underlying calls.
func (l layout) internal(calls *int) internalLookup {
	return cachedInternalLookup(func(container string) (bool, error) {
		*calls++
		return container == l.internalContainer, nil
	})
}

// device returns the volume with the given device identifier.
func device(t *testing.T, volumes []apfsVolume, id string) apfsVolume {
	for _, v := range volumes {
		if v.DeviceIdentifier == id {
			return v
		}
	}
	t.Fatalf("no volume %s in layout", id)
	return apfsVolume{}
}

// uuidsInFstab returns the UUIDs of every entry in content, in order.
func uuidsInFstab(content string) []string {
	var uuids []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "UUID=") {
			uuids = append(uuids, strings.TrimPrefix(strings.Fields(line)[0], "UUID="))
		}
	}
	return uuids
}

func TestDiskInfo_decode(t *testing.T) {
	// Field names as `diskutil info -plist | plutil -convert json` emits them, captured on mac-m4.metal.
	const internalContainer = `{"DeviceIdentifier":"disk3","Internal":true,"APFSContainerReference":"disk3","WholeDisk":true,"SolidState":true}`
	var info diskInfo
	require.NoError(t, json.Unmarshal([]byte(internalContainer), &info))
	assert.Equal(t, diskInfo{Internal: true, Container: "disk3"}, info)
	for _, l := range layouts {
		assert.NotEqual(t, l.internalContainer, l.booted(t), "%s: booted and internal containers must differ", l.name)
	}
}

func TestFileVaultPromptModule_Do_notRequested(t *testing.T) {
	// Returns before any diskutil call, so this runs anywhere.
	message, err := (&FileVaultPromptModule{}).Do(&ModuleContext{Logger: &Logger{}})
	require.NoError(t, err)
	assert.Equal(t, "Not requested to disable automount of internal FileVault volumes", message)
}

// TestNoAutoMountInternalVolumes_layouts runs the module against every captured layout.
func TestNoAutoMountInternalVolumes_layouts(t *testing.T) {
	for _, l := range layouts {
		t.Run(l.name, func(t *testing.T) {
			volumes, booted := l.volumes(t), l.booted(t)
			fstab := filepath.Join(t.TempDir(), "fstab")
			var lookups int
			message, err := noAutoMountInternalVolumes(&ModuleContext{Logger: &Logger{}}, volumes, booted, l.internal(&lookups), fstab)
			require.NoError(t, err)

			var wantUUIDs []string
			for _, id := range l.fileVaultDevices {
				v := device(t, volumes, id)
				assert.True(t, v.FileVault, "%s must be FileVault in the layout", id)
				assert.Equal(t, l.internalContainer, v.Container)
				wantUUIDs = append(wantUUIDs, v.UUID)
			}
			content, _ := os.ReadFile(fstab)
			assert.Equal(t, wantUUIDs, uuidsInFstab(string(content)))
			assert.Equal(t, len(wantUUIDs), strings.Count(string(content), fstabMarker))
			assert.Equal(t, len(wantUUIDs), strings.Count(string(content), " none auto "+fstabOptions+"\n"))

			if len(l.fileVaultDevices) == 0 {
				assert.Equal(t, "no volumes matched", message)
				assert.Zero(t, lookups, "no Internal lookup when nothing is FileVault")
				_, statErr := os.Stat(fstab)
				assert.True(t, os.IsNotExist(statErr), "fstab must not be created on a no-op")
			} else {
				assert.Equal(t, fmt.Sprintf("noauto entries added [%s], already present []", strings.Join(l.fileVaultDevices, " ")), message)
				assert.Equal(t, 1, lookups, "one Internal lookup for the one container that has FileVault volumes")
			}

			// Nothing in the booted container is ever written.
			for _, v := range volumes {
				if v.Container == booted {
					assert.NotContains(t, string(content), v.UUID, "booted container volume %s must never match", v.DeviceIdentifier)
				}
			}
		})
	}
}

// TestNoAutoMountInternalVolumes_selection checks each selection criterion on synthetic volumes: FileVault on, outside
// the booted container, and on an internal disk. Each case flips exactly one of them.
func TestNoAutoMountInternalVolumes_selection(t *testing.T) {
	const (
		internal = "disk3"
		booted   = "disk7"
		ebs      = "disk6"
		uuid     = "AAAAAAAA-0000-4000-8000-000000000001"
	)
	tests := []struct {
		name       string
		volume     apfsVolume
		wantWrite  bool
		wantLookup bool
	}{
		{name: "FileVault volume on the internal disk", volume: apfsVolume{DeviceIdentifier: "disk3s5", UUID: uuid, FileVault: true, Container: internal}, wantWrite: true, wantLookup: true},
		{name: "unlocked volume on the internal disk", volume: apfsVolume{DeviceIdentifier: "disk3s6", UUID: uuid, FileVault: false, Container: internal}},
		{name: "FileVault volume in the booted container", volume: apfsVolume{DeviceIdentifier: "disk7s1", UUID: uuid, FileVault: true, Container: booted}},
		{name: "FileVault volume on an external disk", volume: apfsVolume{DeviceIdentifier: "disk6s1", UUID: uuid, FileVault: true, Container: ebs}, wantLookup: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fstab := filepath.Join(t.TempDir(), "fstab")
			var lookups int
			lookup := func(container string) (bool, error) {
				lookups++
				return container == internal, nil
			}
			_, err := noAutoMountInternalVolumes(&ModuleContext{Logger: &Logger{}}, []apfsVolume{tt.volume}, booted, lookup, fstab)
			require.NoError(t, err)
			content, _ := os.ReadFile(fstab)
			if tt.wantWrite {
				assert.Equal(t, []string{uuid}, uuidsInFstab(string(content)))
			} else {
				assert.Empty(t, content)
			}
			assert.Equal(t, tt.wantLookup, lookups == 1, "Internal is only looked up for FileVault volumes outside the booted container")
		})
	}
}

func TestNoAutoMountInternalVolumes_idempotent(t *testing.T) {
	l := fileVaultHost
	fstab := filepath.Join(t.TempDir(), "fstab")
	ctx := &ModuleContext{Logger: &Logger{}}
	var lookups int
	_, err := noAutoMountInternalVolumes(ctx, l.volumes(t), l.booted(t), l.internal(&lookups), fstab)
	require.NoError(t, err)
	first, _ := os.ReadFile(fstab)
	assert.Equal(t, 2, strings.Count(string(first), fstabMarker))

	message, err := noAutoMountInternalVolumes(ctx, l.volumes(t), l.booted(t), l.internal(&lookups), fstab)
	require.NoError(t, err)
	assert.Equal(t, "noauto entries added [], already present [disk3s1 disk3s5]", message)
	second, _ := os.ReadFile(fstab)
	assert.Equal(t, first, second)
}

func TestNoAutoMountInternalVolumes_lookupError(t *testing.T) {
	l := fileVaultHost
	fstab := filepath.Join(t.TempDir(), "fstab")
	failing := func(string) (bool, error) { return false, fmt.Errorf("diskutil exploded") }
	_, err := noAutoMountInternalVolumes(&ModuleContext{Logger: &Logger{}}, l.volumes(t), l.booted(t), failing, fstab)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk3s1")
	assert.Contains(t, err.Error(), "diskutil exploded")
	_, statErr := os.Stat(fstab)
	assert.True(t, os.IsNotExist(statErr), "fstab must not be written when the lookup fails")
}

func TestNoAutoMountInternalVolumes_permissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	l := fileVaultHost
	fstab := filepath.Join(t.TempDir(), "fstab")
	require.NoError(t, os.WriteFile(fstab, nil, 0000))
	var lookups int
	_, err := noAutoMountInternalVolumes(&ModuleContext{Logger: &Logger{}}, l.volumes(t), l.booted(t), l.internal(&lookups), fstab)
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrPermission), "the permission error must stay unwrappable: %v", err)
	assert.Contains(t, err.Error(), "disk3s1")
}

func TestCachedInternalLookup(t *testing.T) {
	var calls int
	lookup := cachedInternalLookup(func(container string) (bool, error) {
		calls++
		if container == "bad" {
			return false, fmt.Errorf("no such disk")
		}
		return container == "disk3", nil
	})
	for i := 0; i < 3; i++ {
		v, err := lookup("disk3")
		require.NoError(t, err)
		assert.True(t, v)
		v, err = lookup("disk7")
		require.NoError(t, err)
		assert.False(t, v)
	}
	assert.Equal(t, 2, calls, "each container is queried once")
	_, err := lookup("bad")
	assert.Error(t, err)
	_, err = lookup("bad")
	assert.Error(t, err, "errors are not cached")
	assert.Equal(t, 4, calls)
}

func TestEnsureNoAutoFstabEntry(t *testing.T) {
	const (
		uuidA = "AAAAAAAA-0000-4000-8000-000000000001"
		uuidB = "BBBBBBBB-0000-4000-8000-000000000002"
	)
	entryA := fstabMarker + "\nUUID=" + uuidA + " none auto ro,noauto,nobrowse\n"
	entryB := fstabMarker + "\nUUID=" + uuidB + " none auto ro,noauto,nobrowse\n"
	tests := []struct {
		name        string
		existing    string
		uuid        string
		want        fstabWrite
		wantContent string
		wantErr     bool
	}{
		{name: "creates file", uuid: uuidA, want: fstabCreated, wantContent: entryA},
		{name: "appends after missing newline", existing: "UUID=" + uuidB + " none auto ro", uuid: uuidA, want: fstabAppended, wantContent: "UUID=" + uuidB + " none auto ro\n" + entryA},
		{name: "skips existing", existing: entryA, uuid: uuidA, want: fstabUnchanged, wantContent: entryA},
		{name: "skips tab separated entry", existing: "UUID=" + uuidA + "\tnone\tapfs\trw,noauto\n", uuid: uuidA, want: fstabUnchanged, wantContent: "UUID=" + uuidA + "\tnone\tapfs\trw,noauto\n"},
		{name: "skips lowercase entry", existing: "uuid=" + strings.ToLower(uuidA) + " none apfs rw,noauto\n", uuid: uuidA, want: fstabUnchanged, wantContent: "uuid=" + strings.ToLower(uuidA) + " none apfs rw,noauto\n"},
		{name: "commented entry does not count", existing: "# UUID=" + uuidA + " none auto ro,noauto,nobrowse\n", uuid: uuidA, want: fstabAppended, wantContent: "# UUID=" + uuidA + " none auto ro,noauto,nobrowse\n" + entryA},
		{name: "prefix does not match different uuid", existing: entryB, uuid: uuidA, want: fstabAppended, wantContent: entryB + entryA},
		{name: "rejects empty uuid", uuid: "", wantErr: true},
		{name: "rejects malformed uuid", uuid: "disk3s5 none auto rw", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fstab := filepath.Join(t.TempDir(), "fstab")
			if tt.existing != "" {
				require.NoError(t, os.WriteFile(fstab, []byte(tt.existing), 0644))
			}
			write, err := ensureNoAutoFstabEntry(fstab, tt.uuid)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, fstabUnchanged, write)
				_, statErr := os.Stat(fstab)
				assert.True(t, os.IsNotExist(statErr), "nothing is written for a rejected uuid")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, write)
			content, _ := os.ReadFile(fstab)
			assert.Equal(t, tt.wantContent, string(content))
		})
	}
}

func TestModule_identifyModule_filevaultprompt(t *testing.T) {
	m := Module{FileVaultPromptModule: FileVaultPromptModule{NoAutoMountInternalVolumes: true}}
	require.NoError(t, m.identifyModule())
	assert.Equal(t, "filevaultprompt", m.Type)

	// A module with the only option off is indistinguishable from no module; the opt-out is to remove the block.
	off := Module{FileVaultPromptModule: FileVaultPromptModule{}}
	assert.Error(t, off.identifyModule())
}

// TestShippedInitToml_filevaultprompt checks that the module block shipped in configuration/init.toml parses and is
// recognised as this module, so a typo in the file cannot silently drop the fix.
func TestShippedInitToml_filevaultprompt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "configuration", "init.toml"))
	require.NoError(t, err)
	var cfg InitConfig
	require.NoError(t, toml.Unmarshal(data, &cfg))
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if m.Name != "DisableInternalFileVaultAutomount" {
			continue
		}
		require.NoError(t, m.identifyModule())
		assert.Equal(t, "filevaultprompt", m.Type)
		assert.True(t, m.FileVaultPromptModule.NoAutoMountInternalVolumes)
		return
	}
	t.Fatal("DisableInternalFileVaultAutomount not found in configuration/init.toml")
}
