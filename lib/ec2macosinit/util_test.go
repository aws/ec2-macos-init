package ec2macosinit

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"strings"
	"testing"
	"time"
)

func Test_ioReadCloserToString(t *testing.T) {
	expected := "test string"
	input := ioutil.NopCloser(strings.NewReader(expected))

	out, err := ioReadCloserToString(input)
	assert.NoError(t, err)
	assert.Equal(t, expected, out)
}

func Test_retry(t *testing.T) {
	type args struct {
		attempts int
		sleep    time.Duration
		f        func() error
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "FunctionWithNoError",
			args: args{
				attempts: 2,
				sleep:    1 * time.Nanosecond,
				f: func() error {
					return nil
				},
			},
			wantErr: false,
		},
		{
			name: "FunctionWithError",
			args: args{
				attempts: 2,
				sleep:    1 * time.Nanosecond,
				f: func() error {
					return fmt.Errorf("test error")
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := retry(tt.args.attempts, tt.args.sleep, tt.args.f); (err != nil) != tt.wantErr {
				t.Errorf("retry() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
