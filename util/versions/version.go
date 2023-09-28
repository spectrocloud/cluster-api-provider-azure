package versions

import (
	semverv4 "github.com/blang/semver"
	"github.com/pkg/errors"
)

// GetHigherK8sVersion returns the higher k8s version out of a and b.
func GetHigherK8sVersion(a, b string) (string, error) {
	v1, errv1 := semverv4.ParseTolerant(a)
	v2, errv2 := semverv4.ParseTolerant(b)
	if errv1 != nil && errv2 != nil {
		return "", errors.Wrapf(errv1, "error parsing k8s version %s, %v error parsing k8s version %s", a, errv2, b)
	}
	if errv1 != nil {
		return b, nil
	}
	if errv2 != nil {
		return a, nil
	}

	if v1.GTE(v2) {
		return a, nil
	}
	return b, nil
}
