package ec2macosinit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_generateHistoryKey(t *testing.T) {
	inputs := []Module{
		{
			Type:          "testmodule",
			Name:          "test1",
			PriorityGroup: 1,
			RunOnce:       true,
		},
		{
			Type:          "testmodule",
			Name:          "test2",
			PriorityGroup: 2,
			RunPerBoot:    true,
		},
		{
			Type:           "testmodule",
			Name:           "test3",
			PriorityGroup:  3,
			RunPerInstance: true,
		},
	}
	expected := []string{
		"1_RunOnce_testmodule_test1",
		"2_RunPerBoot_testmodule_test2",
		"3_RunPerInstance_testmodule_test3",
	}

	for i, tt := range inputs {
		a := tt.generateHistoryKey()
		assert.Equal(t, expected[i], a)
	}
}
