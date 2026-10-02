// Package pam is a minimal cgo wrapper around libpam: it authenticates a
// user name and password non-interactively, runs the account check, and
// opens/closes sessions (pam_setcred + pam_open_session) for the root
// session helper.
package pam

/*
#cgo LDFLAGS: -lpam
#include <security/pam_appl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// la_wipe zeroes a buffer in a way the compiler cannot drop. explicit_bzero
// would do, but needs glibc 2.25 and release builds target glibc 2.17.
static void la_wipe(void *p, size_t n) {
	volatile unsigned char *v = p;
	while (n--) *v++ = 0;
}

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
				if (r[j].resp) { la_wipe(r[j].resp, strlen(r[j].resp)); free(r[j].resp); }
			}
			free(r);
			return PAM_CONV_ERR;
		}
		if (answer != NULL) {
			r[i].resp = strdup(answer);
			if (r[i].resp == NULL) {
				for (int j = 0; j < i; j++) {
					if (r[j].resp) { la_wipe(r[j].resp, strlen(r[j].resp)); free(r[j].resp); }
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
// la_noconv is the conversation for session and account operations: no
// password is known, so prompts fail; messages are accepted silently.
static int la_noconv(int n, const struct pam_message **msg, struct pam_response **resp, void *appdata) {
	(void)appdata;
	if (n <= 0 || n > PAM_MAX_NUM_MSG) return PAM_CONV_ERR;
	for (int i = 0; i < n; i++) {
		if (msg[i]->msg_style != PAM_ERROR_MSG && msg[i]->msg_style != PAM_TEXT_INFO) return PAM_CONV_ERR;
	}
	struct pam_response *r = calloc((size_t)n, sizeof(struct pam_response));
	if (r == NULL) return PAM_BUF_ERR;
	*resp = r;
	return PAM_SUCCESS;
}

static struct pam_conv la_noconv_s = { la_noconv, NULL };

static int la_start_noconv(const char *service, const char *user, const char *rhost, pam_handle_t **h) {
	int rc = pam_start(service, user, &la_noconv_s, h);
	if (rc != PAM_SUCCESS) return rc;
	if (rhost != NULL && rhost[0] != 0) pam_set_item(*h, PAM_RHOST, rhost);
	pam_set_item(*h, PAM_RUSER, user);
	return PAM_SUCCESS;
}

// la_acct runs only pam_acct_mgmt (no authentication).
static int la_acct(const char *service, const char *user, const char *rhost, char *errbuf, size_t errlen) {
	pam_handle_t *h = NULL;
	int rc = la_start_noconv(service, user, rhost, &h);
	if (rc != PAM_SUCCESS) {
		snprintf(errbuf, errlen, "pam_start failed (%d)", rc);
		return rc;
	}
	rc = pam_acct_mgmt(h, PAM_DISALLOW_NULL_AUTHTOK);
	if (rc != PAM_SUCCESS) snprintf(errbuf, errlen, "%s", pam_strerror(h, rc));
	pam_end(h, rc);
	return rc;
}

// la_open runs pam_acct_mgmt, pam_setcred(ESTABLISH) and pam_open_session
// and returns the open handle. *stage: 1 start, 2 account, 3 setcred,
// 4 open_session.
static int la_open(const char *service, const char *user, const char *rhost, pam_handle_t **out, int *stage, char *errbuf, size_t errlen) {
	pam_handle_t *h = NULL;
	*out = NULL;
	*stage = 1;
	int rc = la_start_noconv(service, user, rhost, &h);
	if (rc != PAM_SUCCESS) {
		snprintf(errbuf, errlen, "pam_start failed (%d)", rc);
		return rc;
	}
	*stage = 2;
	rc = pam_acct_mgmt(h, PAM_DISALLOW_NULL_AUTHTOK);
	if (rc == PAM_SUCCESS) {
		*stage = 3;
		rc = pam_setcred(h, PAM_ESTABLISH_CRED);
		if (rc == PAM_SUCCESS) {
			*stage = 4;
			rc = pam_open_session(h, 0);
			if (rc != PAM_SUCCESS) pam_setcred(h, PAM_DELETE_CRED);
		}
	}
	if (rc != PAM_SUCCESS) {
		snprintf(errbuf, errlen, "%s", pam_strerror(h, rc));
		pam_end(h, rc);
		return rc;
	}
	*out = h;
	return PAM_SUCCESS;
}

static int la_close(pam_handle_t *h) {
	int rc = pam_close_session(h, 0);
	int rc2 = pam_setcred(h, PAM_DELETE_CRED);
	if (rc == PAM_SUCCESS) rc = rc2;
	pam_end(h, rc);
	return rc;
}

static void la_free_env(char **env) {
	if (env == NULL) return;
	for (char **e = env; *e != NULL; e++) free(*e);
	free(env);
}

static int la_is_account_error(int rc) {
	return rc == PAM_ACCT_EXPIRED || rc == PAM_NEW_AUTHTOK_REQD || rc == PAM_PERM_DENIED ||
		rc == PAM_USER_UNKNOWN || rc == PAM_AUTH_ERR || rc == PAM_AUTHTOK_EXPIRED;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"

	"github.com/ervisio/ervisio/server/internal/brand"
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

// Service returns the PAM service to use: "ervisio" when
// /etc/pam.d/ervisio exists, otherwise "login".
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
		C.la_wipe(unsafe.Pointer(cPass), C.size_t(len(password)))
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

func validInput(ss ...string) bool {
	for _, s := range ss {
		if strings.ContainsRune(s, 0) {
			return false
		}
	}
	return true
}

// CheckAccount runs PAM account management (pam_acct_mgmt) for user without
// authenticating: expired or locked accounts, password change required,
// pam_access/pam_time rules. It returns an error wrapping ErrAccount when
// PAM refuses the account, and another error when PAM itself failed (which
// callers should treat as "unknown", not as a refusal).
func CheckAccount(service, user, rhost string) error {
	if user == "" || !validInput(service, user, rhost) {
		return &Error{Kind: ErrAccount, Msg: "invalid input"}
	}
	cService, cUser, cHost := C.CString(service), C.CString(user), C.CString(rhost)
	defer func() {
		C.free(unsafe.Pointer(cService))
		C.free(unsafe.Pointer(cUser))
		C.free(unsafe.Pointer(cHost))
	}()
	var buf [256]C.char
	rc := C.la_acct(cService, cUser, cHost, &buf[0], C.size_t(len(buf)))
	if rc == C.PAM_SUCCESS {
		return nil
	}
	msg := C.GoString(&buf[0])
	if C.la_is_account_error(rc) != 0 {
		return &Error{Kind: ErrAccount, Msg: msg}
	}
	return fmt.Errorf("pam: %s", msg)
}

// Session is an open PAM session (pam_setcred + pam_open_session). It must
// be opened by root: session modules such as pam_systemd, pam_loginuid and
// pam_limits act on the calling process, so it is opened by a small helper
// process that then starts the bridge (see bridge.RunSessionHelper).
type Session struct {
	mu sync.Mutex
	h  *C.pam_handle_t
}

// OpenSession runs account management, establishes credentials and opens
// a session for user. Errors wrap ErrAccount when the account is refused.
func OpenSession(service, user, rhost string) (*Session, error) {
	if user == "" || !validInput(service, user, rhost) {
		return nil, &Error{Kind: ErrAccount, Msg: "invalid input"}
	}
	cService, cUser, cHost := C.CString(service), C.CString(user), C.CString(rhost)
	defer func() {
		C.free(unsafe.Pointer(cService))
		C.free(unsafe.Pointer(cUser))
		C.free(unsafe.Pointer(cHost))
	}()
	var h *C.pam_handle_t
	var stage C.int
	var buf [256]C.char
	rc := C.la_open(cService, cUser, cHost, &h, &stage, &buf[0], C.size_t(len(buf)))
	if rc == C.PAM_SUCCESS {
		return &Session{h: h}, nil
	}
	msg := C.GoString(&buf[0])
	if stage == 2 && C.la_is_account_error(rc) != 0 {
		return nil, &Error{Kind: ErrAccount, Msg: msg}
	}
	stages := map[C.int]string{1: "pam_start", 2: "pam_acct_mgmt", 3: "pam_setcred", 4: "pam_open_session"}
	return nil, fmt.Errorf("pam: %s: %s", stages[stage], msg)
}

// Env returns the environment set by the session modules (pam_env,
// pam_systemd's XDG_RUNTIME_DIR…), as KEY=value strings.
func (s *Session) Env() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == nil {
		return nil
	}
	list := C.pam_getenvlist(s.h)
	if list == nil {
		return nil
	}
	defer C.la_free_env(list)
	var out []string
	for p := list; *p != nil; p = (**C.char)(unsafe.Add(unsafe.Pointer(p), unsafe.Sizeof(*p))) {
		out = append(out, C.GoString(*p))
	}
	return out
}

// Close closes the session, deletes the credentials and ends PAM. It is
// idempotent.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h == nil {
		return nil
	}
	rc := C.la_close(s.h)
	s.h = nil
	if rc != C.PAM_SUCCESS {
		return fmt.Errorf("pam: close session failed (%d)", int(rc))
	}
	return nil
}
