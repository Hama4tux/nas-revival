// Package security holds the Security Advisor (SEC-13): read-only checks of
// the NAS configuration that each end in a finding with a link to the page
// where it is fixed. Texts live in the frontend (finding ID + parameters), so
// the advisor stays translatable (I18N-01).
package security

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/smb"
	"nas-revival/backend/internal/tlscert"
)

// Status of one finding, worst first.
const (
	StatusCrit    = "crit"
	StatusWarn    = "warn"
	StatusInfo    = "info"
	StatusOK      = "ok"
	StatusUnknown = "unknown" // the input could not be read
)

// Fix links (frontend routes).
const (
	FixServices = "#/services"
	FixSystem   = "#/system"
	FixUsers    = "#/users"
	FixSecurity = "#/system/security"
	FixUpdates  = "#/system/updates"
)

// UpdatesStaleAfter: a list of Debian updates older than this is a warning.
const UpdatesStaleAfter = 7 * 24 * time.Hour

// CertWarnBefore: a certificate expiring sooner is a warning.
const CertWarnBefore = 30 * 24 * time.Hour

// Finding is the result of one check.
type Finding struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Params map[string]string `json:"params,omitempty"`
	Fix    string            `json:"fix,omitempty"`
}

// SSH is what the advisor needs from the SSH service.
type SSH struct {
	Running      bool
	RootLogin    string // sshd -T permitrootlogin
	PasswordAuth bool   // effective
	SftpGroups   int
}

// SMB is what the advisor needs from Samba.
type SMB struct {
	Enabled     bool
	MinProtocol string
}

// NFS is what the advisor needs from the NFS server.
type NFS struct {
	Enabled bool
	Exports []nfs.Export
}

// Firewall is what the advisor needs from the firewall.
type Firewall struct {
	Supported bool
	Enabled   bool
}

// Updates is what the advisor needs from the Update Manager.
type Updates struct {
	CheckedAt *time.Time // last successful refresh of the package lists
	Security  int        // pending security updates
	Other     int        // pending other updates
}

// Accounts counts the administrators who can log in.
type Accounts struct {
	NASAdmins     int  // enabled NAS accounts with the administrator role
	RecoveryAdmin bool // admin.json exists
}

// Input collects the state to check; a nil part makes its checks unknown.
type Input struct {
	Now            time.Time
	SSH            *SSH
	SMB            *SMB
	NFS            *NFS
	TLS            *tlscert.Info
	Accounts       *Accounts
	Firewall       *Firewall
	Updates        *Updates
	BlockedClients int // clients blocked by the login limiter right now
}

// Evaluate runs every check; the result is ordered worst first, then by ID.
func Evaluate(in Input) []Finding {
	var out []Finding
	out = append(out, sshChecks(in.SSH)...)
	out = append(out, smbCheck(in.SMB), nfsCheck(in.NFS))
	out = append(out, tlsChecks(in.TLS, in.Now)...)
	out = append(out, accountCheck(in.Accounts), loginCheck(in.BlockedClients), firewallCheck(in.Firewall))
	out = append(out, updatesChecks(in.Updates, in.Now)...)
	rank := map[string]int{StatusCrit: 0, StatusWarn: 1, StatusInfo: 2, StatusUnknown: 3, StatusOK: 4}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Status] != rank[out[j].Status] {
			return rank[out[i].Status] < rank[out[j].Status]
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func f(id, status, fix string, params map[string]string) Finding {
	return Finding{ID: id, Status: status, Params: params, Fix: fix}
}

func sshChecks(s *SSH) []Finding {
	if s == nil {
		return []Finding{f("ssh.rootPassword", StatusUnknown, FixServices, nil), f("ssh.passwordAuth", StatusUnknown, FixServices, nil)}
	}
	root := f("ssh.rootPassword", StatusOK, FixServices, nil)
	if s.Running && s.RootLogin == "yes" {
		root.Status = StatusCrit
	}
	pw := f("ssh.passwordAuth", StatusOK, FixServices, nil)
	if s.Running && s.PasswordAuth {
		// root never uses a password (above), so passwords matter only
		// when SFTP users exist.
		pw.Status = StatusInfo
		if s.SftpGroups > 0 {
			pw.Status = StatusWarn
		}
	}
	return []Finding{root, pw}
}

func smbCheck(s *SMB) Finding {
	if s == nil {
		return f("smb.protocol", StatusUnknown, FixServices, nil)
	}
	// SMB1 cannot be configured at all (smb.Settings refuses it); SMB 2.1
	// has no encryption, SMB 3 does.
	if s.Enabled && s.MinProtocol == smb.ProtoSMB2 {
		return f("smb.protocol", StatusInfo, FixServices, map[string]string{"min": s.MinProtocol})
	}
	return f("smb.protocol", StatusOK, FixServices, nil)
}

// nfsCheck flags exports to addresses outside private networks: NFS with
// sec=sys trusts the client's user IDs.
func nfsCheck(s *NFS) Finding {
	if s == nil {
		return f("nfs.publicClient", StatusUnknown, FixServices, nil)
	}
	if !s.Enabled {
		return f("nfs.publicClient", StatusOK, FixServices, nil)
	}
	var hits []string
	for _, e := range s.Exports {
		for _, c := range e.Clients {
			if !privateHost(c.Host) {
				hits = append(hits, e.Share+" → "+c.Host)
			}
		}
	}
	if len(hits) == 0 {
		return f("nfs.publicClient", StatusOK, FixServices, nil)
	}
	return f("nfs.publicClient", StatusWarn, FixServices, map[string]string{"exports": strings.Join(hits, ", ")})
}

func privateHost(h string) bool {
	if p, err := netip.ParsePrefix(h); err == nil {
		a := p.Masked().Addr()
		return a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast()
	}
	a, err := netip.ParseAddr(h)
	return err == nil && (a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast())
}

func tlsChecks(info *tlscert.Info, now time.Time) []Finding {
	if info == nil {
		return []Finding{f("tls.expiry", StatusUnknown, FixSystem, nil)}
	}
	exp := f("tls.expiry", StatusOK, FixSystem, map[string]string{"notAfter": info.NotAfter})
	end, err := time.Parse(time.RFC3339, info.NotAfter)
	switch {
	case err != nil:
		exp.Status = StatusUnknown
	case !now.Before(end):
		exp.Status = StatusCrit
	case end.Sub(now) < CertWarnBefore:
		exp.Status = StatusWarn
		exp.Params["days"] = strconv.Itoa(int(end.Sub(now).Hours() / 24))
	}
	self := f("tls.selfSigned", StatusOK, FixSystem, nil)
	if info.SelfSigned {
		self.Status = StatusInfo
	}
	return []Finding{exp, self}
}

func accountCheck(a *Accounts) Finding {
	if a == nil {
		return f("accounts.recoveryOnly", StatusUnknown, FixUsers, nil)
	}
	switch {
	case a.NASAdmins == 0:
		// Only admin.json can administer: it is not a system account and
		// cannot use SMB/SFTP; a NAS administrator account is the norm.
		return f("accounts.recoveryOnly", StatusWarn, FixUsers, nil)
	case a.RecoveryAdmin:
		return f("accounts.recoveryOnly", StatusInfo, FixUsers, map[string]string{"admins": strconv.Itoa(a.NASAdmins)})
	}
	return f("accounts.recoveryOnly", StatusOK, FixUsers, nil)
}

func loginCheck(blocked int) Finding {
	if blocked > 0 {
		return f("login.blocked", StatusWarn, "", map[string]string{"clients": strconv.Itoa(blocked)})
	}
	return f("login.blocked", StatusOK, "", nil)
}

// firewallCheck warns when the kernel supports the firewall but it is off.
func firewallCheck(fw *Firewall) Finding {
	switch {
	case fw == nil:
		return f("firewall.off", StatusUnknown, FixSecurity, nil)
	case fw.Supported && !fw.Enabled:
		return f("firewall.off", StatusWarn, FixSecurity, nil)
	}
	return f("firewall.off", StatusOK, FixSecurity, nil)
}

// updatesChecks: pending security updates are a warning, other updates a
// recommendation; a list that was not refreshed for a week cannot be trusted.
func updatesChecks(u *Updates, now time.Time) []Finding {
	if u == nil {
		return []Finding{f("updates.pending", StatusUnknown, FixUpdates, nil), f("updates.stale", StatusUnknown, FixUpdates, nil)}
	}
	pending := f("updates.pending", StatusOK, FixUpdates, nil)
	switch {
	case u.Security > 0:
		pending.Status = StatusWarn
	case u.Other > 0:
		pending.Status = StatusInfo
	}
	if pending.Status != StatusOK {
		pending.Params = map[string]string{"security": strconv.Itoa(u.Security), "total": strconv.Itoa(u.Security + u.Other)}
	}
	stale := f("updates.stale", StatusOK, FixUpdates, nil)
	if u.CheckedAt == nil {
		stale.Status = StatusWarn
	} else if age := now.Sub(*u.CheckedAt); age > UpdatesStaleAfter {
		stale.Status = StatusWarn
		stale.Params = map[string]string{"days": strconv.Itoa(int(age.Hours() / 24))}
	}
	return []Finding{pending, stale}
}
