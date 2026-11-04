package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

const normalProfile = "normal"
const lifecycleFaultProfile = "private-lifecycle-fault"
const lifecycleFaultMarker = "PRIVATE_LIFECYCLE_EXPECTED_POST_EXECUTE_FAILURE"
const lifecycleFaultCondition = `NOT REMOVE AND VX_LIFECYCLE_FAIL = "1"`

// checkProfileBoundary permits normal authoring without run approval, but binds
// private fault authoring to its exact source/version and first manual hosted
// context. It does not authorize executing any produced MSI.
func checkProfileBoundary(profile, revision, version string, env func(string) string) error {
	if profile == normalProfile {
		return nil
	}
	if profile != lifecycleFaultProfile || version != "v0.0.2" ||
		env("GITHUB_ACTIONS") != "true" || env("RUNNER_ENVIRONMENT") != "github-hosted" ||
		env("GITHUB_EVENT_NAME") != "workflow_dispatch" || env("GITHUB_WORKFLOW") != "VX Bridge disposable lifecycle" ||
		env("GITHUB_RUN_ATTEMPT") != "1" || env("GITHUB_REPOSITORY") != "devigned/vx-bridge" ||
		revision != env("REVISION") || revision != env("WORKFLOW_REVISION") || len(revision) != 40 {
		return errors.New("private authoring-only fault profile requires the exact manual disposable lifecycle workflow")
	}
	return nil
}

func injectLifecycleFault(source string) string {
	nodes := `<Property Id="VX_LIFECYCLE_FAIL"><RegistrySearch Id="VxLifecycleFault" Root="HKCU" Key="Software\VXBridgeLifecycleHarness" Name="FailUpgrade" Type="raw" Win64="yes"/></Property>
<CustomAction Id="VxLifecycleExpectedFail" Error="` + lifecycleFaultMarker + `"/>
`
	source = strings.Replace(source, "<InstallExecuteSequence>", nodes+"<InstallExecuteSequence>", 1)
	return strings.Replace(source, "<InstallExecuteSequence>",
		`<InstallExecuteSequence><Custom Action="VxLifecycleExpectedFail" Sequence="6550">`+lifecycleFaultCondition+`</Custom>`, 1)
}

func verifyBuildProfile(source []byte, profile string) error {
	if profile == normalProfile {
		if bytes.Contains(source, []byte("VxLifecycle")) || bytes.Contains(source, []byte("VX_LIFECYCLE")) ||
			bytes.Contains(source, []byte("VXBridgeLifecycleHarness")) {
			return errors.New("normal production authoring must not contain private lifecycle fault rows")
		}
		return nil
	}
	if profile != lifecycleFaultProfile {
		return errors.New("unknown authoring profile")
	}
	var document struct {
		Product struct {
			Properties []struct {
				ID     string `xml:"Id,attr"`
				Value  string `xml:"Value,attr"`
				Search []struct {
					ID                           string `xml:"Id,attr"`
					Root, Key, Name, Type, Win64 string `xml:",attr"`
				} `xml:"RegistrySearch"`
			} `xml:"Property"`
			Actions []struct {
				ID                                string `xml:"Id,attr"`
				Error, Execute, Return, BinaryKey string `xml:",attr"`
			} `xml:"CustomAction"`
			Sequence struct {
				Actions []struct {
					Action, Sequence, Before, After string `xml:",attr"`
					Condition                       string `xml:",chardata"`
				} `xml:"Custom"`
			} `xml:"InstallExecuteSequence"`
		} `xml:"Product"`
	}
	if err := xml.NewDecoder(io.LimitReader(bytes.NewReader(source), 1<<20)).Decode(&document); err != nil {
		return err
	}
	properties, actions, sequences := 0, 0, 0
	for _, property := range document.Product.Properties {
		if property.ID != "VX_LIFECYCLE_FAIL" {
			continue
		}
		properties++
		if property.Value != "" || len(property.Search) != 1 {
			return errors.New("private fault cannot have an initialized/default failure property")
		}
		search := property.Search[0]
		if search.ID != "VxLifecycleFault" || search.Root != "HKCU" ||
			search.Key != `Software\VXBridgeLifecycleHarness` || search.Name != "FailUpgrade" ||
			search.Type != "raw" || search.Win64 != "yes" {
			return errors.New("private fault search must use only the dedicated test-account hive")
		}
	}
	for _, action := range document.Product.Actions {
		if action.ID == "VxLifecycleExpectedFail" {
			actions++
			if action.Error != lifecycleFaultMarker || action.Execute != "" || action.Return != "" || action.BinaryKey != "" {
				return errors.New("private failure must be an immediate standard Type19 error, not a native bypass")
			}
		}
	}
	for _, action := range document.Product.Sequence.Actions {
		if action.Action == "VxLifecycleExpectedFail" {
			sequences++
			if action.Sequence != "6550" || action.Before != "" || action.After != "" ||
				strings.TrimSpace(action.Condition) != lifecycleFaultCondition {
				return errors.New("private failure must follow actual InstallExecute and exclude REMOVE")
			}
		}
	}
	if properties != 1 || actions != 1 || sequences != 1 {
		return errors.New("private lifecycle failure rows must be exact and complete")
	}
	return nil
}
