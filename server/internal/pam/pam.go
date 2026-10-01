// Package pam is a minimal cgo wrapper around libpam: it authenticates a
// user name and password non-interactively and runs the account check.
package pam

/*
#cgo LDFLAGS: -lpam
#include <security/pam_appl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	const char *user;
	const char *password;
} la_creds;

// la_conv answers password prompts with the password and echo-on prompts
// (user name) with the user; informational messages get no reply.
static int la_conv(int n, const struct pam_message **msg, struct pam_response **resp, void *appdata) {
	la_creds *c = (la_creds *)appdata;
	if (n <= 0 || n > PAM_MAX_NUM_MSG) return PAM_CONV_ERR;
	struct pam_response *r = calloc((size_t)n, sizeof(struct pam_response));
	if (r == NULL) return PAM_BUF_ERR;
	for (int i = 0; i < n; i++) {
		const char *answer = NULL;
		switch (msg[i]->msg_style) {
		case PAM_PROMPT_ECHO_OFF: answer = c->password; break;
		case PAM_PROMPT_ECHO_ON:  answer = c->user; break;
		case PAM_ERROR_MSG:
		case PAM_TEXT_INFO:       break;
		default:
			for (int j = 0; j < i; j++) {
				if (r[j].resp) { explicit_bzero(r[j].resp, strlen(r[j].resp)); free(r[j].resp); }
			}
			free(r);
			return PAM_CONV_ERR;
		}
		if (answer != NULL) {
			r[i].resp = strdup(answer);
			if (r[i].resp == NULL) {
				for (int j = 0; j < i; j++) {
					if (r[j].resp) { explicit_bzero(r[j].resp, strlen(r[j].resp)); free(r[j].resp); }
				}
				free(r);
				return PAM_BUF_ERR;
			}
		}
	}
	*resp = r;
	return PAM_SUCCESS;
}

// la_auth runs pam_authenticate + pam_acct_mgmt. *stage is set to 1 when
// authentication failed and 2 when the account check failed.
static int la_auth(const char *service, const char *user, const char *password, const char *rhost, int *stage, char *errbuf, size_t errlen) {
	la_creds creds = { user, password };
	struct pam_conv conv = { la_conv, &creds };
	pam_handle_t *h = NULL;
	*stage = 0;
	int rc = pam_start(service, user, &conv, &h);
	if (rc != PAM_SUCCESS) {
		snprintf(errbuf, errlen, "pam_start failed (%d)", rc);
		return rc;
	}
	if (rhost != NULL && rhost[0] != 0) pam_set_item(h, PAM_RHOST, rhost);
	pam_set_item(h, PAM_RUSER, user);
	rc = pam_authenticate(h, PAM_DISALLOW_NULL_AUTHTOK);
	if (rc != PAM_SUCCESS) {
		*stage = 1;
	} else {
		rc = pam_acct_mgmt(h, PAM_DISALLOW_NULL_AUTHTOK);
		if (rc != PAM_SUCCESS) *stage = 2;
	}
	if (rc != PAM_SUCCESS) snprintf(errbuf, errlen, "%s", pam_strerror(h, rc));
	pam_end(h, rc);
	return rc;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
)

// Errors returned by Authenticate.
var (
	// ErrAuth means wrong user name or password.
	ErrAuth = errors.New("authentication failed")
	// ErrAccount means the password was accepted but the account may not
	// sign in (expired, locked, password change required…).
	ErrAccount = errors.New("account not available")
)

// Error carries PAM's own description.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Kind.Error() + ": " + e.Msg }
func (e *Error) Unwrap() error { return e.Kind }

// Service returns the PAM service to use: "linuxadmin" when
// /etc/pam.d/linuxadmin exists, otherwise "login".
func Service() string {
	if _, err := os.Stat("/etc/pam.d/" + brand.PAMService); err == nil {
		return brand.PAMService
	}
	return brand.PAMFallbackService
}

// Authenticate checks user/password with PAM (authentication and account
// management). rhost is the client address, used by PAM for logging and
// lock-out modules. It blocks for as long as PAM does (failure delays).
func Authenticate(service, user, password, rhost string) error {
	if user == "" || strings.ContainsRune(user, 0) || strings.ContainsRune(password, 0) || strings.ContainsRune(rhost, 0) {
		return &Error{Kind: ErrAuth, Msg: "invalid input"}
	}
	cService := C.CString(service)
	cUser := C.CString(user)
	cPass := C.CString(password)
	cHost := C.CString(rhost)
	defer func() {
		C.explicit_bzero(unsafe.Pointer(cPass), C.size_t(len(password)))
		C.free(unsafe.Pointer(cService))
		C.free(unsafe.Pointer(cUser))
		C.free(unsafe.Pointer(cPass))
		C.free(unsafe.Pointer(cHost))
	}()
	var stage C.int
	var buf [256]C.char
	rc := C.la_auth(cService, cUser, cPass, cHost, &stage, &buf[0], C.size_t(len(buf)))
	if rc == C.PAM_SUCCESS {
		return nil
	}
	msg := C.GoString(&buf[0])
	switch stage {
	case 1:
		return &Error{Kind: ErrAuth, Msg: msg}
	case 2:
		return &Error{Kind: ErrAccount, Msg: msg}
	}
	return fmt.Errorf("pam: %s", msg)
}
