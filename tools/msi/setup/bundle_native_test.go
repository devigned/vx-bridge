package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestNativeBundleParserAgreesWithEmittedLayout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable C parser fixture uses the native POSIX compiler; Windows authoring checks the actual PE")
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("native C compiler unavailable")
	}
	header, err := filepath.Abs("../../../installer/setup/bundle.h")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	source := "#include \"" + header + `"
#include <stdio.h>
#include <stdlib.h>
int main(int argc,char **argv) {
	if(argc!=3) return 2;
	FILE *file=fopen(argv[1],"rb");
	if(!file || fseek(file,0,SEEK_END)) return 3;
	long size=ftell(file);
	VX_BUNDLE bundle;
	if(size<(long)sizeof(bundle) || fseek(file,size-sizeof(bundle),SEEK_SET) ||
		fread(&bundle,1,sizeof(bundle),file)!=sizeof(bundle) || fclose(file)) return 4;
	return bundle_layout(&bundle,(uint64_t)size,argv[2])?0:1;
}
`
	path := filepath.Join(directory, "fixture.c")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(directory, "fixture")
	if output, err := exec.Command(cc, "-std=c11", "-Wall", "-Wextra", "-Werror", path, "-o", fixture).CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	for _, name := range []string{"normal", "source", "magic", "stub-size", "entry-size", "entry-empty", "extra-byte"} {
		t.Run(name, func(t *testing.T) {
			payload, _, revision, _ := installerTestPayload(t)
			payload["application.zip"] = []byte("synthetic archive")
			data, err := installerEXE(installerTestStub(revision), payload, revision)
			if err != nil {
				t.Fatal(err)
			}
			footer := data[len(data)-bundleFooterSize:]
			switch name {
			case "source":
				footer[16] = 'b'
			case "magic":
				footer[0] ^= 1
			case "stub-size":
				binary.LittleEndian.PutUint64(footer[8:], ^uint64(0))
			case "entry-size":
				binary.LittleEndian.PutUint32(footer[88:], ^uint32(0))
			case "entry-empty":
				binary.LittleEndian.PutUint32(footer[88:], 0)
			case "extra-byte":
				data = append(append(data[:len(data)-bundleFooterSize:len(data)-bundleFooterSize], 0), footer...)
			}
			bundle := filepath.Join(directory, name+".exe")
			if err := os.WriteFile(bundle, data, 0o600); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(fixture, bundle, revision).CombinedOutput()
			if (err == nil) != (name == "normal") {
				t.Fatal("native parser disagrees with emitted layout", name, string(output), err)
			}
		})
	}
}

func TestBundleRetainsSafeInnerSetupAndStagingBoundaries(t *testing.T) {
	source := setupSource(t, "../../../installer/setup/bundle.c")
	start := strings.Index(source, "static const WCHAR *names[")
	end := strings.Index(source[start:], "};") + start
	literals := regexp.MustCompile(`L"([^"]+)"`).FindAllStringSubmatch(source[start:end], -1)
	if len(literals) != len(bundleNames()) {
		t.Fatal("native/Go bundle inventory count differs")
	}
	for i, name := range bundleNames() {
		if literals[i][1] != name {
			t.Fatal("native/Go bundle inventory order differs", name)
		}
	}
	for _, required := range []string{
		"vx_context(&context)", "CreateDirectoryW(stage,&sa)", "OICI", "CREATE_NEW",
		"FILE_SHARE_READ,OPEN_EXISTING", "same_file(&actual,&files[i].identity)",
		"FlushFileBuffers(output)", "vx_hash(files[i].pin,hash)",
		"setup_receipt(db,&receipt,owner)", "strcmp(receipt.revision,revision)",
		`L"\"%ls\" /package \"%ls\"%ls%ls"`, "WaitForSingleObject(process.hProcess,INFINITE)",
		"keep=TRUE; result=1", "remove_owned(path,FALSE,&files[i].identity)",
		"remove_owned(stage,TRUE,&stage_identity)", "!elevation.TokenIsElevated",
		"Normal uninstall does not need the original download",
		"within_directory(stage,context.root)", "within_directory(stage,owned)",
	} {
		if !strings.Contains(source, required) {
			t.Fatal("bundle lost a source/staging/lifetime boundary", required)
		}
	}
	for _, forbidden := range []string{"MsiInstallProduct", "ShellExecute", "TerminateProcess", "MoveFileEx",
		"FILE_FLAG_DELETE_ON_CLOSE", "msiexec", "CREATE_SUSPENDED"} {
		if strings.Contains(source, forbidden) {
			t.Fatal("outer bundle bypasses the existing Setup or source lifetime", forbidden)
		}
	}
}
