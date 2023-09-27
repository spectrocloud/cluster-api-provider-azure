package versions

import (
	semverv4 "github.com/blang/semver"
	"github.com/pkg/errors"
)

// GetHigherK8sVersion returns the higher k8s version out of a and b
func GetHigherK8sVersion(a, b string) (string, error) {
	v1, err := semverv4.ParseTolerant(a)
	if err != nil {
		return "", errors.Wrap(err, "error parsing k8s version")
	}
	v2, err := semverv4.ParseTolerant(b)
	if err != nil {
		return "", errors.Wrap(err, "error parsing k8s version")
	}
	if v1.GTE(v2) {
		return a, nil
	}
	return b, nil
}
