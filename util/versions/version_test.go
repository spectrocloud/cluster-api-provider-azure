package versions

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestGetHigherK8sVersion(t *testing.T) {
	cases := []struct {
		name      string
		a         string
		b         string
		output    string
		expectErr bool
	}{
		{
			name:      "a is greater than b",
			a:         "v1.17.8",
			b:         "v1.18.8",
			output:    "v1.18.8",
			expectErr: false,
		},
		{
			name:      "b is greater than a",
			a:         "v1.18.9",
			b:         "v1.18.8",
			output:    "v1.18.9",
			expectErr: false,
		},
		{
			name:      "b is greater than a",
			a:         "v1.18",
			b:         "v1.18.8",
			output:    "v1.18.8",
			expectErr: false,
		},
		{
			name:      "a is equal to b",
			a:         "v1.18.8",
			b:         "v1.18.8",
			output:    "v1.18.8",
			expectErr: false,
		},
		{
			name:      "a is greater than b and a is major.minor",
			a:         "v1.18",
			b:         "v1.17.8",
			output:    "v1.18",
			expectErr: false,
		},
		{
			name:      "a is greater than b and a is major.minor",
			a:         "1.18",
			b:         "1.17.8",
			output:    "1.18",
			expectErr: false,
		},
		{
			name:      "a is invalid",
			a:         "1.18.",
			b:         "v1.17.8",
			output:    "",
			expectErr: true,
		},
		{
			name:      "b is invalid",
			a:         "1.18.1",
			b:         "v1.17.8.",
			output:    "",
			expectErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)
			output, err := GetHigherK8sVersion(c.a, c.b)
			g.Expect(output).To(Equal(c.output))
			if c.expectErr {
				g.Expect(err).NotTo(BeNil())
			} else {
				g.Expect(err).To(BeNil())
			}
		})
	}
}
