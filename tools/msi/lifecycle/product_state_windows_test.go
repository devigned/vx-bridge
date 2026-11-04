package main

import (
	"errors"
	"testing"
)

func TestUnavailableInstalledPropertiesRemainExplicitPartialState(t *testing.T) {
	for _, property := range []string{"VersionString", "InstallLocation", "LocalPackage"} {
		err := &msiPropertyError{Property: property, Code: 1608, Size: 32768}
		if !unavailableInstalledProperty(property, err) {
			t.Fatal("advertised/incomplete property was not recognized explicitly", property)
		}
	}
	for _, test := range []struct {
		property string
		err      error
	}{
		{"State", &msiPropertyError{Property: "State", Code: 1608}},
		{"VersionString", &msiPropertyError{Property: "VersionString", Code: 1605}},
		{"VersionString", &msiPropertyError{Property: "VersionString", Code: 1610}},
		{"VersionString", errors.New("unexpected API failure")},
	} {
		if unavailableInstalledProperty(test.property, test.err) {
			t.Fatal("unknown product/bad configuration/state query was hidden", test)
		}
	}
}
