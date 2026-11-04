/* SPDX-License-Identifier: MIT */
#ifndef VX_MANAGED_STATE_H
#define VX_MANAGED_STATE_H
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <msi.h>
#include <msiquery.h>
#include <stdint.h>
#include <stddef.h>

#define VX_UPGRADE L"{5A5BB970-EB68-4550-AF42-F013F92DB3D8}"
#define VX_STATE_KEY L"Software\\VxInfiniteTeesBridge\\Installer"
#define VX_COUNT 6
extern const WCHAR *vx_files[VX_COUNT];
#pragma pack(push, 1)
typedef struct {
	char magic[8];
	uint32_t size, schema;
	char product[39], package[39], version[32], revision[41];
	char hashes[VX_COUNT][65];
	char reserved[83];
} VX_RECEIPT;
typedef struct {
	char magic[8];
	uint32_t size, operation, phase, has_before;
	char nonce[39], reserved;
	VX_RECEIPT before, after;
	DWORD run_saved, run_changed;
	WCHAR run_command[261];
} VX_PENDING;
#pragma pack(pop)
_Static_assert(sizeof(VX_RECEIPT) == 640, "Go/native receipt layout");
_Static_assert(offsetof(VX_RECEIPT,product) == 16, "product offset");
_Static_assert(offsetof(VX_RECEIPT,package) == 55, "package offset");
_Static_assert(offsetof(VX_RECEIPT,version) == 94, "version offset");
_Static_assert(offsetof(VX_RECEIPT,revision) == 126, "revision offset");
_Static_assert(offsetof(VX_RECEIPT,hashes) == 167, "payload hash offset");
_Static_assert(sizeof(VX_PENDING) == 1874, "transaction layout");
enum { VX_INSTALL = 1, VX_UNINSTALL = 2, VX_REPAIR = 3 };
enum { VX_WAITING = 1, VX_QUIESCENT = 2, VX_MUTATING = 3, VX_ROLLED_BACK = 4, VX_COMMITTED = 5 };
typedef struct {
	WCHAR sid[256], base[MAX_PATH], root[MAX_PATH], state[MAX_PATH];
	HANDLE gate, user_token, pins[64];
	unsigned int count;
	HKEY key;
} VX_CONTEXT;

BOOL vx_context(VX_CONTEXT *c);
BOOL vx_open_state(VX_CONTEXT *c, BOOL create);
BOOL vx_enter(VX_CONTEXT *c);
BOOL vx_leave(VX_CONTEXT *c);
BOOL vx_close(VX_CONTEXT *c);
HANDLE vx_file(const WCHAR *path, BOOL directory, DWORD access, DWORD share, DWORD creation);
BOOL vx_pin_tree(VX_CONTEXT *c, const WCHAR *path);
BOOL vx_path(WCHAR *out, const WCHAR *directory, const WCHAR *name);
BOOL vx_binary(VX_CONTEXT *c, const WCHAR *name, void *data, DWORD size, BOOL *exists);
BOOL vx_put(VX_CONTEXT *c, const WCHAR *name, const void *data, DWORD size);
BOOL vx_delete(VX_CONTEXT *c, const WCHAR *name);
BOOL vx_receipt(const VX_RECEIPT *r);
BOOL vx_pending(const VX_PENDING *p);
BOOL vx_ascii(char *out, unsigned int width, const WCHAR *text);
BOOL vx_wide(WCHAR *out, unsigned int width, const char *text);
BOOL vx_hex(const char *text, unsigned int count);
BOOL vx_guid(const char *text);
BOOL vx_hash(HANDLE file, char out[65]);
BOOL vx_payload(VX_CONTEXT *c, const VX_RECEIPT *r, BOOL empty);
BOOL vx_registered(VX_CONTEXT *c, const VX_RECEIPT *r, BOOL absent);
UINT vx_error(MSIHANDLE session, const WCHAR *reason);
#endif
