package ec2macosinit

import (
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"strings"
	"testing"
)

func Test_ioReadCloserToString(t *testing.T) {
	expected := "test string"
	input := ioutil.NopCloser(strings.NewReader(expected))

	out, err := ioReadCloserToString(input)
	assert.NoError(t, err)
	assert.Equal(t, expected, out)
}
