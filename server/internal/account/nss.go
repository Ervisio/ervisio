package account

/*
#include <pwd.h>
#include <stdlib.h>
#include <errno.h>
#include <unistd.h>

// la_shell copies the login shell of name into buf. It returns 0 on
// success, 1 when the user is unknown and -1 on error.
static int la_shell(const char *name, char *buf, size_t buflen) {
	struct passwd pw, *res = NULL;
	long sz = sysconf(_SC_GETPW_R_SIZE_MAX);
	if (sz < 16384) sz = 16384;
	for (int i = 0; i < 6; i++) {
		char *tmp = malloc((size_t)sz);
		if (tmp == NULL) return -1;
		int rc = getpwnam_r(name, &pw, tmp, (size_t)sz, &res);
		if (rc == ERANGE) { free(tmp); sz *= 2; continue; }
		if (rc != 0) { free(tmp); return -1; }
		if (res == NULL) { free(tmp); return 1; }
		int ret = 0;
		if (pw.pw_shell == NULL) buf[0] = 0;
		else {
			size_t n = 0;
			while (pw.pw_shell[n] != 0 && n + 1 < buflen) { buf[n] = pw.pw_shell[n]; n++; }
			buf[n] = 0;
			if (pw.pw_shell[n] != 0) ret = -1;
		}
		free(tmp);
		return ret;
	}
	return -1;
}
*/
import "C"

import "unsafe"

// nssShell returns the login shell of name from NSS, or "" when unknown.
func nssShell(name string) string {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	var buf [4096]C.char
	if C.la_shell(cName, &buf[0], C.size_t(len(buf))) != 0 {
		return ""
	}
	return C.GoString(&buf[0])
}
