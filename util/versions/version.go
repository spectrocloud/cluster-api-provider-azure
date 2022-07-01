/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package versions

import (
	semverv4 "github.com/blang/semver"
)

// GetHigherK8sVersion returns the higher k8s version out of a and b.
func GetHigherK8sVersion(a, b string) string {
	v1, errv1 := semverv4.ParseTolerant(a)
	v2, errv2 := semverv4.ParseTolerant(b)
	if errv1 != nil && errv2 != nil {
		return ""
	}
	if errv1 != nil {
		return b
	}
	if errv2 != nil {
		return a
	}
	if v1.GTE(v2) {
		return a
	}
	return b
}
