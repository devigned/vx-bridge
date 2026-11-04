package diagnostics

import "golang.org/x/sys/windows"

func reportFolders() (string, string) {
	local, _ := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	config, _ := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DONT_VERIFY)
	return local, config
}

func windowsFacts() (*Windows, error) {
	version := windows.RtlGetVersion()
	return &Windows{Major: version.MajorVersion, Minor: version.MinorVersion, Build: version.BuildNumber}, nil
}
