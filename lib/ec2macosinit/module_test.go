package ec2macosinit

import (
	"fmt"
	"testing"
	"time"
)

// values used via pointers in module configs
var (
	trueValue  = true
	falseValue = false
)

func TestModule_validateModule(t *testing.T) {
	tests := []struct {
		name    string
		fields  Module
		wantErr bool
	}{
		{
			name:    "Bad case: 0 Run Types set",
			fields:  Module{},
			wantErr: true,
		},
		{
			name: "Bad case: 3 Run Types set",
			fields: Module{
				RunOnce:        true,
				RunPerBoot:     true,
				RunPerInstance: true,
			},
			wantErr: true,
		},
		{
			name: "Bad case: 1 Run Type set, priority unset",
			fields: Module{
				RunOnce: true,
			},
			wantErr: true,
		},
		{
			name: "Good case: 1 Run Type set, PriorityGroup > 1",
			fields: Module{
				PriorityGroup: 2,
				RunOnce:       true,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fields.validateModule(); (err != nil) != tt.wantErr {
				t.Errorf("validateModule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestModule_identifyModule(t *testing.T) {
	tests := []struct {
		name     string
		fields   Module
		wantType string
		wantErr  bool
	}{
		{
			name:    "Bad case: unidentified or empty module",
			fields:  Module{},
			wantErr: true,
		},
		{
			name: "Good case: Command Module",
			fields: Module{
				CommandModule: CommandModule{
					RunAsUser: "ec2-user",
				},
			},
			wantType: "command",
			wantErr:  false,
		},
		{
			name: "Good case: SSHKeys Module",
			fields: Module{
				SSHKeysModule: SSHKeysModule{
					User: "ec2-user",
				},
			},
			wantType: "sshkeys",
			wantErr:  false,
		},
		{
			name: "Good case: UserData Module",
			fields: Module{
				UserDataModule: UserDataModule{
					ExecuteUserData: true,
				},
			},
			wantType: "userdata",
			wantErr:  false,
		},
		{
			name: "Good case: NetworkCheck Module",
			fields: Module{
				NetworkCheckModule: NetworkCheckModule{
					PingCount: 3,
				},
			},
			wantType: "networkcheck",
			wantErr:  false,
		},
		{
			name: "Good case: Enable secureSSHDConfig",
			fields: Module{
				SystemConfigModule: SystemConfigModule{
					SecureSSHDConfig: &trueValue,
				},
			},
			wantType: "systemconfig",
			wantErr:  false,
		},
		{
			name: "Good case: Disable secureSSHDConfig",
			fields: Module{
				SystemConfigModule: SystemConfigModule{
					SecureSSHDConfig: &falseValue,
				},
			},
			wantType: "systemconfig",
			wantErr:  false,
		},
		{
			name: "Bad case: don't provide secureSSHDConfig",
			fields: Module{
				SystemConfigModule: SystemConfigModule{
					SecureSSHDConfig: nil,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fields.identifyModule(); (err != nil) != tt.wantErr {
				t.Errorf("identifyModule() error = %v, wantErr %v", err, tt.wantErr)
			} else if tt.wantType != tt.fields.Type {
				t.Errorf("identifyModule() Type = %v, wantType %v", tt.fields.Type, tt.wantType)
			}
		})
	}
}

func TestModule_generateHistoryKey(t *testing.T) {
	tests := []struct {
		name    string
		fields  Module
		wantKey string
	}{
		{
			name: "Key with RunOnce",
			fields: Module{
				Type:          "testmodule",
				Name:          "test1",
				PriorityGroup: 1,
				RunOnce:       true,
			},
			wantKey: "1_RunOnce_testmodule_test1",
		},
		{
			name: "Key with RunPerBoot",
			fields: Module{
				Type:          "testmodule",
				Name:          "test2",
				PriorityGroup: 2,
				RunPerBoot:    true,
			},
			wantKey: "2_RunPerBoot_testmodule_test2",
		},
		{
			name: "Key with RunPerInstance",
			fields: Module{
				Type:           "testmodule",
				Name:           "test3",
				PriorityGroup:  3,
				RunPerInstance: true,
			},
			wantKey: "3_RunPerInstance_testmodule_test3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotKey := tt.fields.generateHistoryKey(); gotKey != tt.wantKey {
				t.Errorf("generateHistoryKey() = %v, want %v", gotKey, tt.wantKey)
			}
		})
	}
}

func TestModule_ShouldRun(t *testing.T) {
	type args struct {
		instanceID string
		history    []History
	}
	tests := []struct {
		name          string
		fields        Module
		args          args
		wantShouldRun bool
	}{
		{
			name:          "No Run Type set",
			fields:        Module{},
			args:          args{},
			wantShouldRun: false,
		},
		{
			name: "RunPerBoot Module",
			fields: Module{
				RunPerBoot: true,
			},
			args:          args{},
			wantShouldRun: true,
		},
		{
			name: "RunPerInstance - No matches",
			fields: Module{ // key will be 2_RunPerInstance_testType_testName
				Name:           "testName",
				PriorityGroup:  2,
				RunPerInstance: true,
				Type:           "testType",
			},
			args: args{
				instanceID: "i-1234567890ab",
				history: []History{
					{
						InstanceID:      "i-ba0987654321",
						RunTime:         time.Time{},
						ModuleHistories: []ModuleHistory{},
					},
				},
			},
			wantShouldRun: true,
		},
		{
			name: "RunPerInstance - Instance match with no keys",
			fields: Module{ // key will be 2_RunPerInstance_testType_testName
				Name:           "testName",
				PriorityGroup:  2,
				RunPerInstance: true,
				Type:           "testType",
			},
			args: args{
				instanceID: "i-1234567890ab",
				history: []History{
					{
						InstanceID:      "i-1234567890ab",
						RunTime:         time.Time{},
						ModuleHistories: []ModuleHistory{},
					},
				},
			},
			wantShouldRun: true,
		},
		{
			name: "RunPerInstance - Instance match with key match",
			fields: Module{ // key will be 2_RunPerInstance_testType_testName
				Name:           "testName",
				PriorityGroup:  2,
				RunPerInstance: true,
				Type:           "testType",
			},
			args: args{
				instanceID: "i-1234567890ab",
				history: []History{
					{
						InstanceID: "i-1234567890ab",
						RunTime:    time.Time{},
						ModuleHistories: []ModuleHistory{
							{
								Key:     "2_RunPerInstance_testType_testName",
								Success: true,
							},
						},
					},
				},
			},
			wantShouldRun: false,
		},
		{
			name: "RunOnce - No matches",
			fields: Module{ // key will be 2_RunOnce_testType_testName
				Name:          "testName",
				PriorityGroup: 2,
				RunOnce:       true,
				Type:          "testType",
			},
			args: args{
				instanceID: "i-1234567890ab",
				history: []History{
					{
						InstanceID:      "i-ba0987654321",
						RunTime:         time.Time{},
						ModuleHistories: []ModuleHistory{},
					},
				},
			},
			wantShouldRun: true,
		},
		{
			name: "RunOnce - Key match",
			fields: Module{ // key will be 2_RunOnce_testType_testName
				Name:          "testName",
				PriorityGroup: 2,
				RunOnce:       true,
				Type:          "testType",
			},
			args: args{
				instanceID: "i-1234567890ab",
				history: []History{
					{
						InstanceID: "i-1234567890ab",
						RunTime:    time.Time{},
						ModuleHistories: []ModuleHistory{
							{
								Key:     "2_RunOnce_testType_testName",
								Success: true,
							},
						},
					},
				},
			},
			wantShouldRun: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotShouldRun := tt.fields.ShouldRun(tt.args.instanceID, tt.args.history); gotShouldRun != tt.wantShouldRun {
				t.Errorf("ShouldRun() = %v, want %v", gotShouldRun, tt.wantShouldRun)
				fmt.Println(tt.fields.generateHistoryKey())
			}
		})
	}
}
