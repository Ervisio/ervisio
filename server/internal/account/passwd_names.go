package account

import "strings"

// splitDomainAccount splits a Windows account name into its domain part and
// SAM account name. "DOMAIN\user" yields (DOMAIN, user, false); "user@corp.example"
// yields (corp.example, user, true) where dns reports a UPN suffix (a DNS name).
// Names without a domain part yield ok=false.
func splitDomainAccount(name string) (domain, user string, dns, ok bool) {
	if i := strings.IndexByte(name, '\\'); i > 0 && i < len(name)-1 {
		return name[:i], name[i+1:], false, true
	}
	if i := strings.LastIndexByte(name, '@'); i > 0 && i < len(name)-1 {
		return name[i+1:], name[:i], true, true
	}
	return "", "", false, false
}
