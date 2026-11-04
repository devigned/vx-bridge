package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfiguration(t *testing.T) (configuration, map[string]string) {
	t.Helper()
	source := strings.Repeat("a", 40)
	dir := t.TempDir()
	c := configuration{Schema: 1, Approval: approval, Source: source, Workflow: source, Run: "12345", Attempt: "1",
		ExpectedSID: "S-1-5-21-1-2-3-1002", HarnessSID: "S-1-5-21-1-2-3-1001", Account: "invented-standard-user",
		Evidence: filepath.Join(dir, "evidence")}
	for index, role := range []string{"a", "b", "b-fault"} {
		version, product, profile := "v0.0.2", "{22222222-2222-4222-8222-222222222222}", normalTestProfile(role)
		if index == 0 {
			version, product = "v0.0.1", "{11111111-1111-4111-8111-111111111111}"
		}
		payload := map[string]string{}
		for _, name := range payloadNames {
			payload[name] = strings.Repeat("8", 64)
		}
		binaryHash := strings.Repeat("a", 64)
		if index == 0 {
			binaryHash = strings.Repeat("9", 64)
		}
		payload["GSPconnect.exe"], payload["vxbridge.exe"] = binaryHash, binaryHash
		packages := []string{"{AAAAAAAA-1111-4111-8111-111111111111}", "{BBBBBBBB-2222-4222-8222-222222222222}", "{CCCCCCCC-3333-4333-8333-333333333333}"}
		archiveHash := strings.Repeat("4", 64)
		if index == 0 {
			archiveHash = strings.Repeat("3", 64)
		}
		c.Media = append(c.Media, media{Role: role, Source: source, Version: version, MSIVersion: strings.TrimPrefix(version, "v"),
			Product: product, Package: packages[index], Profile: profile, MSI: filepath.Join(dir, role, "bridge.msi"),
			MSISHA256: strings.Repeat([]string{"5", "6", "7"}[index], 64), Setup: filepath.Join(dir, role, "setup.exe"), SetupSHA256: strings.Repeat("1", 64),
			ActionsSHA256: strings.Repeat("2", 64), Archive: filepath.Join(dir, role, "payload.zip"), ArchiveSHA256: archiveHash, Payload: payload})
		c.Media[len(c.Media)-1].Components = map[string]string{}
		for i, id := range []string{"VxGUI", "VxControl", "VxLicense", "VxNotices", "VxManifest", "VxSums", "Shortcut", "Maintenance"} {
			c.Media[len(c.Media)-1].Components[id] = fmt.Sprintf("{%08X-1111-4111-8111-111111111111}", i+1)
		}
	}
	env := map[string]string{"REVISION": source, "WORKFLOW_REVISION": source, "GITHUB_RUN_ID": c.Run,
		"GITHUB_RUN_ATTEMPT": "1", "GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted",
		"GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_WORKFLOW": "VX Bridge disposable lifecycle"}
	env["GITHUB_REPOSITORY"] = "devigned/vx-bridge"
	return c, env
}

func normalTestProfile(role string) string {
	if role == "b-fault" {
		return "private-lifecycle-fault"
	}
	return "normal"
}

func TestExactDisposableExecutionIdentity(t *testing.T) {
	c, env := testConfiguration(t)
	get := func(key string) string { return env[key] }
	if err := validate(c, c.Source, get); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"GITHUB_ACTIONS", "RUNNER_ENVIRONMENT", "GITHUB_EVENT_NAME", "GITHUB_WORKFLOW",
		"REVISION", "WORKFLOW_REVISION", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_REPOSITORY", "DIAGNOSTIC_LANE"} {
		t.Run(field, func(t *testing.T) {
			c, env := testConfiguration(t)
			env[field] = "invented-wrong-context"
			if err := validate(c, c.Source, func(key string) string { return env[key] }); err == nil {
				t.Fatal("execution admitted outside the exact disposable hosted context")
			}
		})
	}
}

func TestRealVersionCoreAndProfileGuards(t *testing.T) {
	for name, mutate := range map[string]func(*configuration){
		"harness-account":        func(c *configuration) { c.ExpectedSID = c.HarnessSID },
		"retry":                  func(c *configuration) { c.Attempt = "2" },
		"relabelled-version":     func(c *configuration) { c.Media[1].Version = "v0.0.1" },
		"changed-native-owner":   func(c *configuration) { c.Media[1].SetupSHA256 = strings.Repeat("f", 64) },
		"changed-native-actions": func(c *configuration) { c.Media[1].ActionsSHA256 = strings.Repeat("f", 64) },
		"reused-package":         func(c *configuration) { c.Media[2].Package = c.Media[1].Package },
		"reused-msi-bytes":       func(c *configuration) { c.Media[2].MSISHA256 = c.Media[1].MSISHA256 },
		"fault-normal-profile":   func(c *configuration) { c.Media[2].Profile = "normal" },
		"fault-changes-payload":  func(c *configuration) { c.Media[2].Payload["LICENSE"] = strings.Repeat("f", 64) },
		"same-app-bits":          func(c *configuration) { c.Media[1].Payload["GSPconnect.exe"] = c.Media[0].Payload["GSPconnect.exe"] },
		"fault-changes-archive":  func(c *configuration) { c.Media[2].ArchiveSHA256 = strings.Repeat("f", 64) },
		"fault-other-product":    func(c *configuration) { c.Media[2].Product = c.Media[0].Product },
		"changed-component":      func(c *configuration) { c.Media[2].Components["VxGUI"] = "{99999999-1111-4111-8111-111111111111}" },
	} {
		t.Run(name, func(t *testing.T) {
			c, env := testConfiguration(t)
			mutate(&c)
			if err := validate(c, c.Source, func(key string) string { return env[key] }); err == nil {
				t.Fatal("unsafe/synthetic lifecycle configuration admitted")
			}

		})
	}
}

func TestRetiredComparisonConfigurationIsNotReinterpretedAsFullLifecycle(t *testing.T) {
	c, _ := testConfiguration(t)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "retired-config.json")
	data = append(data[:len(data)-1], []byte(`,"diagnostic_lane":""}`)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(path, 0); err == nil || !strings.Contains(err.Error(), `unknown field "diagnostic_lane"`) {
		t.Fatal("retired config was not refused before authorization/execution:", err)
	}
}
