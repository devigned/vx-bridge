/* SPDX-License-Identifier: MIT */
#include "protocol.h"
#include "bundle.h"
#include <bcrypt.h>
#include <objbase.h>
#include <sddl.h>
#include <shellapi.h>
#include <stdio.h>

static const char source_identity[]="VX_BUNDLE_SOURCE=" VX_SOURCE_REVISION;
static const WCHAR *names[VX_BUNDLE_COUNT]={
	L"setup.exe",L"bridge.msi",L"setup-input.json",L"msi-inspection.json",
	L"INSTALLER-NOTICE.txt",L"MINGW-COPYRIGHT.txt",L"LICENSE",
	L"THIRD_PARTY_NOTICES.txt",L"SHA256SUMS",L"application.zip"
};
typedef struct {
	HANDLE pin;
	BY_HANDLE_FILE_INFORMATION identity;
	BOOL created,identified;
} STAGED_FILE;

static BOOL hash_bytes(const BYTE *data,DWORD size,const BYTE wanted[32])
{
	BCRYPT_ALG_HANDLE alg=NULL; BCRYPT_HASH_HANDLE hash=NULL;
	BYTE actual[32]; BOOL ok=FALSE;
	if(BCryptOpenAlgorithmProvider(&alg,BCRYPT_SHA256_ALGORITHM,NULL,0)<0 ||
		BCryptCreateHash(alg,&hash,NULL,0,NULL,0,0)<0 ||
		BCryptHashData(hash,(BYTE *)data,size,0)<0 ||
		BCryptFinishHash(hash,actual,sizeof(actual),0)<0) goto done;
	ok=!memcmp(actual,wanted,sizeof(actual));
done:
	if(hash && BCryptDestroyHash(hash)<0) ok=FALSE;
	if(alg && BCryptCloseAlgorithmProvider(alg,0)<0) ok=FALSE;
	return ok;
}
static BOOL same_file(const BY_HANDLE_FILE_INFORMATION *a,const BY_HANDLE_FILE_INFORMATION *b)
{
	return a->dwVolumeSerialNumber==b->dwVolumeSerialNumber &&
		a->nFileIndexHigh==b->nFileIndexHigh && a->nFileIndexLow==b->nFileIndexLow;
}
static BOOL within_directory(const WCHAR *path,const WCHAR *directory)
{
	size_t n=wcslen(directory);
	return !_wcsnicmp(path,directory,n) && (!path[n] || path[n]==L'\\');
}
static BOOL remove_owned(const WCHAR *path,BOOL directory,const BY_HANDLE_FILE_INFORMATION *identity)
{
	HANDLE file=vx_file(path,directory,DELETE|FILE_READ_ATTRIBUTES,0,OPEN_EXISTING);
	if(file==INVALID_HANDLE_VALUE) return FALSE;
	BY_HANDLE_FILE_INFORMATION actual; FILE_DISPOSITION_INFO disposition={TRUE};
	BOOL ok=GetFileInformationByHandle(file,&actual) && same_file(&actual,identity) &&
		SetFileInformationByHandle(file,FileDispositionInfo,&disposition,sizeof(disposition));
	if(!CloseHandle(file)) ok=FALSE;
	return ok;
}
static BOOL check_media(const WCHAR *directory,const char *revision)
{
	WCHAR path[MAX_PATH],owner[65]; VX_RECEIPT receipt; MSIHANDLE db=0;
	char expected[65],actual[65]; BOOL ok=FALSE;
	if(!vx_path(path,directory,L"bridge.msi") ||
		MsiOpenDatabaseW(path,MSIDBOPEN_READONLY,&db)!=ERROR_SUCCESS ||
		!setup_receipt(db,&receipt,owner) || strcmp(receipt.revision,revision) ||
		!vx_ascii(expected,65,owner) || !vx_path(path,directory,L"setup.exe")) goto done;
	HANDLE file=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	if(file==INVALID_HANDLE_VALUE) goto done;
	ok=vx_hash(file,actual) && !strcmp(actual,expected);
	if(!CloseHandle(file)) ok=FALSE;
done:
	if(db && MsiCloseHandle(db)!=ERROR_SUCCESS) ok=FALSE;
	return ok;
}
static DWORD run_bundle(BOOL quiet,BOOL maintain,const WCHAR *extract)
{
	VX_CONTEXT context={0}; HANDLE self=INVALID_HANDLE_VALUE,mapping=NULL,directory=INVALID_HANDLE_VALUE;
	const BYTE *bytes=NULL; WCHAR image[MAX_PATH],stage[MAX_PATH]={0},path[MAX_PATH];
	STAGED_FILE files[VX_BUNDLE_COUNT]={0}; BY_HANDLE_FILE_INFORMATION stage_identity;
	BOOL created=FALSE,identified=FALSE,keep=FALSE,cleanup=TRUE,child_reported=FALSE;
	DWORD result=1; const WCHAR *failure=L"Installer verification or staging failed. No installation was started.";
	if(!vx_context(&context)) goto done;
	DWORD length=GetModuleFileNameW(NULL,image,ARRAYSIZE(image));
	if(!length || length>=ARRAYSIZE(image)) goto done;
	self=vx_file(image,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
	LARGE_INTEGER size;
	if(self==INVALID_HANDLE_VALUE || !GetFileSizeEx(self,&size) ||
		size.QuadPart<(LONGLONG)sizeof(VX_BUNDLE) || size.QuadPart>VX_BUNDLE_LIMIT) goto done;
	mapping=CreateFileMappingW(self,NULL,PAGE_READONLY,0,0,NULL);
	if(!mapping || !(bytes=MapViewOfFile(mapping,FILE_MAP_READ,0,0,0))) goto done;
	VX_BUNDLE bundle;
	memcpy(&bundle,bytes+size.QuadPart-sizeof(bundle),sizeof(bundle));
	if(!bundle_layout(&bundle,(uint64_t)size.QuadPart,source_identity+17) ||
		!hash_bytes(bytes,(DWORD)bundle.stub_size,bundle.stub_hash)) goto done;
	uint64_t offset=bundle.stub_size;
	for(unsigned int i=0;i<VX_BUNDLE_COUNT;++i) {
		if(!hash_bytes(bytes+offset,bundle.entries[i].size,bundle.entries[i].hash)) goto done;
		offset+=bundle.entries[i].size;
	}
	if(extract) {
		WCHAR owned[MAX_PATH];
		if(wcslen(extract)>=MAX_PATH || !GetFullPathNameW(extract,MAX_PATH,stage,NULL) ||
			_wcsicmp(extract,stage) || !vx_path(owned,context.base,L"VxInfiniteTeesBridge") ||
			within_directory(stage,context.root) || within_directory(stage,owned)) goto done;
		wcscpy(path,stage); WCHAR *slash=wcsrchr(path,L'\\');
		if(!slash || !slash[1]) goto done;
		*slash=0;
		if(!vx_pin_tree(&context,path)) goto done;
	} else {
		GUID id; WCHAR unique[64],guid[39];
		if(CoCreateGuid(&id)!=S_OK || StringFromGUID2(&id,guid,39)!=39 ||
			swprintf(unique,ARRAYSIZE(unique),L"vx-bridge-setup-%ls",guid)<=0 ||
			!vx_path(stage,context.base,unique)) goto done;
	}
	WCHAR sddl[640]; PSECURITY_DESCRIPTOR sd=NULL;
	if(swprintf(sddl,ARRAYSIZE(sddl),L"O:%lsD:P(A;OICI;GA;;;SY)(A;OICI;GA;;;%ls)",context.sid,context.sid)<=0 ||
		!ConvertStringSecurityDescriptorToSecurityDescriptorW(sddl,SDDL_REVISION_1,&sd,NULL)) goto done;
	SECURITY_ATTRIBUTES sa={sizeof(sa),sd,FALSE};
	created=CreateDirectoryW(stage,&sa);
	LocalFree(sd);
	if(!created) goto done;
	directory=vx_file(stage,TRUE,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE,OPEN_EXISTING);
	if(directory==INVALID_HANDLE_VALUE ||
		!GetFileInformationByHandle(directory,&stage_identity)) goto done;
	identified=TRUE; offset=bundle.stub_size;
	for(unsigned int i=0;i<VX_BUNDLE_COUNT;++i) {
		if(!vx_path(path,stage,names[i])) goto done;
		HANDLE output=vx_file(path,FALSE,GENERIC_READ|GENERIC_WRITE,0,CREATE_NEW);
		if(output==INVALID_HANDLE_VALUE) goto done;
		files[i].created=TRUE;
		files[i].identified=GetFileInformationByHandle(output,&files[i].identity);
		DWORD wrote=0;
		BOOL ok=files[i].identified &&
			WriteFile(output,bytes+offset,bundle.entries[i].size,&wrote,NULL) &&
			wrote==bundle.entries[i].size && FlushFileBuffers(output);
		if(!CloseHandle(output)) ok=FALSE;
		if(!ok) goto done;
		files[i].pin=vx_file(path,FALSE,GENERIC_READ,FILE_SHARE_READ,OPEN_EXISTING);
		BY_HANDLE_FILE_INFORMATION actual; char hash[65],wanted[65];
		for(unsigned int j=0;j<32;++j)
			snprintf(wanted+2*j,3,"%02x",bundle.entries[i].hash[j]);
		if(files[i].pin==INVALID_HANDLE_VALUE ||
			!GetFileInformationByHandle(files[i].pin,&actual) ||
			!same_file(&actual,&files[i].identity) ||
			!vx_hash(files[i].pin,hash) || strcmp(hash,wanted)) goto done;
		offset+=bundle.entries[i].size;
	}
	if(!check_media(stage,source_identity+17)) goto done;
	if(extract) { keep=TRUE; result=0; goto done; }
	WCHAR setup[MAX_PATH],msi[MAX_PATH],command[2*MAX_PATH+64];
	STARTUPINFOW startup={0}; PROCESS_INFORMATION process={0}; startup.cb=sizeof(startup);
	if(!vx_path(setup,stage,L"setup.exe") || !vx_path(msi,stage,L"bridge.msi") ||
		swprintf(command,ARRAYSIZE(command),L"\"%ls\" /package \"%ls\"%ls%ls",setup,msi,
			quiet?L" /quiet":L"",maintain?L" /maintain":L"")<=0 ||
		!CreateProcessW(setup,command,NULL,NULL,FALSE,0,NULL,stage,&startup,&process)) goto done;
	/* The child owns cancellation and guarded Quit. Never terminate it or delete
	 * its source while Windows Installer or the post-success UI is using it. */
	failure=L"Setup completion could not be observed. Keep the bridge stopped and use verified Setup maintenance.";
	BOOL thread_closed=CloseHandle(process.hThread);
	if(WaitForSingleObject(process.hProcess,INFINITE)==WAIT_OBJECT_0) {
		if(!GetExitCodeProcess(process.hProcess,&result)) result=1;
		else child_reported=TRUE;
	} else {
		/* Preserve media if the child's lifetime is unknown. */
		keep=TRUE; result=1;
	}
	if(!CloseHandle(process.hProcess) || !thread_closed) { result=1; child_reported=FALSE; }
done:
	for(unsigned int i=0;i<VX_BUNDLE_COUNT;++i) {
		if(files[i].pin && files[i].pin!=INVALID_HANDLE_VALUE && !CloseHandle(files[i].pin)) cleanup=FALSE;
		if(!keep && files[i].created && (!files[i].identified ||
			!vx_path(path,stage,names[i]) || !remove_owned(path,FALSE,&files[i].identity))) cleanup=FALSE;
	}
	if(directory!=INVALID_HANDLE_VALUE && !CloseHandle(directory)) cleanup=FALSE;
	if(!keep && created && (!identified || !remove_owned(stage,TRUE,&stage_identity))) cleanup=FALSE;
	if(bytes && !UnmapViewOfFile(bytes)) cleanup=FALSE;
	if(mapping && !CloseHandle(mapping)) cleanup=FALSE;
	if(self!=INVALID_HANDLE_VALUE && !CloseHandle(self)) cleanup=FALSE;
	if(!vx_close(&context)) cleanup=FALSE;
	if(!cleanup) {
		result=1;
		child_reported=FALSE;
		failure=L"Installer staging cleanup was incomplete. Installation may already have completed; do not assume it was rolled back.";
	}
	if(result && !quiet && !child_reported) {
		WCHAR message[1024];
		swprintf(message,ARRAYSIZE(message),L"%ls\n\nStaging folder: %ls\nNo forced shutdown or reboot was performed.",
			failure,stage[0]?stage:L"(not created)");
		MessageBoxW(NULL,message,L"VX Bridge Setup",MB_OK|MB_ICONERROR);
	} else if(!result && extract && !quiet) {
		MessageBoxW(NULL,L"Verified installer media was saved. No installation was performed. Keep these files together; raw MSI installation is not supported.",
			L"VX Bridge Setup",MB_OK|MB_ICONINFORMATION);
	}
	return result;
}
int WINAPI wWinMain(HINSTANCE instance,HINSTANCE previous,WCHAR *command,int show)
{
	(void)instance; (void)previous; (void)command; (void)show;
	int argc=0; WCHAR **argv=CommandLineToArgvW(GetCommandLineW(),&argc);
	if(!argv) return 1;
	BOOL quiet=FALSE,maintain=FALSE,remove=FALSE,usage=TRUE; const WCHAR *extract=NULL;
	DWORD result=2;
	for(int i=1;i<argc;++i) {
		if(!wcscmp(argv[i],L"/quiet")) quiet=TRUE;
		else if(!wcscmp(argv[i],L"/maintain")) maintain=TRUE;
		else if(!wcscmp(argv[i],L"/remove")) remove=TRUE;
		else if(!wcscmp(argv[i],L"/extract") && !extract && i+1<argc) extract=argv[++i];
		else goto done;
	}
	if((extract && (maintain || remove)) || (maintain && remove)) goto done;
	usage=FALSE;
	if(remove) {
		if(!quiet) MessageBoxW(NULL,L"Uninstall VX Bridge through Windows Settings > Apps > Installed apps. Normal uninstall does not need the original download.",
			L"VX Bridge Setup",MB_OK|MB_ICONINFORMATION);
		result=ERROR_CALL_NOT_IMPLEMENTED; goto done;
	}
	HANDLE token=NULL; TOKEN_ELEVATION elevation; DWORD size=sizeof(elevation);
	BOOL user=OpenProcessToken(GetCurrentProcess(),TOKEN_QUERY,&token) &&
		GetTokenInformation(token,TokenElevation,&elevation,size,&size) && !elevation.TokenIsElevated;
	if(token && !CloseHandle(token)) user=FALSE;
	if(!user) {
		if(!quiet) MessageBoxW(NULL,L"Run Setup normally as the installing user, not as administrator.",
			L"VX Bridge Setup",MB_OK|MB_ICONERROR);
		result=1; goto done;
	}
	result=run_bundle(quiet,maintain,extract);
done:
	if(usage && !quiet) MessageBoxW(NULL,L"Use Setup with no arguments, /quiet, /maintain, /remove, or /extract followed by a new absolute folder path.",
		L"VX Bridge Setup",MB_OK|MB_ICONERROR);
	LocalFree(argv); return (int)result;
}
