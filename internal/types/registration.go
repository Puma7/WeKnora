package types

import (
	"os"
	"strings"
)

// RegistrationMode controls how new accounts can be created.
type RegistrationMode string

const (
	// RegistrationModeOpen allows anyone to register (legacy default).
	RegistrationModeOpen RegistrationMode = "open"
	// RegistrationModeInviteOnly requires a valid invitation token.
	RegistrationModeInviteOnly RegistrationMode = "invite_only"
	// RegistrationModeWhitelist allows self-registration only when the email matches
	// the configured whitelist (exact addresses or @domains). Invitations still work.
	RegistrationModeWhitelist RegistrationMode = "whitelist"
	// RegistrationModeDisabled blocks all registration, including invitations.
	// Existing users keep working; equivalent to the legacy DISABLE_REGISTRATION=true.
	RegistrationModeDisabled RegistrationMode = "disabled"
)

// IsValid reports whether the mode string is a known value.
func (m RegistrationMode) IsValid() bool {
	switch m {
	case RegistrationModeOpen, RegistrationModeInviteOnly, RegistrationModeWhitelist, RegistrationModeDisabled:
		return true
	default:
		return false
	}
}

// RegistrationSettings is the resolved configuration the auth layer reads on every register call.
// It's derived from environment variables for now; a future iteration could persist it in DB.
type RegistrationSettings struct {
	Mode RegistrationMode
	// Whitelist contains lowercased entries:
	//   - "@example.com"  -> any email under that domain
	//   - "user@example.com" -> exact match
	Whitelist []string
}

// LoadRegistrationSettingsFromEnv reads REGISTRATION_MODE and REGISTRATION_EMAIL_WHITELIST
// (plus the legacy DISABLE_REGISTRATION flag) and returns the effective settings.
//
// Precedence:
//   - DISABLE_REGISTRATION=true  -> mode = disabled (legacy compatibility)
//   - REGISTRATION_MODE set      -> use it (must be one of the known values)
//   - otherwise                  -> open
func LoadRegistrationSettingsFromEnv() RegistrationSettings {
	mode := RegistrationModeOpen
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DISABLE_REGISTRATION")), "true") {
		mode = RegistrationModeDisabled
	}
	if raw := strings.TrimSpace(os.Getenv("REGISTRATION_MODE")); raw != "" {
		candidate := RegistrationMode(strings.ToLower(raw))
		if candidate.IsValid() {
			mode = candidate
		}
	}

	whitelist := parseWhitelist(os.Getenv("REGISTRATION_EMAIL_WHITELIST"))
	return RegistrationSettings{Mode: mode, Whitelist: whitelist}
}

// EmailMatchesWhitelist returns true if the email matches an exact entry or domain entry.
// Returns false when the whitelist is empty.
func (s RegistrationSettings) EmailMatchesWhitelist(email string) bool {
	if len(s.Whitelist) == 0 {
		return false
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	domain := ""
	if idx := strings.LastIndex(email, "@"); idx >= 0 {
		domain = email[idx:] // includes the '@'
	}
	for _, entry := range s.Whitelist {
		if entry == email {
			return true
		}
		if strings.HasPrefix(entry, "@") && entry == domain {
			return true
		}
	}
	return false
}

// PublicView returns the subset of settings safe to expose to unauthenticated clients.
// We never reveal the whitelist contents — only whether one is configured — so attackers
// can't enumerate valid corporate domains.
func (s RegistrationSettings) PublicView() RegistrationModePublic {
	return RegistrationModePublic{
		Mode:                s.Mode,
		HasEmailWhitelist:   len(s.Whitelist) > 0,
		AllowSelfRegister:   s.Mode == RegistrationModeOpen,
		AllowInvitedRegister: s.Mode != RegistrationModeDisabled,
	}
}

// RegistrationModePublic is the unauthenticated view returned by /auth/registration-mode.
type RegistrationModePublic struct {
	Mode                 RegistrationMode `json:"mode"`
	HasEmailWhitelist    bool             `json:"has_email_whitelist"`
	AllowSelfRegister    bool             `json:"allow_self_register"`
	AllowInvitedRegister bool             `json:"allow_invited_register"`
}

func parseWhitelist(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		entry := strings.ToLower(strings.TrimSpace(p))
		if entry == "" {
			continue
		}
		out = append(out, entry)
	}
	return out
}
