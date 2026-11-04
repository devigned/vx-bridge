//go:build !windows

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type tagFixture struct {
	root, remote, source, main, gate, publish string
	env                                       map[string]string
}

func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := command(root, nil, "git", args...)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func fixtureCommit(t *testing.T, root, message string) string {
	t.Helper()
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "-c", "core.hooksPath="+filepath.Join(root, "no-hooks"),
		"commit", "--quiet", "--no-gpg-sign", "-m", message,
		"-m", "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>")
	return fixtureGit(t, root, "rev-parse", "HEAD")
}

func newTagFixture(t *testing.T) *tagFixture {
	t.Helper()
	gate, err := filepath.Abs("../../.github/scripts/check-tag-release.sh")
	if err != nil {
		t.Fatal(err)
	}
	publish, err := filepath.Abs("../../.github/scripts/publish-tag-release.sh")
	if err != nil {
		t.Fatal(err)
	}
	root, source := newDistributionFixture(t)
	fixtureGit(t, root, "-c", "tag.gpgSign=false", "tag", "v1.0.1", source)
	for _, version := range []string{"v1.0.2", "v1.0.3"} {
		fixtureGit(t, root, "-c", "tag.gpgSign=false", "tag", "--annotate", version, source, "-m", version)
	}
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("Changed only on later main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := fixtureCommit(t, root, "Advance main after tagged source")
	fixtureGit(t, root, "checkout", "--quiet", "--detach", source)
	if err := os.WriteFile(filepath.Join(root, "unreviewed"), []byte("Invented divergent source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unreviewed := fixtureCommit(t, root, "Create source not in main")
	fixtureGit(t, root, "-c", "tag.gpgSign=false", "tag", "v9.0.0", unreviewed)
	remote := filepath.Join(t.TempDir(), "remote.git")
	fixtureGit(t, root, "clone", "--quiet", "--bare", root, remote)
	stubs := t.TempDir()
	gh := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FIXTURE_API_LOG"
remote() { git --git-dir="$FIXTURE_REMOTE" "$@"; }
if [[ "$1" == api ]]; then
  [[ "${FIXTURE_FAILURE:-}" != api ]] || { echo 'fixture API unavailable' >&2; exit 1; }
  endpoint="$2"
  case "$endpoint" in
    repos/invented/repository/git/ref/tags/*)
      remote rev-parse --verify "refs/tags/${endpoint##*/}"
      ;;
    repos/invented/repository/git/ref/heads/main)
      remote rev-parse --verify refs/heads/main
      ;;
    repos/invented/repository/compare/*)
      pair="${endpoint##*/}"
      base="${pair%%...*}"
      head="${pair#*...}"
      if [[ "$base" == "$head" ]]; then
        echo identical
      elif remote merge-base --is-ancestor "$base" "$head"; then
        echo ahead
      else
        echo diverged
      fi
      ;;
    repos/invented/repository/actions/artifacts/123)
      jq -n --arg source "${FIXTURE_ARTIFACT_SOURCE:-$SOURCE_REVISION}" \
        --arg digest "sha256:${FIXTURE_ARTIFACT_DIGEST:-$ARTIFACT_DIGEST}" \
        --arg run "${FIXTURE_ARTIFACT_RUN:-$GITHUB_RUN_ID}" \
        --arg name "vx-bridge-tag-assets-$RELEASE_VERSION-$SOURCE_REVISION" \
        '{expired:false,name:$name,digest:$digest,workflow_run:{id:$run,head_sha:$source}}'
      ;;
    *) echo 'unexpected fixture API call' >&2; exit 92 ;;
  esac
elif [[ "$1 $2" == 'release create' ]]; then
  [[ ! -e "$FIXTURE_RELEASE_STATE" ]] || { echo 'fixture duplicate release' >&2; exit 1; }
  printf 'draft\n' > "$FIXTURE_RELEASE_STATE"
  [[ "${FIXTURE_FAILURE:-}" != upload ]] || { echo 'fixture upload failure' >&2; exit 1; }
  if [[ "${FIXTURE_FAILURE:-}" == moved_after_upload ]]; then
    remote update-ref "refs/tags/$RELEASE_VERSION" "$FIXTURE_MOVED_OID"
  fi
elif [[ "$1 $2" == 'release edit' ]]; then
  [[ "${FIXTURE_FAILURE:-}" != publish ]] || { echo 'fixture publish failure' >&2; exit 1; }
  [[ "$(cat "$FIXTURE_RELEASE_STATE")" == draft ]] || exit 93
  printf 'published\n' > "$FIXTURE_RELEASE_STATE"
else
  echo 'unexpected fixture gh command' >&2; exit 94
fi
`
	if err := os.WriteFile(filepath.Join(stubs, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	// Publication fixtures isolate GitHub orchestration. The full PE/MSI/bundle
	// verifier is exercised by tools/msi/setup, not this synthetic media.
	makeStub := `#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == verify-installer ]]
unzip -p "$ARCHIVE" build-manifest.json |
  jq -e --arg revision "$REVISION" --arg version "$RELEASE_VERSION" '
    .schema == 2 and .source_revision == $revision and .release_version == $version and
    all(.binaries[]; .build_revision == $revision and .release_version == $version)' >/dev/null
[[ -n "$(unzip -p "$INSTALLER_ARCHIVE" setup.exe)" ]]
`
	if err := os.WriteFile(filepath.Join(stubs, "make"), []byte(makeStub), 0o755); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	return &tagFixture{
		root: root, remote: remote, source: source, main: main, gate: gate, publish: publish,
		env: map[string]string{
			"PATH":                    stubs + string(os.PathListSeparator) + os.Getenv("PATH"),
			"GH_TOKEN":                "",
			"GITHUB_EVENT_NAME":       "push",
			"GITHUB_REPOSITORY":       "invented/repository",
			"GITHUB_EVENT_PATH":       filepath.Join(directory, "event.json"),
			"GITHUB_OUTPUT":           filepath.Join(directory, "output"),
			"GITHUB_SERVER_URL":       "https://github.invalid",
			"GITHUB_RUN_ID":           "1",
			"RUNNER_TEMP":             directory,
			"FIXTURE_REMOTE":          remote,
			"FIXTURE_API_LOG":         filepath.Join(directory, "calls.txt"),
			"FIXTURE_RELEASE_STATE":   filepath.Join(directory, "release-state"),
			"FIXTURE_FAILURE":         "",
			"ARTIFACT_DIGEST":         strings.Repeat("a", 64),
			"FIXTURE_ARTIFACT_SOURCE": "",
			"FIXTURE_ARTIFACT_DIGEST": "",
			"FIXTURE_ARTIFACT_RUN":    "",
			"FIXTURE_MOVED_OID":       fixtureGit(t, root, "rev-parse", "refs/tags/v1.0.3"),
		},
	}
}

func (f *tagFixture) selectTag(t *testing.T, version string) {
	t.Helper()
	oid := fixtureGit(t, f.root, "rev-parse", "refs/tags/"+version)
	fixtureGit(t, f.root, "checkout", "--quiet", "--detach", oid)
	f.env["GITHUB_REF"] = "refs/tags/" + version
	f.env["GITHUB_WORKFLOW_REF"] = "invented/repository/.github/workflows/release.yml@" + f.env["GITHUB_REF"]
	f.env["SOURCE_REVISION"] = fixtureGit(t, f.root, "rev-parse", "HEAD")
	f.env["RELEASE_VERSION"], f.env["TAG_OID"] = version, oid
	f.writeEvent(t, nil)
}

func (f *tagFixture) writeEvent(t *testing.T, overrides map[string]any) {
	t.Helper()
	event := map[string]any{
		"ref": f.env["GITHUB_REF"], "after": f.env["TAG_OID"], "before": strings.Repeat("0", 40),
		"created": true, "deleted": false, "forced": false,
	}
	for key, value := range overrides {
		event[key] = value
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.env["GITHUB_EVENT_PATH"], data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *tagFixture) execute(t *testing.T, script string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = f.root
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, exists := f.env[key]; !exists {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range f.env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestTagReleaseSourceSelection(t *testing.T) {
	f := newTagFixture(t)
	t.Chdir(f.root)
	for _, version := range []string{"v1.0.1", "v1.0.2"} {
		t.Run(version, func(t *testing.T) {
			f.selectTag(t, version)
			output, err := f.execute(t, f.gate, "resolve")
			if err != nil {
				t.Fatal(output, err)
			}
			resolved, err := os.ReadFile(f.env["GITHUB_OUTPUT"])
			if err != nil {
				t.Fatal(err)
			}
			want := "revision=" + f.source + "\nversion=" + version + "\ntag-oid=" + f.env["TAG_OID"] + "\n"
			if !strings.HasSuffix(string(resolved), want) || f.source == f.main {
				t.Fatal("tag must select its older exact commit, not advancing main", string(resolved))
			}
			if (f.env["TAG_OID"] != f.source) != (version == "v1.0.2") {
				t.Fatal("annotated tag object was not distinguished from commit")
			}
			if output, err := f.execute(t, f.gate, "verify"); err != nil {
				t.Fatal(output, err)
			}
			build := filepath.Join(t.TempDir(), "package")
			if err := run(f.source, version, build, "", 0); err != nil {
				t.Fatal(err)
			}
			files := readTestArchive(t, filepath.Join(build, archiveName(f.source, version)))
			if string(files["LICENSE"]) != "Invented distribution-test license\n" {
				t.Fatal("built later main content instead of tagged source")
			}
			var m manifest
			if err := json.Unmarshal(files["build-manifest.json"], &m); err != nil {
				t.Fatal(err)
			}
			if m.Revision != f.source || m.Version != version {
				t.Fatal("package does not carry exact tag version/source")
			}
			t.Logf("%s ref-object=%s selected-commit=%s advanced-main=%s package-version=%s",
				version, f.env["TAG_OID"], m.Revision, f.main, m.Version)
		})
	}
}

func TestTagReleaseRejectsInvalidEventsAndIdentity(t *testing.T) {
	f := newTagFixture(t)
	for _, test := range []struct {
		name   string
		env    map[string]string
		event  map[string]any
		change func(*testing.T)
	}{
		{name: "manual", env: map[string]string{"GITHUB_EVENT_NAME": "workflow_dispatch"}},
		{name: "pull_request", env: map[string]string{"GITHUB_EVENT_NAME": "pull_request"}},
		{name: "branch", env: map[string]string{"GITHUB_REF": "refs/heads/main"}, event: map[string]any{"ref": "refs/heads/main"}},
		{name: "deleted", event: map[string]any{"deleted": true, "after": strings.Repeat("0", 40)}},
		{name: "moved", event: map[string]any{"created": false, "before": f.main}},
		{name: "forced", event: map[string]any{"forced": true}},
		{name: "unsupported", event: map[string]any{"ref": "refs/tags/v1.0.1-pilot.1"}, env: map[string]string{"GITHUB_REF": "refs/tags/v1.0.1-pilot.1"}},
		{name: "leading_zero", event: map[string]any{"ref": "refs/tags/v01.0.1"}, env: map[string]string{"GITHUB_REF": "refs/tags/v01.0.1"}},
		{name: "wrong_ref", event: map[string]any{"ref": "refs/tags/v1.0.1"}},
		{name: "wrong_oid", event: map[string]any{"after": f.main}},
		{name: "commit_instead_of_tag_object", event: map[string]any{"after": f.source}},
		{name: "wrong_workflow", env: map[string]string{"GITHUB_WORKFLOW_REF": "invented/repository/.github/workflows/ci.yml@refs/heads/main"}},
		{name: "api_failure", env: map[string]string{"FIXTURE_FAILURE": "api"}},
		{name: "wrong_pinned_source", env: map[string]string{"SOURCE_REVISION": f.main}},
		{name: "wrong_pinned_version", env: map[string]string{"RELEASE_VERSION": "v1.0.1"}},
		{name: "wrong_pinned_tag_object", env: map[string]string{"TAG_OID": f.source}},
		{name: "current_main_checkout", change: func(t *testing.T) {
			fixtureGit(t, f.root, "checkout", "--quiet", "--detach", f.main)
		}},
		{name: "not_in_main", change: func(t *testing.T) { f.selectTag(t, "v9.0.0") }},
		{name: "remote_deleted", change: func(t *testing.T) {
			fixtureGit(t, f.root, "--git-dir="+f.remote, "update-ref", "-d", "refs/tags/v1.0.2")
		}},
		{name: "remote_reannotated_same_commit", change: func(t *testing.T) {
			fixtureGit(t, f.root, "--git-dir="+f.remote, "update-ref", "refs/tags/v1.0.2", f.env["FIXTURE_MOVED_OID"])
		}},
		{name: "remote_moved_commit", change: func(t *testing.T) {
			fixtureGit(t, f.root, "--git-dir="+f.remote, "update-ref", "refs/tags/v1.0.2", f.main)
		}},
		{name: "local_tag_moved", change: func(t *testing.T) {
			fixtureGit(t, f.root, "update-ref", "refs/tags/v1.0.2", f.main)
		}},
		{name: "local_tag_deleted", change: func(t *testing.T) {
			fixtureGit(t, f.root, "update-ref", "-d", "refs/tags/v1.0.2")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oid := fixtureGit(t, f.root, "rev-parse", "refs/tags/v1.0.2")
			f.selectTag(t, "v1.0.2")
			saved := make(map[string]string)
			for key, value := range f.env {
				saved[key] = value
			}
			t.Cleanup(func() {
				f.env = saved
				fixtureGit(t, f.root, "update-ref", "refs/tags/v1.0.2", oid)
				fixtureGit(t, f.root, "--git-dir="+f.remote, "update-ref", "refs/tags/v1.0.2", oid)
			})
			for key, value := range test.env {
				f.env[key] = value
			}
			// TAG_OID is the pinned output, not the push payload's after.
			f.writeEvent(t, test.event)
			if test.name == "wrong_pinned_tag_object" {
				f.writeEvent(t, map[string]any{"after": oid})
			}
			if test.change != nil {
				test.change(t)
			}
			if output, err := f.execute(t, f.gate, "verify"); err == nil {
				t.Fatalf("unsafe tag/event accepted: %s", output)
			}
		})
	}
}

func TestTagPublicationFailuresRemainVisible(t *testing.T) {
	f := newTagFixture(t)
	t.Chdir(f.root)
	f.selectTag(t, "v1.0.2")
	build := filepath.Join(t.TempDir(), "package")
	if err := run(f.source, "v1.0.2", build, "", 0); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, failure, state string
	}{
		{"normal_version", "", "published"},
		{"duplicate_release", "", "existing"},
		{"upload_failure", "upload", "draft"},
		{"publish_failure", "publish", "draft"},
		{"tag_moved_after_upload", "moved_after_upload", "draft"},
		{"missing_artifact_id", "", ""},
		{"missing_artifact_digest", "", ""},
		{"wrong_artifact_source", "", ""},
		{"wrong_artifact_digest", "", ""},
		{"wrong_artifact_run", "", ""},
		{"missing_installer", "", ""},
		{"mismatched_direct_msi", "", ""},
		{"different_package_version", "", ""},
		{"manifest_version_mismatch", "", ""},
		{"corrupt_checksum", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			files, err := os.ReadDir(build)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				data, err := os.ReadFile(filepath.Join(build, file.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if test.name == "corrupt_checksum" && strings.HasSuffix(file.Name(), ".sha256") {
					data[0] = 'z'
				}
				if err := os.WriteFile(filepath.Join(directory, file.Name()), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var installer bytes.Buffer
			writer := zip.NewWriter(&installer)
			for name, data := range map[string][]byte{"setup.exe": []byte("synthetic Setup"), "bridge.msi": []byte("synthetic MSI")} {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			installerName := "vx-bridge-installer-windows-amd64-v1.0.2-" + f.source + ".zip"
			msiName := "vx-bridge-windows-amd64-v1.0.2-" + f.source + ".msi"
			msiBytes := []byte("synthetic MSI")
			if test.name == "mismatched_direct_msi" {
				msiBytes = []byte("different MSI with valid external checksum")
			}
			for name, data := range map[string][]byte{installerName: installer.Bytes(), msiName: msiBytes} {
				if test.name == "missing_installer" && name == installerName {
					continue
				}
				if err := os.WriteFile(filepath.Join(directory, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+".sha256"), []byte(digest(data)+"  "+name+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f.env["RELEASE_DIRECTORY"], f.env["FIXTURE_FAILURE"], f.env["ARTIFACT_ID"] = directory, test.failure, "123"
			f.env["FIXTURE_RELEASE_STATE"] = filepath.Join(t.TempDir(), "state")
			f.env["FIXTURE_API_LOG"] = filepath.Join(t.TempDir(), "calls")
			f.env["RELEASE_VERSION"] = "v1.0.2"
			f.env["ARTIFACT_DIGEST"] = strings.Repeat("a", 64)
			f.env["FIXTURE_ARTIFACT_SOURCE"], f.env["FIXTURE_ARTIFACT_DIGEST"], f.env["FIXTURE_ARTIFACT_RUN"] = "", "", ""
			if test.name == "duplicate_release" {
				if err := os.WriteFile(f.env["FIXTURE_RELEASE_STATE"], []byte("existing\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.name == "missing_artifact_id" {
				f.env["ARTIFACT_ID"] = ""
			}
			switch test.name {
			case "missing_artifact_digest":
				f.env["ARTIFACT_DIGEST"] = ""
			case "wrong_artifact_source":
				f.env["FIXTURE_ARTIFACT_SOURCE"] = f.main
			case "wrong_artifact_digest":
				f.env["FIXTURE_ARTIFACT_DIGEST"] = strings.Repeat("b", 64)
			case "wrong_artifact_run":
				f.env["FIXTURE_ARTIFACT_RUN"] = "2"
			}
			if test.name == "different_package_version" {
				f.env["RELEASE_VERSION"] = "v1.0.1"
			}
			if test.name == "manifest_version_mismatch" {
				name := archiveName(f.source, "v1.0.2")
				path := filepath.Join(directory, name)
				files := readTestArchive(t, path)
				var m manifest
				if err := json.Unmarshal(files["build-manifest.json"], &m); err != nil {
					t.Fatal(err)
				}
				m.Version = "v1.0.1"
				files["build-manifest.json"], err = json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := writeArchive(path, files, time.Unix(0, 0)); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".sha256", []byte(digest(data)+"  "+name+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				fixtureGit(t, f.root, "--git-dir="+f.remote, "update-ref", "refs/tags/v1.0.2", f.env["TAG_OID"])
			})
			output, err := f.execute(t, f.publish)
			if (err == nil) != (test.name == "normal_version") {
				t.Fatalf("publication success=%t output=%s", err == nil, output)
			}
			state, readErr := os.ReadFile(f.env["FIXTURE_RELEASE_STATE"])
			if test.state == "" {
				if !os.IsNotExist(readErr) {
					t.Fatal("failed preflight attempted release creation")
				}
			} else if readErr != nil || strings.TrimSpace(string(state)) != test.state {
				t.Fatal("unexpected draft/publication state", string(state), readErr)
			}
			if test.name == "normal_version" {
				calls, err := os.ReadFile(f.env["FIXTURE_API_LOG"])
				if err != nil {
					t.Fatal(err)
				}
				for _, expected := range []string{"release create v1.0.2", "--verify-tag --target " + f.source,
					"--title v1.0.2", "release edit v1.0.2", "--draft=false --prerelease=false --latest=false",
					installerName, installerName + ".sha256", msiName, msiName + ".sha256"} {
					if !strings.Contains(string(calls), expected) {
						t.Fatalf("publication lacks exact version/source or normal-release flags: %s", calls)
					}
				}
			}
		})
	}
}
