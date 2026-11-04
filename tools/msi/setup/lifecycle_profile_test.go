package main

import (
	"strings"
	"testing"
)

func TestPrivateFaultIsAuthoringOnlyAndNormalProfileRejectsIt(t *testing.T) {
	payload := map[string][]byte{}
	for _, name := range files {
		payload[name] = []byte(name)
	}
	normal := author(guid("p"), "0.0.2", strings.Repeat("a", 40), strings.Repeat("b", 64), payload)
	fault := injectLifecycleFault(normal)
	if err := verifyBuildProfile([]byte(normal), normalProfile); err != nil {
		t.Fatal(err)
	}
	if err := verifyBuildProfile([]byte(fault), lifecycleFaultProfile); err != nil {
		t.Fatal(err)
	}
	if verifyBuildProfile([]byte(fault), normalProfile) == nil || verifyBuildProfile([]byte(normal), lifecycleFaultProfile) == nil {
		t.Fatal("private authoring and normal production profile were interchangeable")
	}
	for name, invalid := range map[string]string{
		"before-mutation":   strings.Replace(fault, `Sequence="6550"`, `Sequence="6400"`, 1),
		"uninstall-fault":   strings.Replace(fault, `NOT REMOVE AND VX_LIFECYCLE_FAIL = "1"`, `VX_LIFECYCLE_FAIL`, 1),
		"owned-control-key": strings.Replace(fault, `Software\VXBridgeLifecycleHarness`, `Software\VxInfiniteTeesBridge\Installer`, 1),
		"machine-hive":      strings.Replace(fault, `Root="HKCU" Key="Software\VXBridgeLifecycleHarness"`, `Root="HKLM" Key="Software\VXBridgeLifecycleHarness"`, 1),
		"default-property":  strings.Replace(fault, `<Property Id="VX_LIFECYCLE_FAIL">`, `<Property Id="VX_LIFECYCLE_FAIL" Value="1">`, 1),
		"native-entrypoint": strings.Replace(fault, `Error="`+lifecycleFaultMarker+`"`, `BinaryKey="SetupActions"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyBuildProfile([]byte(invalid), lifecycleFaultProfile); err == nil {
				t.Fatal("private failure contract weakened")
			}
		})
	}
}

func TestPrivateFaultCannotBeAuthoredOutsideManualDisposableScope(t *testing.T) {
	source := strings.Repeat("a", 40)
	values := map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted",
		"GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_WORKFLOW": "VX Bridge disposable lifecycle",
		"GITHUB_RUN_ATTEMPT": "1", "GITHUB_REPOSITORY": "devigned/vx-bridge",
		"REVISION": source, "WORKFLOW_REVISION": source}
	if err := checkProfileBoundary(lifecycleFaultProfile, source, "v0.0.2", func(key string) string { return values[key] }); err != nil {
		t.Fatal(err)
	}
	for key := range values {
		t.Run(key, func(t *testing.T) {
			copy := map[string]string{}
			for key, value := range values {
				copy[key] = value
			}
			copy[key] = "wrong"
			if checkProfileBoundary(lifecycleFaultProfile, source, "v0.0.2", func(key string) string { return copy[key] }) == nil {
				t.Fatal("private fault profile escaped its execution boundary")
			}
		})
	}
	if checkProfileBoundary(lifecycleFaultProfile, source, "development", func(key string) string { return values[key] }) == nil {
		t.Fatal("private failure profile accepted an unrelated production/development version")
	}
}

func TestLifecycleWorkflowHasSeparateRealExecutionBoundary(t *testing.T) {
	workflow := setupSource(t, "../lifecycle/workflow.yml")
	harness := setupSource(t, "../lifecycle/setup-lifecycle-run.ps1")
	worker := setupSource(t, "../lifecycle/worker_windows.go")
	token := setupSource(t, "../lifecycle/context_windows.go")
	for _, value := range []string{"workflow_dispatch:", "contents: read", "cancel-in-progress: false",
		"runs-on: windows-2025", "digest-mismatch: error", "if: ${{ always() }}", "overwrite: false"} {
		if !strings.Contains(workflow, value) {
			t.Fatal("lifecycle workflow lost scope/evidence boundary", value)
		}

	}
	for _, forbidden := range []string{"pull_request:", "\n  push:", "contents: write", "secrets:"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatal("lifecycle workflow gained automatic/untrusted execution", forbidden)
		}
	}
	for _, value := range []string{"New-LocalUser", "S-1-5-32-544", "-Credential $credential -LoadUserProfile",
		"::add-mask::", "Win32_UserProfile", "Win32_ProcessStartTrace"} {
		if !strings.Contains(harness, value) {
			t.Fatal("genuine standard-account/process audit missing", value)
		}
	}
	for _, value := range []string{"GetTokenGroups()", "windows.TokenElevation", "windows.TokenElevationType",
		"u.AdminSIDPresent", "u.SID != c.ExpectedSID", "windows.OpenProcessToken",
		"windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE",
		"token.Environ(false)", "token.KnownFolderPath(", "token.GetUserProfileDirectory()"} {
		if !strings.Contains(token, value) {
			t.Fatal("independent actual token proof missing", value)
		}
		if strings.Contains(token, "windows.KnownFolderPath(") || strings.Contains(token, "GetCurrentProcessToken()") {
			t.Fatal("Known Folder lookup lost its explicit actual-user token")
		}
	}
	for _, value := range []string{"Stop-Process", "TerminateProcess", "taskkill", "Remove-LocalUser", "CommandContext", ".Process.Kill("} {
		if strings.Contains(harness+worker, value) {
			t.Fatal("lifecycle harness hides state by forced cleanup", value)
		}
	}
}

func TestDisposablePolicyAdjustmentStaysExactAndRestored(t *testing.T) {
	policy := setupSource(t, "../lifecycle/setup-lifecycle-policy.ps1")
	harness := setupSource(t, "../lifecycle/setup-lifecycle-run.ps1")
	workflow := setupSource(t, "../lifecycle/workflow.yml")
	for _, required := range []string{"RegistryView]::Registry64", "original_disable_msi_raw_hex",
		"Check-DisableValue $original", "Check-NoElevation $active", "Unrelated-Signature",
		"$key.SetValue($name,0,[Microsoft.Win32.RegistryValueKind]::DWord)",
		"$key.DeleteValue('DisableMSI',$false)", "@($after.entries).Count -eq 0",
		"@($after.subkeys).Count -eq 0", "if (-not $before.key_exists",
		"(Signature $after) -cne (Signature $before)", "restoration-failed"} {
		if !strings.Contains(policy, required) {
			t.Fatal("policy ownership/restoration guard missing", required)
		}

	}
	for _, forbidden := range []string{"DeleteSubKeyTree", "Remove-Item", "Set-ItemProperty", "AlwaysInstallElevated',1", "RegistryHive]::CurrentUser"} {
		if strings.Contains(policy, forbidden) {
			t.Fatal("policy scope broadened", forbidden)
		}
	}
	if !strings.Contains(harness, "SetAccessRuleProtection($true,$false)") ||
		!strings.Contains(harness, "& $policyScript -Mode Restore") ||
		!strings.Contains(harness, "policy_restoration_failure") ||
		!strings.Contains(workflow, "Always verify exact original disposable-machine policy restoration") {
		t.Fatal("protected policy record/finally/always restoration boundary missing")
	}
}
