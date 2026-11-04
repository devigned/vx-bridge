package desktop

import (
	"testing"
	"unsafe"
)

func TestDiagnosticSaveDialogNativeABI(t *testing.T) {
	if unsafe.Sizeof(openFilename{}) != 152 || unsafe.Offsetof(openFilename{}.File) != 48 ||
		unsafe.Offsetof(openFilename{}.Flags) != 96 || unsafe.Offsetof(openFilename{}.Reserved) != 136 {
		t.Fatal("incorrect Windows x64 OPENFILENAMEW layout")
	}
	for _, proc := range []interface{ Find() error }{getSaveName, dialogError} {
		if err := proc.Find(); err != nil {
			t.Fatal("native save/cancel API unavailable", err)
		}
	}
}
