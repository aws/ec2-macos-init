package ec2macosinit

import (
	"testing"
)

func TestUserManagementModule_Do(t *testing.T) {
	var emptyCtx ModuleContext
	type fields struct {
		RandomizePassword bool
		User              string
	}
	type args struct {
		ctx *ModuleContext
	}
	tests := []struct {
		name        string
		fields      fields
		args        args
		wantMessage string
		wantErr     bool
	}{
		{"No Randomization", fields{RandomizePassword: false, User: "ec2-user"}, args{&emptyCtx}, "randomizing password disabled, skipping", false},
		{"User doesn't exist", fields{RandomizePassword: true, User: "thereisnowaythisusercouldexist"}, args{&emptyCtx}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &UserManagementModule{
				RandomizePassword: tt.fields.RandomizePassword,
				User:              tt.fields.User,
			}
			gotMessage, err := c.Do(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Do() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotMessage != tt.wantMessage {
				t.Errorf("Do() gotMessage = %v, want %v", gotMessage, tt.wantMessage)
			}
		})
	}
}

func Test_generateRandomBytes(t *testing.T) {
	type args struct {
		n int
	}
	tests := []struct {
		name          string
		args          args
		exampleResult []byte
		wantErr       bool
	}{
		{"Basic case", args{18}, []byte{'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', '=', '='}, false},
		{"Slice of 1", args{1}, []byte{'A'}, false},
		{"Empty case", args{0}, []byte{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateRandomBytes(tt.args.n)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateRandomBytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Check that length is correct, since its random, no more can be done without adjusting seeding
			if len(got) != len(tt.exampleResult) {
				t.Errorf("generateRandomBytes() length of got = %d, want %d", len(got), len(tt.exampleResult))
			}
		})
	}
}

func Test_generateSecurePassword(t *testing.T) {
	type args struct {
		length int
	}
	tests := []struct {
		name            string
		args            args
		examplePassword string
		wantErr         bool
	}{
		// Randomly create some tests, run the same one over and over to ensure seeding is working
		{"Basic case 1", args{25}, "Qfmk0rD8HAq3zZD37hvs41234", false},
		{"Basic case 2", args{25}, "5iWL3MoeSTQ0ILk4hC4s43214", false},
		{"Basic case 3", args{25}, "y0pFxuh_sTp1qhp_WCv3w4afd", false},
		{"Basic case 4", args{25}, "q2yogfL6JCDntj9cYfdszda35", false},
		{"Basic case 5", args{25}, "TI29Yhy32f3tZtsj42q34rCgG", false},
		{"Basic case 6", args{25}, "4Y0FGvwsFcCm-2QtadfzR9324", false},
		{"Short password", args{6}, "Ad8-3S", false},
	}
	// Build a map that will detect duplicates
	repeatedResults := make(map[string]bool)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPassword, err := generateSecurePassword(tt.args.length)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateSecurePassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			// Check that length is correct, since its random, no more can be done without adjusting seeding
			if len(gotPassword) != len(tt.examplePassword) {
				t.Errorf("generateSecurePassword() length of gotPassword = %d, wantPassword %v", len(gotPassword), len(tt.examplePassword))
			}
			// Add to the map for detecting duplicates
			repeatedResults[gotPassword] = true
		})
	}
	// Fail if there are fewer deduplicated passwords than tests
	if len(repeatedResults) < len(tests) {
		t.Errorf("generateSecurePassword() collision detected: length of unique passwords: %d, number of tests: %d", len(repeatedResults), len(tests))
	}
}
