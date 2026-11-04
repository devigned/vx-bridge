package managed

import (
	"os"
	"strings"
	"testing"
)

func TestManagedAdmissionDoesNotCoordinateWithInstaller(t *testing.T) {
	data, err := os.ReadFile("admission_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"msi.dll", "MsiGet", "MsiBegin", "MsiEnd", "MsiDoAction", "exec.Command"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("ordinary managed start must use local checks only: %s", forbidden)
		}
	}
	if !strings.Contains(ErrDenied.Error(), "Repair required") {
		t.Fatal("damaged managed copies must explain the maintenance path")
	}
}
