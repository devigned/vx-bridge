package main

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestNumericAndStableIdentity(t *testing.T) {
	for _, version := range []string{"v0.0.0", "v1.0.1", "v255.255.65535", "development"} {
		if _, err := numeric(version); err != nil {
			t.Fatal(version, err)
		}

	}
	for _, version := range []string{"1.0.1", "v256.0.0", "v1.0.65536", "v1.0.1.0", "v1.0.1-beta", "v01.0.1"} {
		if _, err := numeric(version); err == nil {
			t.Fatal("unsupported MSI version", version)
		}
	}
	if guid("product/a") != guid("product/a") || guid("product/a") == guid("component/a") {
		t.Fatal("identity unstable or domains overlap")
	}
}
func TestUpgradeRangesExcludeOnlyImpossibleZeroVersionHistory(t *testing.T) {
	type upgradeRange struct {
		Minimum, Maximum                      string
		IncludeMinimum, IncludeMaximum        string
		Property, OnlyDetect, MigrateFeatures string
	}
	for _, version := range []string{"development", "v0.0.0", "v0.0.1", "v1.2.3"} {
		t.Run(version, func(t *testing.T) {
			number, err := numeric(version)
			if err != nil {
				t.Fatal(err)
			}
			data := author(guid("p"), number, strings.Repeat("a", 40), strings.Repeat("b", 64), nil)
			decoder := xml.NewDecoder(strings.NewReader(data))
			var older, newer []upgradeRange
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				start, ok := token.(xml.StartElement)
				if !ok || start.Name.Local != "UpgradeVersion" {
					continue
				}
				attrs := map[string]string{}
				for _, attr := range start.Attr {
					attrs[attr.Name.Local] = attr.Value
				}
				row := upgradeRange{attrs["Minimum"], attrs["Maximum"], attrs["IncludeMinimum"],
					attrs["IncludeMaximum"], attrs["Property"], attrs["OnlyDetect"], attrs["MigrateFeatures"]}
				switch row.Property {
				case "WIX_UPGRADE_DETECTED":
					older = append(older, row)
				case "WIX_DOWNGRADE_DETECTED":
					newer = append(newer, row)
				default:
					t.Fatal("unexpected upgrade range", row)
				}
			}
			if len(newer) != 1 || newer[0] != (upgradeRange{number, "", "yes", "", "WIX_DOWNGRADE_DETECTED", "yes", ""}) {
				t.Fatal("newer-version admission range changed", newer)
			}
			if number == "0.0.0" {
				if len(older) != 0 {
					t.Fatal("lowest version must not emit an impossible empty upgrade range", older)
				}
			} else if len(older) != 1 || older[0] != (upgradeRange{"0.0.0", number, "yes", "no", "WIX_UPGRADE_DETECTED", "", "yes"}) {
				t.Fatal("positive-version older-product detection changed", older)
			}
		})
	}
}

func TestUpgradeUsesConventionalMSISequencing(t *testing.T) {
	source := setupSource(t, "main.go")
	if !strings.Contains(source, `guid("product/" + version + "/" + revision)`) ||
		!strings.Contains(source, `"product_code_policy": "standard-major-upgrade"`) ||
		!strings.Contains(source, `<RemoveExistingProducts After="InstallFinalize">NOT REMOVE AND WIX_UPGRADE_DETECTED`) ||
		!strings.Contains(source, `REMOVE = "ALL" OR Installed OR NOT WIX_DOWNGRADE_DETECTED`) {
		t.Fatal("actual new versions need standard MSI-owned removal after owned preflight")
	}
}

func TestAuthoredScopeAndNoEagerLaunch(t *testing.T) {
	payload := map[string][]byte{}
	for _, name := range files {
		payload[name] = []byte(name)
	}
	data := author(guid("p"), "0.0.0", strings.Repeat("a", 40), strings.Repeat("b", 64), payload)
	var document any
	if err := xml.Unmarshal([]byte(data), &document); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`InstallScope="perUser"`, `InstallPrivileges="limited"`, `AllowAdvertise="no"`,
		`Name="VX Bridge"`, `Name="VX Bridge Setup"`,
		`Target="[INSTALLDIR]GSPconnect.exe"`,
		`MSIRESTARTMANAGERCONTROL" Value="Disable"`, `REBOOT" Value="ReallySuppress"`,
		`VxSetupAuthorize" Before="InstallValidate"`, `Execute="rollback"`, `Execute="deferred"`,
		`<PublishFeatures Suppress="yes"/>`, `<PublishProduct Suppress="yes"/>`, `<CreateShortcuts Suppress="yes"/>`,
		`Name="SourcePackage"`, `ARPNOMODIFY" Value="1"`, `ARPNOREPAIR" Value="1"`} {
		if !strings.Contains(data, required) {
			t.Fatal(required)
		}
	}
	for _, forbidden := range []string{"ForceReboot", "ScheduleReboot", "async", "NoImpersonate", "StartupApproved",
		"ServiceInstall", "ARPSYSTEMCOMPONENT", "UninstallString", `Target="[#VxGUI]"`} {
		if strings.Contains(data, forbidden) {
			t.Fatal(forbidden)
		}
	}
}

func TestAuthoredComponentDirectoriesAndGeneratedPackageCode(t *testing.T) {
	payload := map[string][]byte{}
	for _, name := range files {
		payload[name] = []byte(name)
	}

	decoder := xml.NewDecoder(strings.NewReader(author(guid("p"), "0.0.0",
		strings.Repeat("a", 40), strings.Repeat("b", 64), payload)))
	var directories []string
	seen := map[string]bool{}
	packages, rootRows, fixedTargets := 0, 0, 0
	component := ""
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch element := token.(type) {
		case xml.StartElement:
			attrs := map[string]string{}
			for _, attr := range element.Attr {
				attrs[attr.Name.Local] = attr.Value
			}
			switch element.Name.Local {
			case "Package":
				packages++
				if _, exists := attrs["Id"]; exists {
					t.Fatal("PackageCode must be generated by WiX for each emitted MSI")
				}
			case "Directory":
				if attrs["Id"] == "ProgramsFolder" {
					t.Fatal("shared Programs parent must not be MSI-owned")
				}
				directories = append(directories, attrs["Id"])
			case "Component":
				id := attrs["Id"]
				component = id
				want := "INSTALLDIR"
				if id == "Shortcut" {
					want = "ProgramMenuFolder"
				} else if id != "Maintenance" {
					found := false
					for _, fileID := range ids {
						found = found || id == fileID
					}
					if !found {
						t.Fatal("unexpected component", id)
					}
				}
				if seen[id] || len(directories) == 0 || directories[len(directories)-1] != want ||
					attrs["Directory"] != "" || attrs["Win64"] != "yes" {
					t.Fatal("component must be nested under its actual intended directory", id)
				}
				seen[id] = true
			case "RemoveFolder":
				rootRows++
				if component != "Maintenance" || attrs["Id"] != "VxRemoveOwnedRoot" || attrs["On"] != "uninstall" {
					t.Fatal("unexpected static directory cleanup")
				}
			case "CustomAction":
				if attrs["Id"] == "VxSetRoot" {
					fixedTargets++
					if attrs["Property"] != "INSTALLDIR" || attrs["Value"] != `[LocalAppDataFolder]Programs\VxInfiniteTeesBridge\` {
						t.Fatal("fixed target root changed")
					}
				}
			}
		case xml.EndElement:
			if element.Name.Local == "Directory" {
				directories = directories[:len(directories)-1]
			} else if element.Name.Local == "Component" {
				component = ""
			}
		}
	}
	if packages != 1 || len(seen) != 8 || rootRows != 1 || fixedTargets != 1 {
		t.Fatal("package/component/root scope changed", packages, seen, rootRows, fixedTargets)
	}
}

func TestScopedAdvertiseWarningPolicy(t *testing.T) {
	payload := map[string][]byte{}
	for _, name := range files {
		payload[name] = []byte(name)
	}
	source := author(guid("p"), "0.0.0", strings.Repeat("a", 40), strings.Repeat("b", 64), payload)
	for _, lineEnding := range []string{"\n", "\r\n"} {
		if err := verifyWarningPolicy([]byte(strings.ReplaceAll(source, "\n", lineEnding))); err != nil {
			t.Fatal("valid exact suppression policy", err)
		}
	}
	for name, invalid := range map[string]string{
		"missing": strings.Replace(source, `<PublishProduct Suppress="yes"/>`, "", 1),
		"duplicate": strings.Replace(source, `<PublishProduct Suppress="yes"/>`,
			`<PublishProduct Suppress="yes"/><PublishProduct Suppress="yes"/>`, 1),
		"condition":        strings.Replace(source, `<PublishProduct Suppress="yes"/>`, `<PublishProduct>0</PublishProduct>`, 1),
		"enabled-required": strings.Replace(source, `<PublishProduct Suppress="yes"/>`, `<PublishProduct/>`, 1),
		"extra-mutator": strings.Replace(source, `</AdvertiseExecuteSequence>`,
			`<PublishComponents/></AdvertiseExecuteSequence>`, 1),
		"extra-attribute": strings.Replace(source, `<PublishProduct Suppress="yes"/>`,
			`<PublishProduct Suppress="yes" Sequence="1"/>`, 1),
		"nested-content": strings.Replace(source, `<PublishProduct Suppress="yes"/>`,
			`<PublishProduct Suppress="yes"><Other/></PublishProduct>`, 1),
		"unexpected-action": strings.Replace(source, `<PublishProduct Suppress="yes"/>`, `<InstallFinalize Suppress="yes"/>`, 1),
		"other-table": strings.Replace(source, `<InstallExecuteSequence>`,
			`<InstallExecuteSequence><PublishProduct Suppress="yes"/>`, 1),
		"normal-install-disabled": strings.Replace(source, `<InstallExecuteSequence>`,
			`<InstallExecuteSequence><PublishProduct>0</PublishProduct>`, 1),
		"merge-module": strings.Replace(source, `<Feature `, `<Merge Id="Unreviewed"/><Feature `, 1),
		"malformed":    source[:len(source)-10],
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyWarningPolicy([]byte(invalid)); err == nil {
				t.Fatal("unexpected warning/suppression policy accepted")
			}
		})
	}
}

func TestExactValidationExceptions(t *testing.T) {
	record := func(ice, actions []string, code int) []byte {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"ice_exceptions": ice,
			"warning_exceptions": []any{map[string]any{
				"tool": "light.exe", "table": "AdvtExecuteSequence", "code": code, "actions": actions,
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if err := verifyValidationPolicy(record([]string{"ICE91", "ICE96"}, omittedAdvertise, 1008)); err != nil {
		t.Fatal("exact approved validation policy refused", err)
	}
	for name, policy := range map[string][]byte{
		"another-ice":            record([]string{"ICE91", "ICE84"}, omittedAdvertise, 1008),
		"extra-ice":              record([]string{"ICE91", "ICE96", "ICE64"}, omittedAdvertise, 1008),
		"duplicate-ice":          record([]string{"ICE91", "ICE96", "ICE96"}, omittedAdvertise, 1008),
		"missing-ice":            record([]string{"ICE96"}, omittedAdvertise, 1008),
		"generic-linker-warning": record([]string{"ICE91", "ICE96"}, omittedAdvertise, 1076),
		"unknown-action":         record([]string{"ICE91", "ICE96"}, []string{"PublishFeatures", "PublishProduct", "InstallFinalize"}, 1008),
		"duplicate-action":       record([]string{"ICE91", "ICE96"}, []string{"PublishFeatures", "PublishProduct", "PublishProduct"}, 1008),
		"missing-action":         record([]string{"ICE91", "ICE96"}, []string{"PublishFeatures", "PublishProduct"}, 1008),
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyValidationPolicy(policy); err == nil {
				t.Fatal("unexpected diagnostic or ICE waiver accepted")
			}
		})
	}
}
