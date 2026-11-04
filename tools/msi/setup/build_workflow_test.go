package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func checkRoutineWorkflowLayout(root string) error {
	directory := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	routine := map[string]bool{"ci.yml": false, "release.yml": false}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("workflow must be a regular file: %s", entry.Name())
		}
		if _, ok := routine[entry.Name()]; ok {
			routine[entry.Name()] = true
			continue
		}
		if entry.Name() != "setup-lifecycle.yml" {
			return fmt.Errorf("unexpected active workflow: %s", entry.Name())
		}
		active, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return err
		}
		parked, err := os.ReadFile(filepath.Join(root, "tools", "msi", "lifecycle", "workflow.yml"))
		if err != nil {
			return err
		}
		if !bytes.Equal(active, parked) {
			return errors.New("temporary qualification must be an exact copy of the parked template")
		}
	}
	for name, found := range routine {
		if !found {
			return fmt.Errorf("missing routine workflow: %s", name)
		}
	}
	return nil
}

func TestPublicRepositoryAndArtifactIdentity(t *testing.T) {
	for path, required := range map[string][]string{
		"../../../go.mod": {
			"module github.com/devigned/vx-bridge\n",
		},
		"../../../tools/dist/main.go": {
			`const modulePath = "github.com/devigned/vx-bridge"`,
			`return "vx-bridge-windows-amd64-"`,
		},
		"../../../.github/workflows/ci.yml": {
			"name: vx-bridge-ci-input-${{ github.sha }}-${{ github.run_id }}",
			"name: vx-bridge-ci-authoring-${{ github.sha }}-${{ github.run_id }}",
		},
		"../../../.github/workflows/release.yml": {
			"name: vx-bridge-tag-input-${{ steps.source.outputs.revision }}-${{ github.run_id }}",
			"name: vx-bridge-tag-assets-${{ needs.inputs.outputs.version }}-${{ needs.inputs.outputs.revision }}",
			"name: vx-bridge-tag-authoring-${{ needs.inputs.outputs.revision }}-${{ github.run_id }}",
		},
		"../../../.github/scripts/publish-tag-release.sh": {
			`--arg name "vx-bridge-tag-assets-$RELEASE_VERSION-$SOURCE_REVISION"`,
			`installer="vx-bridge-installer-windows-amd64-$RELEASE_VERSION.exe"`,
		},
		"../lifecycle/workflow.yml": {
			"github.repository == 'devigned/vx-bridge' && github.event.repository.private && !github.event.repository.fork",
		},
	} {
		source := setupSource(t, path)
		for _, text := range required {
			if !strings.Contains(source, text) {
				t.Fatalf("%s: missing public naming or private-only qualification guard %q", path, text)
			}
		}
	}
}

func TestRoutineWorkflowLayoutAndStrictActivation(t *testing.T) {
	const template = "name: parked fixture\non: workflow_dispatch\n"
	for _, test := range []struct {
		name    string
		extra   map[string]string
		missing string
		want    bool
	}{
		{name: "parked", want: true},
		{name: "exact_activation", extra: map[string]string{"setup-lifecycle.yml": template}, want: true},
		{name: "modified_activation", extra: map[string]string{"setup-lifecycle.yml": template + "# changed\n"}},
		{name: "wrong_name", extra: map[string]string{"qualification.yml": template}},
		{name: "extra_fourth", extra: map[string]string{"setup-lifecycle.yml": template, "extra.yml": template}},
		{name: "stray_yaml", extra: map[string]string{"extra.yaml": template}},
		{name: "yaml_activation", extra: map[string]string{"setup-lifecycle.yaml": template}},
		{name: "yaml_fourth", extra: map[string]string{"setup-lifecycle.yml": template, "extra.yaml": template}},
		{name: "renamed_routine", missing: "ci.yml", extra: map[string]string{"ci.yaml": template}},
		{name: "missing_routine", missing: "release.yml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, ".github", "workflows")
			parked := filepath.Join(root, "tools", "msi", "lifecycle", "workflow.yml")
			for _, path := range []string{directory, filepath.Dir(parked)} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(parked, []byte(template), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"ci.yml", "release.yml"} {
				if name != test.missing {
					if err := os.WriteFile(filepath.Join(directory, name), []byte("routine fixture\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			for name, data := range test.extra {
				if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := checkRoutineWorkflowLayout(root); (err == nil) != test.want {
				t.Fatalf("layout accepted=%t want=%t: %v", err == nil, test.want, err)
			}
		})
	}
}

func TestPinnedCompilerInstallExecutesWithHarmlessTools(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("Bash is required to exercise the actual embedded build step", err)
	}
	script := setupSource(t, "../../../.github/scripts/install-setup-toolchain.sh")
	manifest := setupSource(t, "../../../installer/toolchain.json")
	var pins struct {
		Image    string `json:"cross_build_image"`
		Compiler string `json:"cross_compiler_package"`
		Binutils string `json:"cross_binutils_package"`
		Headers  string `json:"cross_headers_package"`
	}
	if err := json.Unmarshal([]byte(manifest), &pins); err != nil {
		t.Fatal(err)
	}
	const harmlessTools = `
jq() {
  [[ "$1" == -er && "$3" == installer/toolchain.json && -f "$3" ]] || return 90
  [[ "$FIXTURE_JQ_FAIL" == 0 ]] || return 91
  case "$2" in
    .cross_build_image) printf '%s\n' "$FIXTURE_IMAGE";;
    .cross_compiler_package) printf '%s\n' "$FIXTURE_COMPILER";;
    .cross_binutils_package) printf '%s\n' "$FIXTURE_BINUTILS";;
    .cross_headers_package) printf '%s\n' "$FIXTURE_HEADERS";;
    *) return 92;;
  esac
}
apt-get() {
  [[ "$DEBIAN_FRONTEND" == noninteractive ]] || return 93
  printf '%s\n' "$@" >> apt.calls
}
`
	for _, ending := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		t.Run(ending.name, func(t *testing.T) {
			snippet := strings.ReplaceAll(strings.ReplaceAll(script, "\n", ending.value), "\r\n", "\n")
			for _, field := range []string{"", "IMAGE", "COMPILER", "BINUTILS", "HEADERS", "JQ_FAIL"} {
				name := field
				if name == "" {
					name = "approved-pins"
				}
				t.Run(name, func(t *testing.T) {
					dir := t.TempDir()
					emptyPath := filepath.Join(dir, "no-external-tools")
					if err := os.Mkdir(emptyPath, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dir, "installer"), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "installer", "toolchain.json"), []byte(manifest), 0o600); err != nil {
						t.Fatal(err)
					}
					values := map[string]string{
						"IMAGE": pins.Image, "COMPILER": pins.Compiler, "BINUTILS": pins.Binutils,
						"HEADERS": pins.Headers, "JQ_FAIL": "0",
					}
					if field != "" {
						values[field] = "invented-mismatch"
					}
					cmd := exec.Command(bash, "--noprofile", "--norc", "-euo", "pipefail", "-c", harmlessTools+snippet)
					cmd.Dir = dir
					cmd.Env = append(os.Environ(), "PATH="+emptyPath, "BASH_ENV=", "ENV=")
					for key, value := range values {
						cmd.Env = append(cmd.Env, "FIXTURE_"+key+"="+value)
					}
					output, runErr := cmd.CombinedOutput()
					calls, callErr := os.ReadFile(filepath.Join(dir, "apt.calls"))
					if field != "" {
						if runErr == nil || !errors.Is(callErr, os.ErrNotExist) {
							t.Fatalf("mismatched/unreadable pins reached fake install: run=%v calls=%q read=%v output=%s",
								runErr, calls, callErr, output)
						}
						return
					}
					if runErr != nil || callErr != nil {
						t.Fatalf("approved embedded step failed: run=%v read=%v output=%s", runErr, callErr, output)
					}
					want := strings.Join([]string{"install", "-y", "-qq", "--no-install-recommends",
						pins.Compiler, pins.Binutils, pins.Headers, ""}, "\n")
					if string(calls) != want {
						t.Fatalf("unexpected fake install arguments: %q", calls)
					}
				})
			}
		})
	}
}

func TestMinimalWorkflowsShareExactBuildAndReadOnlyBoundaries(t *testing.T) {
	ci := setupSource(t, "../../../.github/workflows/ci.yml")
	release := setupSource(t, "../../../.github/workflows/release.yml")
	input := setupSource(t, "../../../.github/scripts/setup-build-input.sh")
	windows := setupSource(t, "../../../.github/scripts/setup-build-only.ps1")
	builder := setupSource(t, "../../../installer/build-setup.ps1")
	if err := checkRoutineWorkflowLayout("../../.."); err != nil {
		t.Fatal("routine layout or exact temporary qualification copy required", err)
	}
	if _, err := os.Stat("../lifecycle/workflow.yml"); err != nil {
		t.Fatal("retain the exceptional guarded lifecycle as a parked template", err)
	}

	step := strings.Index(ci, "      - name: Retain built application/installer bytes, authoring outcome and logs")
	if step < 0 || strings.Count(ci, "actions/upload-artifact@") != 2 {
		t.Fatal("retain actual bytes in the existing evidence artifact, not an extra artifact/workflow")
	}
	artifact := ci[step:]
	for _, required := range []string{"if: ${{ always() }}", "name: vx-bridge-ci-authoring-",
		"${{ runner.temp }}/release-artifacts/*", "${{ runner.temp }}/setup-build-evidence/*",
		"${{ runner.temp }}/native-tool-tests/*", "retention-days: 7", "overwrite: false", "compression-level: 0"} {
		if !strings.Contains(artifact, required) {
			t.Fatal("CI lost bounded actual asset/log evidence", required)
		}
	}
	for _, workflow := range []string{ci, release} {
		for _, required := range []string{"contents: read", "runs-on: windows-2025",
			"GIT_CONFIG_KEY_1: safe.directory", "GIT_CONFIG_VALUE_1: ${{ github.workspace }}",
			"artifact-ids: ${{ needs.inputs.outputs.artifact-id }}", "digest-mismatch: error",
			"if: ${{ always() }}", "overwrite: false", "build-setup-artifacts",
			"make install-setup-toolchain build-setup-input"} {
			if !strings.Contains(workflow, required) {
				t.Fatal("shared build lost a scope/identity boundary", required)
			}
		}
	}
	for _, forbidden := range []string{"contents: write", "secrets:", "workflow_dispatch:"} {
		if strings.Contains(ci, forbidden) {
			t.Fatal("CI gained publication/manual scope", forbidden)
		}
	}
	if !strings.Contains(ci, "pull_request:") || !strings.Contains(ci, "branches: [main]") ||
		!strings.Contains(ci, "run: make ci\n") || !strings.Contains(ci, "make ci-windows build-setup-artifacts") ||
		!strings.Contains(release, "tags: ['v*']") || strings.Count(release, "contents: write") != 1 ||
		strings.Contains(release[:strings.Index(release, "\n  publish:")], "contents: write") {
		t.Fatal("CI/tag triggers or publication-only write permission differs")
	}
	for _, forbidden := range []string{"pull_request_target:", "workflow_run:", "environment:", "secrets:", "workflow_dispatch:"} {
		if strings.Contains(ci+release, forbidden) {
			t.Fatal("CI/release gained an untrusted or invented execution gate", forbidden)
		}
	}
	pins := map[string]string{
		"actions/checkout":          "3d3c42e5aac5ba805825da76410c181273ba90b1",
		"actions/setup-go":          "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"actions/upload-artifact":   "cf430e030ddbb5b0abf93d22962f4752f3646cd9",
		"actions/download-artifact": "9000827ccba6bdab643e8b6fd33ac0654aef8333",
	}
	for _, action := range regexp.MustCompile(`uses: ([^@\s]+)@([^\s]+)`).FindAllStringSubmatch(ci+release, -1) {
		if want, ok := pins[action[1]]; !ok || action[2] != want {
			t.Fatal("build workflow changed reviewed action pins", action[1], action[2])
		}
	}
	for _, required := range []string{"make build-setup-native", "make package", "make prepare-setup",
		"git show", "INPUT-SHA256SUMS", `"$count" == 13`, "bundle-stub.exe"} {
		if !strings.Contains(input, required) {
			t.Fatal("native/input build lost authoritative provenance", required)
		}
	}
	for _, required := range []string{"sourceHashes.Count -ne 13", "input_artifact_digest", "bundle-stub.exe",
		"'prepare-setup'", "'build-setup-msi'", "Windows regeneration differs",
		"native-tools.log", "outcome.json", "throw", "'package-installer'", "Language.Parser]::ParseFile"} {
		if !strings.Contains(windows, required) {
			t.Fatal("Windows build lost input/evidence verification", required)
		}
	}
	for _, code := range []string{ci, release, input, windows, builder} {
		for _, forbidden := range []string{"MsiInstallProduct", "MsiDoAction", ".InstallProduct(", "msiexec",
			"Start-Process", "test-private-msi-session", "test-outer-msi-rollback"} {
			if strings.Contains(code, forbidden) {
				t.Fatal("build-only path gained execution", forbidden)
			}
		}
		if regexp.MustCompile(`(?im)^\s*&\s+.*(?:GSPconnect|vxbridge|setup|private-[^\s]*)\.exe`).MatchString(code) {
			t.Fatal("build path invokes a product/private native executable")
		}
	}
	for _, required := range []string{"'-wx'", "'-sice:ICE91'", "SELECT `Component`",
		"FROM `Registry`", "FROM `File`", "FROM `Media`", "FROM `AdvtExecuteSequence`",
		"Actual feature must prohibit advertising.", "Extracted payload differs",
		"Actual Binary stream differs", "Actual MSI summary differs"} {
		if !strings.Contains(builder, required) {
			t.Fatal("actual MSI author/static verification weakened", required)
		}
	}
	iceFlags := regexp.MustCompile(`'-sice:[^']*'`).FindAllString(builder, -1)
	if strings.Contains(builder, "-sval") || strings.Join(iceFlags, "|") != "'-sice:ICE91'|'-sice:ICE96'" {
		t.Fatal("blanket/additional MSI validation suppression")
	}
	warningFlags := regexp.MustCompile(`'-sw[^']*'`).FindAllString(builder, -1)
	if len(warningFlags) != 1 || warningFlags[0] != "'-sw1008'" || strings.Contains(builder, "-swall") ||
		strings.Contains(builder, "-sadv") || !strings.Contains(builder, "'-verify-authoring'") ||
		!strings.Contains(builder, "Intentionally omitted advertise action was emitted.") {
		t.Fatal("scoped advertise diagnostic exception lost its exact source/emitted-table gates")
	}
}
