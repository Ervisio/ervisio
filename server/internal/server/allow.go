package server

import (
	"slices"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/config"
)

// signInAllowed applies the sign-in allowlist (auth.allow_users,
// auth.allow_groups, auth.admins_only) to a resolved account. With nothing
// configured every account is allowed. Group membership is the one NSS
// reports now (primary and supplementary groups), so it is current at every
// sign-in and session re-check.
func signInAllowed(cfg *config.Config, a *account.Account) bool {
	au := cfg.Auth
	if !au.Restricted() {
		return true
	}
	if slices.Contains(au.AllowUsers, a.Name) {
		return true
	}
	for _, g := range a.GroupNames {
		if slices.Contains(au.AllowGroups, g) {
			return true
		}
	}
	// Root can only reach this point when allow_root is set (checked
	// earlier by every sign-in path).
	return au.AdminsOnly && a.CanSudo()
}
