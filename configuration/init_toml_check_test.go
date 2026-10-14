package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aws/ec2-macos-init/lib/ec2macosinit"
)

// Load and test the current copy of init.toml to ensure it remains
// loadable and valid for use in packaging.

func TestConfiguration_initTOML(t *testing.T) {
	var loadedConfig ec2macosinit.InitConfig

	loadErr := loadedConfig.ReadConfig("./init.toml")
	assert.NoError(t, loadErr, "should be able to load config file")
	require.NotEmpty(t, loadedConfig.Modules, "should have modules configured")

	validateErr := loadedConfig.ValidateAndIdentify()
	assert.NoError(t, validateErr, "should have valid modules")
}
