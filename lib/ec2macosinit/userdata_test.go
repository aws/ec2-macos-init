package ec2macosinit

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserdataReader_ValidTexts(t *testing.T) {
	const expected = "hello, world!"
	texts := []string{
		"aGVsbG8sIHdvcmxkIQ==", // printf 'hello, world!' | base64 -w0
		"hello, world!",
	}

	for i, text := range texts {
		t.Run(fmt.Sprintf("Text_%d", i), func(t *testing.T) {
			t.Logf("input: %q", text)
			actual, err := io.ReadAll(userdataReader(text))
			assert.NoError(t, err, "should prepare a reader")
			assert.Equal(t, expected, string(actual), "should decode valid texts")
		})
	}
}
