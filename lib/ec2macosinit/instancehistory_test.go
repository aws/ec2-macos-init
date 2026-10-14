package ec2macosinit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryRecorder_Record(t *testing.T) {
	dir := t.TempDir()
	instanceID := "i-abc123"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, instanceID), 0755))

	recorder := NewHistoryRecorder(instanceID, dir, "history.json")

	m := &Module{
		Name:          "testmod",
		Type:          "command",
		PriorityGroup: 1,
		RunOnce:       true,
		Success:       true,
	}

	err := recorder.Record(m)
	require.NoError(t, err)

	// Verify the history file was written
	data, err := os.ReadFile(filepath.Join(dir, instanceID, "history.json"))
	require.NoError(t, err)

	var h History
	require.NoError(t, json.Unmarshal(data, &h))

	assert.Equal(t, instanceID, h.InstanceID)
	assert.Equal(t, historyVersion, h.Version)
	require.Len(t, h.ModuleHistories, 1)
	assert.Equal(t, m.generateHistoryKey(), h.ModuleHistories[0].Key)
	assert.True(t, h.ModuleHistories[0].Success)
}

func TestHistoryRecorder_RecordMultiple(t *testing.T) {
	dir := t.TempDir()
	instanceID := "i-multi456"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, instanceID), 0755))

	recorder := NewHistoryRecorder(instanceID, dir, "history.json")

	modules := []*Module{
		{Name: "mod1", Type: "command", PriorityGroup: 1, RunOnce: true, Success: true},
		{Name: "mod2", Type: "sshkeys", PriorityGroup: 1, RunPerBoot: true, Success: false},
		{Name: "mod3", Type: "motd", PriorityGroup: 2, RunPerInstance: true, Success: true},
	}

	for _, m := range modules {
		require.NoError(t, recorder.Record(m))
	}

	data, err := os.ReadFile(filepath.Join(dir, instanceID, "history.json"))
	require.NoError(t, err)

	var h History
	require.NoError(t, json.Unmarshal(data, &h))

	assert.Equal(t, instanceID, h.InstanceID)
	require.Len(t, h.ModuleHistories, 3)

	for i, m := range modules {
		assert.Equal(t, m.generateHistoryKey(), h.ModuleHistories[i].Key)
		assert.Equal(t, m.Success, h.ModuleHistories[i].Success)
	}
}

func TestHistoryRecorder_RecordConcurrent(t *testing.T) {
	dir := t.TempDir()
	instanceID := "i-concurrent789"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, instanceID), 0755))

	recorder := NewHistoryRecorder(instanceID, dir, "history.json")

	modules := make([]*Module, 20)
	for i := range modules {
		modules[i] = &Module{
			Name:          "mod" + string(rune('A'+i)),
			Type:          "command",
			PriorityGroup: 1,
			RunPerBoot:    true,
			Success:       true,
		}
	}

	var wg sync.WaitGroup
	for _, m := range modules {
		wg.Add(1)
		go func(m *Module) {
			defer wg.Done()
			assert.NoError(t, recorder.Record(m))
		}(m)
	}
	wg.Wait()

	data, err := os.ReadFile(filepath.Join(dir, instanceID, "history.json"))
	require.NoError(t, err)

	var h History
	require.NoError(t, json.Unmarshal(data, &h))

	assert.Equal(t, instanceID, h.InstanceID)
	assert.Len(t, h.ModuleHistories, 20)
}

func TestHistoryRecorder_RecordFailedModule(t *testing.T) {
	dir := t.TempDir()
	instanceID := "i-fail000"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, instanceID), 0755))

	recorder := NewHistoryRecorder(instanceID, dir, "history.json")

	m := &Module{
		Name:          "failing",
		Type:          "userdata",
		PriorityGroup: 2,
		RunPerInstance: true,
		Success:       false,
	}

	require.NoError(t, recorder.Record(m))

	data, err := os.ReadFile(filepath.Join(dir, instanceID, "history.json"))
	require.NoError(t, err)

	var h History
	require.NoError(t, json.Unmarshal(data, &h))

	require.Len(t, h.ModuleHistories, 1)
	assert.False(t, h.ModuleHistories[0].Success)
}

func TestHistoryRecorder_IncrementalPersistence(t *testing.T) {
	dir := t.TempDir()
	instanceID := "i-incr111"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, instanceID), 0755))

	recorder := NewHistoryRecorder(instanceID, dir, "history.json")
	historyFile := filepath.Join(dir, instanceID, "history.json")

	m1 := &Module{Name: "first", Type: "command", PriorityGroup: 1, RunOnce: true, Success: true}
	require.NoError(t, recorder.Record(m1))

	// After first record, file should exist with 1 entry
	data, err := os.ReadFile(historyFile)
	require.NoError(t, err)
	var h1 History
	require.NoError(t, json.Unmarshal(data, &h1))
	assert.Len(t, h1.ModuleHistories, 1)

	m2 := &Module{Name: "second", Type: "motd", PriorityGroup: 1, RunPerBoot: true, Success: true}
	require.NoError(t, recorder.Record(m2))

	// After second record, file should have 2 entries
	data, err = os.ReadFile(historyFile)
	require.NoError(t, err)
	var h2 History
	require.NoError(t, json.Unmarshal(data, &h2))
	assert.Len(t, h2.ModuleHistories, 2)
}
