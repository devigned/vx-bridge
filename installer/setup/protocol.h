/* SPDX-License-Identifier: MIT */
#ifndef VX_SETUP_PROTOCOL_H
#define VX_SETUP_PROTOCOL_H
#define WIN32_LEAN_AND_MEAN
#include "../managed/state.h"
#include <commctrl.h>
#include <wchar.h>
#include <string.h>

#define VX_SETUP_PROTOCOL L"1"
#define VX_AUTOLAUNCH_LABEL L"Auto-launch on Windows Startup"
#define VX_AUTOLAUNCH_HELP L"Starts when you sign in to Windows."
#define VX_SETUP_PIPE_PREFIX L"\\\\.\\pipe\\VxBridge.Setup."
#define VX_AUTH_MESSAGE L"VX_SETUP_AUTH"
typedef struct {
	WCHAR nonce[39], product[39], revision[41];
} VX_AUTH;
typedef struct { ULONGLONG start; LONG id; const char *label; } VX_DIAG_SPAN;
BOOL setup_diagnostics_enabled(void);
VX_DIAG_SPAN setup_diag_start(const char *label);
void setup_diag_end(VX_DIAG_SPAN span,const char *domain,DWORD code);
VX_DIAG_SPAN setup_diag_session_start(MSIHANDLE session,const char *label);
void setup_diag_session_end(MSIHANDLE session,VX_DIAG_SPAN span,const char *domain,DWORD code);
void setup_diag_event(MSIHANDLE session,const char *label,const char *domain,DWORD code);
void setup_diag_handle(const char *role,const char *transition,HANDLE handle,DWORD access,DWORD share,DWORD creation);
void setup_diag_token(MSIHANDLE session,HANDLE token);
BOOL setup_property(MSIHANDLE h, const WCHAR *name, WCHAR *out, DWORD count);
BOOL setup_receipt(MSIHANDLE db, VX_RECEIPT *r, WCHAR owner_hash[65]);
BOOL setup_catalog_owned(VX_CONTEXT *c, const VX_RECEIPT *before, const VX_RECEIPT *after);
BOOL setup_single_product(const VX_RECEIPT *r);
BOOL setup_related_owned(VX_CONTEXT *c, const VX_RECEIPT *before, const VX_RECEIPT *after);
UINT setup_upgrade_authorize(MSIHANDLE h);
BOOL setup_payload(VX_CONTEXT *c, const VX_RECEIPT *r, BOOL allow_missing);
BOOL setup_pin_root(VX_CONTEXT *c, BOOL *created, unsigned int *slot);
BOOL setup_root_pinned(VX_CONTEXT *c);
BOOL setup_pin_directory(VX_CONTEXT *c, const WCHAR *path);
BOOL setup_shortcut_owned(VX_CONTEXT *c, const WCHAR *name, const WCHAR *path);
BOOL setup_support_shortcut(VX_CONTEXT *c, BOOL required);
BOOL setup_remove_root(VX_CONTEXT *c, unsigned int *slot);
BOOL setup_root_snapshot(VX_CONTEXT *c, unsigned int slot, BY_HANDLE_FILE_INFORMATION *identity);
BOOL setup_release_root(VX_CONTEXT *c, unsigned int *slot, const BY_HANDLE_FILE_INFORMATION *identity);
BOOL setup_repin_root(VX_CONTEXT *c, unsigned int *slot, const BY_HANDLE_FILE_INFORMATION *identity);
BOOL setup_recreate_root(VX_CONTEXT *c, unsigned int *slot, BY_HANDLE_FILE_INFORMATION *identity);
BOOL setup_startup(VX_CONTEXT *c, VX_PENDING *p, int choice, BOOL restore, BOOL inspect);
BOOL setup_startup_state(VX_CONTEXT *c, BOOL *enabled, WCHAR command[261]);
HANDLE setup_lifetime(VX_CONTEXT *c);
BOOL setup_user(HANDLE process, const WCHAR *sid);
BOOL setup_transfer(HANDLE pipe, BOOL write, void *data, DWORD size);
BOOL setup_cache_record(VX_CONTEXT *c, const WCHAR *name, const WCHAR *path);
typedef BOOL (*VX_QUIT_WAIT)(HANDLE process, void *data);
BOOL setup_owned_quit(VX_CONTEXT *c, const VX_RECEIPT *before, VX_QUIT_WAIT wait, void *data);
UINT setup_remove_authorize(MSIHANDLE h);
UINT setup_remove_verify(MSIHANDLE h);
#endif
