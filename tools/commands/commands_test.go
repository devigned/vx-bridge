package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type invocation struct {
	tool, toolchain, cgo string
	args                 []string
}

func TestMakeCommands(t *testing.T) {
	makefile, err := filepath.Abs("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	shell := os.Getenv("MAKE_TEST_SHELL")
	if shell == "" {
		shell = "bash"
	}
	goCall := func(cgo string, args ...string) invocation {
		return invocation{"go", "local", cgo, args}
	}
	gitCall := func(args ...string) invocation {
		return invocation{"git", "local", "0", args}
	}
	formatCall := func(flag string) invocation {
		return invocation{"gofmt", "local", "0", []string{flag, "tracked file.go"}}
	}
	packageCalls := []invocation{
		gitCall("rev-parse", "HEAD"),
		goCall("0", "mod", "download"),
		goCall("0", "mod", "verify"),
		goCall("0", "run", "-mod=readonly", "./tools/dist", "-revision", sha, "-version", "development", "-out", "external path with spaces"),
	}
	benchmarkCall := goCall("0", "test", "-mod=readonly", "-run=^$",
		"-bench=Benchmark(SourceFrameInspect|SplitMerge|ReceiptFrameResponse|ForwardReceiptPipe|IdleTick)$",
		"-benchmem", "-benchtime=300ms", "-count=6", "-cpu=1", "-timeout=5m",
		"./internal/protocol", "./internal/bridge")
	for _, test := range []struct {
		name, target string
		env          map[string]string
		want         []invocation
		fail         bool
		message      string
		pipeline     bool
	}{
		{name: "default_help", message: "Project commands"},
		{name: "help", target: "help", message: "OUT must be a new absolute directory"},
		{name: "installer_help", target: "help-installer", message: "static authoring/integrity only"},
		{name: "qualification_help", target: "help-qualification", message: "private-repository gate intentionally prevents execution in this public home"},
		{name: "test", target: "test", want: []invocation{
			goCall("0", "test", "-mod=readonly", "-count=1", "-timeout=90s", "./..."),
		}},
		{name: "race_overrides_cgo", target: "test-race", want: []invocation{
			goCall("1", "test", "-mod=readonly", "-race", "-count=1", "-timeout=90s", "./..."),
		}},
		{name: "vet", target: "vet", want: []invocation{goCall("0", "vet", "-mod=readonly", "./...")}},
		{name: "format", target: "fmt", want: []invocation{gitCall("ls-files", "-z", "*.go"), formatCall("-w")}},
		{name: "format_check", target: "fmt-check", want: []invocation{gitCall("ls-files", "-z", "*.go"), formatCall("-l")}},
		{name: "format_required", target: "fmt-check", env: map[string]string{"UNFORMATTED": "tracked file.go"},
			want: []invocation{gitCall("ls-files", "-z", "*.go"), formatCall("-l")}, fail: true, message: "Run make fmt"},
		{name: "format_tool_failure", target: "fmt-check", env: map[string]string{"FAIL_TOOL": "gofmt"},
			want: []invocation{gitCall("ls-files", "-z", "*.go"), formatCall("-l")}, fail: true, message: "invented failure"},
		{name: "format_write_failure", target: "fmt", env: map[string]string{"FAIL_TOOL": "gofmt"},
			want: []invocation{gitCall("ls-files", "-z", "*.go"), formatCall("-w")}, fail: true, message: "invented failure"},
		{name: "file_listing_failure", target: "fmt-check", env: map[string]string{"FAIL_TOOL": "git"},
			fail: true, message: "invented failure"},
		{name: "combined_check", target: "check", want: []invocation{
			gitCall("ls-files", "-z", "*.go"), formatCall("-l"),
			goCall("0", "test", "-mod=readonly", "-count=1", "-timeout=90s", "./..."),
			goCall("0", "vet", "-mod=readonly", "./..."),
		}},
		{name: "portable_ci", target: "ci", want: []invocation{
			gitCall("ls-files", "-z", "*.go"), formatCall("-l"),
			goCall("0", "test", "-mod=readonly", "-count=1", "-timeout=90s", "./..."),
			goCall("0", "vet", "-mod=readonly", "./..."),
			goCall("1", "test", "-mod=readonly", "-race", "-count=1", "-timeout=90s", "./..."),
			goCall("0", "run", "github.com/rhysd/actionlint/cmd/actionlint@v1.7.12", "-color",
				".github/workflows/ci.yml", ".github/workflows/release.yml", "tools/msi/lifecycle/workflow.yml"),
		}},
		{name: "windows_ci", target: "ci-windows", env: map[string]string{"OUT": "native tests with spaces"}, want: []invocation{
			goCall("0", "test", "-mod=readonly", "-count=1", "-timeout=90s", "./..."),
			goCall("0", "vet", "-mod=readonly", "./..."),
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "installer/test-native-tools.ps1", "-Out", "native tests with spaces"}},
		}},
		{name: "native_tool_regression", target: "test-native-tools", env: map[string]string{"OUT": "native tests with spaces"}, want: []invocation{
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "installer/test-native-tools.ps1", "-Out", "native tests with spaces"}},
		}},
		{name: "native_tool_requires_output", target: "test-native-tools", fail: true, message: "Set OUT"},
		{name: "native_tool_propagates_failure", target: "test-native-tools",
			env: map[string]string{"OUT": "native tests with spaces", "FAIL_TOOL": "pwsh"}, fail: true, message: "invented failure",
			want: []invocation{{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "installer/test-native-tools.ps1", "-Out", "native tests with spaces"}}}},
		{name: "check_stops_on_format_failure", target: "check", env: map[string]string{"UNFORMATTED": "tracked file.go"},
			want: []invocation{gitCall("ls-files", "-z", "*.go"), formatCall("-l")}, fail: true, message: "Run make fmt"},
		{name: "build_alias", target: "build", env: map[string]string{"OUT": "external path with spaces"}, want: packageCalls},
		{name: "package", target: "package", env: map[string]string{"OUT": "external path with spaces"}, want: packageCalls},
		{name: "package_explicit_revision", target: "package", env: map[string]string{"OUT": "external path with spaces", "REVISION": sha},
			want: packageCalls[1:]},
		{name: "tagged_package", target: "package", env: map[string]string{"OUT": "external path with spaces", "RELEASE_VERSION": "v1.0.1"},
			want: []invocation{packageCalls[0], packageCalls[1], packageCalls[2],
				goCall("0", "run", "-mod=readonly", "./tools/dist", "-revision", sha, "-version", "v1.0.1", "-out", "external path with spaces")}},
		{name: "package_requires_output", target: "package", fail: true, message: "Set OUT"},
		{name: "package_module_failure", target: "package", env: map[string]string{"OUT": "external path with spaces", "FAIL_COMMAND": "mod verify"},
			want: packageCalls[:3], fail: true, message: "invented failure"},
		{name: "package_helper_failure", target: "package", env: map[string]string{"OUT": "external path with spaces", "FAIL_COMMAND": "run"},
			want: packageCalls, fail: true, message: "invented failure"},
		{name: "verify", target: "verify-package", env: map[string]string{"ARCHIVE": "external archive with spaces.zip"}, want: []invocation{
			gitCall("rev-parse", "HEAD"),
			goCall("0", "run", "-mod=readonly", "./tools/dist", "-revision", sha, "-version", "development", "-verify", "external archive with spaces.zip"),
		}},
		{name: "tagged_verify", target: "verify-package", env: map[string]string{"ARCHIVE": "tagged archive with spaces.zip", "REVISION": sha, "RELEASE_VERSION": "v1.0.1"},
			want: []invocation{goCall("0", "run", "-mod=readonly", "./tools/dist", "-revision", sha, "-version", "v1.0.1", "-verify", "tagged archive with spaces.zip")}},
		{name: "verify_requires_archive", target: "verify-package", fail: true, message: "Set ARCHIVE"},
		{name: "setup_native_build", target: "build-setup-native", env: map[string]string{"REVISION": sha},
			want: []invocation{{"build-setup-native.sh", "local", "0", nil}}},
		{name: "setup_prepare", target: "prepare-setup",
			env: map[string]string{"REVISION": sha, "NATIVE_DIR": "native directory with spaces",
				"ARCHIVE": "verified archive with spaces.zip", "OUT": "setup staging with spaces"},
			want: []invocation{goCall("0", "run", "-mod=readonly", "./tools/msi/setup", "-revision", sha,
				"-version", "development", "-archive", "verified archive with spaces.zip",
				"-native", "native directory with spaces", "-out", "setup staging with spaces", "-profile", "normal")}},
		{name: "setup_prepare_requires_native", target: "prepare-setup", fail: true, message: "Set NATIVE_DIR"},
		{name: "setup_prepare_requires_archive", target: "prepare-setup",
			env: map[string]string{"NATIVE_DIR": "native"}, fail: true, message: "Set ARCHIVE"},
		{name: "setup_prepare_requires_output", target: "prepare-setup",
			env: map[string]string{"NATIVE_DIR": "native", "ARCHIVE": "archive.zip"}, fail: true, message: "Set OUT"},
		{name: "setup_prepare_propagates_failure", target: "prepare-setup",
			env: map[string]string{"REVISION": sha, "NATIVE_DIR": "native", "ARCHIVE": "archive.zip",
				"OUT": "staging", "FAIL_TOOL": "go"}, fail: true, message: "invented failure",
			want: []invocation{goCall("0", "run", "-mod=readonly", "./tools/msi/setup", "-revision", sha,
				"-version", "development", "-archive", "archive.zip", "-native", "native", "-out", "staging", "-profile", "normal")}},
		{name: "setup_msi_author_only", target: "build-setup-msi",
			env: map[string]string{"SETUP_STAGING": "staging with spaces", "WIX_ARCHIVE": "approved wix.zip",
				"TOOL_LOG": "native tool log.txt", "OUT": "new output with spaces"},
			want: []invocation{{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "installer/build-setup.ps1",
				"-Profile", "normal",
				"-Staging", "staging with spaces", "-Out", "new output with spaces",
				"-WixArchive", "approved wix.zip", "-ToolLog", "native tool log.txt"}}}},
		{name: "setup_msi_requires_staging", target: "build-setup-msi", fail: true, message: "Set SETUP_STAGING"},
		{name: "setup_msi_requires_wix", target: "build-setup-msi",
			env: map[string]string{"SETUP_STAGING": "staging"}, fail: true, message: "Set WIX_ARCHIVE"},
		{name: "setup_msi_requires_log", target: "build-setup-msi",
			env:  map[string]string{"SETUP_STAGING": "staging", "WIX_ARCHIVE": "approved wix.zip"},
			fail: true, message: "Set TOOL_LOG"},
		{name: "setup_msi_requires_output", target: "build-setup-msi",
			env:  map[string]string{"SETUP_STAGING": "staging", "WIX_ARCHIVE": "approved wix.zip", "TOOL_LOG": "tools.log"},
			fail: true, message: "Set OUT"},
		{name: "benchmark", target: "benchmark", want: []invocation{benchmarkCall}},
		{name: "benchmark_fixed_recipe", target: "benchmark", env: map[string]string{"CGO_ENABLED": "1", "BENCHMARKS": "WrongSelector"},
			want: []invocation{benchmarkCall}},
		{name: "benchmark_failure", target: "benchmark", env: map[string]string{"FAIL_TOOL": "go"},
			want: []invocation{benchmarkCall}, fail: true, message: "invented failure"},
		{name: "piped_benchmark_failure", target: "benchmark", env: map[string]string{"FAIL_TOOL": "go"},
			want: []invocation{benchmarkCall}, fail: true, message: "invented failure", pipeline: true},
		{name: "lint", target: "lint-workflows", want: []invocation{goCall("0", "run", "github.com/rhysd/actionlint/cmd/actionlint@v1.7.12", "-color",
			".github/workflows/ci.yml", ".github/workflows/release.yml", "tools/msi/lifecycle/workflow.yml")}},
		{name: "command_checks", target: "check-commands", want: []invocation{
			goCall("0", "test", "-mod=readonly", "-count=1", "-timeout=90s", "./tools/commands"),
		}},
		{name: "setup_toolchain", target: "install-setup-toolchain", want: []invocation{{"install-setup-toolchain.sh", "local", "0", nil}}},
		{name: "setup_input", target: "build-setup-input", want: []invocation{{"setup-build-input.sh", "local", "0", nil}}},
		{name: "setup_artifacts", target: "build-setup-artifacts", want: []invocation{
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", ".github/scripts/setup-build-only.ps1"}},
		}},
		{name: "installer_package", target: "package-installer",
			env: map[string]string{"REVISION": sha, "AUTHORED_SETUP": "authored with spaces", "ARCHIVE": "app.zip",
				"MINGW_COPYRIGHT": "compiler notice", "OUT": "output with spaces"},
			want: []invocation{goCall("0", "run", "-mod=readonly", "./tools/msi/setup", "-revision", sha,
				"-version", "development", "-archive", "app.zip", "-installer", "authored with spaces",
				"-copyright", "compiler notice", "-out", "output with spaces")}},
		{name: "installer_requires_authoring", target: "package-installer", fail: true, message: "Set AUTHORED_SETUP"},
		{name: "installer_requires_app", target: "package-installer",
			env: map[string]string{"AUTHORED_SETUP": "authored"}, fail: true, message: "Set ARCHIVE"},
		{name: "installer_requires_copyright", target: "package-installer",
			env: map[string]string{"AUTHORED_SETUP": "authored", "ARCHIVE": "app.zip"}, fail: true, message: "Set MINGW_COPYRIGHT"},
		{name: "installer_requires_output", target: "package-installer",
			env: map[string]string{"AUTHORED_SETUP": "authored", "ARCHIVE": "app.zip", "MINGW_COPYRIGHT": "notice"}, fail: true, message: "Set OUT"},
		{name: "installer_verify", target: "verify-installer",
			env: map[string]string{"REVISION": sha, "ARCHIVE": "app.zip", "INSTALLER_ARCHIVE": "installer with spaces.zip"},
			want: []invocation{goCall("0", "run", "-mod=readonly", "./tools/msi/setup", "-revision", sha,
				"-version", "development", "-archive", "app.zip", "-verify-installer", "installer with spaces.zip")}},
		{name: "installer_verify_requires_archive", target: "verify-installer", fail: true, message: "Set ARCHIVE"},
		{name: "installer_verify_requires_installer", target: "verify-installer",
			env: map[string]string{"ARCHIVE": "app.zip"}, fail: true, message: "Set INSTALLER_ARCHIVE"},
		{name: "lifecycle_input", target: "build-lifecycle-input", want: []invocation{{"setup-lifecycle-input.sh", "local", "0", nil}}},
		{name: "lifecycle_author", target: "author-lifecycle", want: []invocation{
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "tools/msi/lifecycle/check-lifecycle-scripts.ps1"}},
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "tools/msi/lifecycle/setup-lifecycle-author.ps1"}},
		}},
		{name: "lifecycle_run", target: "test-installer-lifecycle", want: []invocation{
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "tools/msi/lifecycle/check-lifecycle-scripts.ps1"}},
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "tools/msi/lifecycle/setup-lifecycle-run.ps1"}},
		}},
		{name: "lifecycle_restore", target: "restore-lifecycle-policy", want: []invocation{
			{"pwsh", "local", "0", []string{"-NoLogo", "-NoProfile", "-File", "tools/msi/lifecycle/setup-lifecycle-policy.ps1", "-Mode", "Restore"}},
		}},
		{name: "release_resolve", target: "release-resolve", want: []invocation{{"check-tag-release.sh", "local", "0", []string{"resolve"}}}},
		{name: "release_verify", target: "release-verify", want: []invocation{{"check-tag-release.sh", "local", "0", []string{"verify"}}}},
		{name: "release_publish", target: "release-publish", want: []invocation{{"publish-tag-release.sh", "local", "0", nil}}},
		{name: "release_gate_failure", target: "release-resolve", env: map[string]string{"FAIL_TOOL": "check-tag-release.sh"},
			want: []invocation{{"check-tag-release.sh", "local", "0", []string{"resolve"}}}, fail: true, message: "invented failure"},
		{name: "release_publication_failure", target: "release-publish", env: map[string]string{"FAIL_TOOL": "publish-tag-release.sh"},
			want: []invocation{{"publish-tag-release.sh", "local", "0", nil}}, fail: true, message: "invented failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			stubs := filepath.Join(root, "tools with spaces")
			if err := os.Mkdir(stubs, 0o755); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(root, "commands.txt")
			for _, name := range []string{"go", "git", "gofmt", "check-tag-release.sh", "publish-tag-release.sh",
				"build-setup-native.sh", "install-setup-toolchain.sh", "setup-build-input.sh", "setup-lifecycle-input.sh", "pwsh"} {
				script := `#!/usr/bin/env bash
set -eu
tool="${0##*/}"
if [[ "$tool" == git && "${FAIL_TOOL-}" == git ]]; then
  echo 'invented failure' >&2
  exit 37
fi
if [[ "$tool" == gofmt && $# -eq 1 && "${FAIL_TOOL-}" == git ]]; then
  exit 0
fi
{
  printf 'tool=%s\ntoolchain=%s\ncgo=%s\n' "$tool" "$GOTOOLCHAIN" "$CGO_ENABLED"
  for arg; do printf 'arg=%s\n' "$arg"; done
  printf 'end\n'
} >> "$COMMAND_LOG"
if [[ "${FAIL_TOOL-}" == "$tool" || ( -n "${FAIL_COMMAND-}" && "$*" == "$FAIL_COMMAND"* ) ]]; then
  echo 'invented failure' >&2
  exit 37
fi
case "$tool:$*" in
  'git:rev-parse HEAD') printf '%040d\n' 0 | tr 0 a ;;
  'git:ls-files -z *.go') printf 'tracked file.go\0' ;;
  'gofmt:-l '*) printf '%s' "${UNFORMATTED-}" ;;
esac
`
				path := filepath.Join(stubs, name)
				if strings.HasSuffix(name, ".sh") {
					path = filepath.Join(root, ".github", "scripts", name)
				}
				if name == "build-setup-native.sh" {
					path = filepath.Join(root, "installer", name)
				}
				if name == "setup-lifecycle-input.sh" {
					path = filepath.Join(root, "tools", "msi", "lifecycle", name)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			env := map[string]string{
				"PATH": stubs + string(os.PathListSeparator) + os.Getenv("PATH"),
				"GO":   filepath.ToSlash(filepath.Join(stubs, "go")), "GOFMT": filepath.ToSlash(filepath.Join(stubs, "gofmt")),
				"POWERSHELL":  filepath.ToSlash(filepath.Join(stubs, "pwsh")),
				"GOTOOLCHAIN": "auto", "CGO_ENABLED": "0", "COMMAND_LOG": filepath.ToSlash(log),
				"OUT": "", "REVISION": "", "RELEASE_VERSION": "development", "ARCHIVE": "", "NATIVE_DLL": "", "FAIL_TOOL": "", "FAIL_COMMAND": "", "UNFORMATTED": "",
				"NATIVE_DIR": "", "SETUP_STAGING": "", "WIX_ARCHIVE": "", "TOOL_LOG": "",
				"AUTHORED_SETUP": "", "MINGW_COPYRIGHT": "", "INSTALLER_ARCHIVE": "", "SETUP_BUILD_PROFILE": "normal",
				"MAKEFLAGS": "", "MAKELEVEL": "", "MFLAGS": "",
			}
			for key, value := range test.env {
				env[key] = value
			}
			args := []string{"--no-print-directory", "-f", makefile, "SHELL=" + shell, "GOTOOLCHAIN=auto"}
			if test.target != "" {
				args = append(args, test.target)
				if err := os.WriteFile(filepath.Join(root, test.target), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("make", args...)
			raw := filepath.Join(root, "bench output.txt")
			if test.pipeline {
				cmd = exec.Command(shell, "-c",
					`set -o pipefail; make --no-print-directory -f "$1" "SHELL=$3" GOTOOLCHAIN=auto benchmark 2>&1 | tee "$2"`,
					"fixture", filepath.ToSlash(makefile), filepath.ToSlash(raw), shell)
			}
			cmd.Dir = root
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				if _, replace := env[key]; !replace {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			for key, value := range env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			output, err := cmd.CombinedOutput()
			if (err != nil) != test.fail || !strings.Contains(string(output), test.message) {
				t.Fatalf("make failure=%t, want=%t; output=%s", err != nil, test.fail, output)
			}
			if test.target == "" || test.target == "help" {
				for _, exceptional := range []string{"make test-installer-lifecycle", "make restore-lifecycle-policy"} {
					if strings.Contains(string(output), exceptional) {
						t.Fatal("routine help exposes exceptional execution instead of the qualification index", exceptional)
					}
				}
			}
			if test.pipeline {
				data, err := os.ReadFile(raw)
				if err != nil || !strings.Contains(string(data), "invented failure") {
					t.Fatalf("missing raw failed benchmark diagnostics: %s, %v", data, err)
				}
			}
			data, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			var got []invocation
			for _, block := range strings.Split(string(data), "end\n") {
				if block == "" {
					continue
				}
				var call invocation
				for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
					key, value, _ := strings.Cut(line, "=")
					switch key {
					case "tool":
						call.tool = value
					case "toolchain":
						call.toolchain = value
					case "cgo":
						call.cgo = value
					case "arg":
						call.args = append(call.args, value)
					}
				}
				got = append(got, call)
			}
			if !slices.EqualFunc(got, test.want, func(a, b invocation) bool {
				return a.tool == b.tool && a.toolchain == b.toolchain && a.cgo == b.cgo && slices.Equal(a.args, b.args)
			}) {
				t.Fatalf("calls:\n got %#v\nwant %#v", got, test.want)
			}
		})
	}
}
