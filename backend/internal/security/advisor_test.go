package security

import (
	"testing"
	"time"

	"nas-revival/backend/internal/nfs"
	"nas-revival/backend/internal/smb"
	"nas-revival/backend/internal/tlscert"
)

var now = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func byID(fs []Finding) map[string]Finding {
	m := map[string]Finding{}
	for _, x := range fs {
		m[x.ID] = x
	}
	return m
}

func cert(notAfter time.Time, self bool) *tlscert.Info {
	return &tlscert.Info{NotAfter: notAfter.Format(time.RFC3339), SelfSigned: self}
}

func TestEvaluateHealthy(t *testing.T) {
	fs := Evaluate(Input{Now: now,
		SSH:      &SSH{Running: true, RootLogin: "prohibit-password"},
		SMB:      &SMB{Enabled: true, MinProtocol: smb.ProtoSMB3},
		NFS:      &NFS{Enabled: true, Exports: []nfs.Export{{Share: "data", Clients: []nfs.Client{{Host: "192.168.1.0/24"}, {Host: "fd00::5"}}}}},
		TLS:      cert(now.Add(365*24*time.Hour), false),
		Accounts: &Accounts{NASAdmins: 1},
	})
	for _, x := range fs {
		if x.Status != StatusOK {
			t.Errorf("%s: %s", x.ID, x.Status)
		}
	}
}

func TestEvaluateFindings(t *testing.T) {
	fs := Evaluate(Input{Now: now,
		SSH:            &SSH{Running: true, RootLogin: "yes", PasswordAuth: true, SftpGroups: 1},
		SMB:            &SMB{Enabled: true, MinProtocol: smb.ProtoSMB2},
		NFS:            &NFS{Enabled: true, Exports: []nfs.Export{{Share: "data", Clients: []nfs.Client{{Host: "203.0.113.7"}}}}},
		TLS:            cert(now.Add(10*24*time.Hour), true),
		Accounts:       &Accounts{NASAdmins: 0, RecoveryAdmin: true},
		BlockedClients: 2,
	})
	want := map[string]string{
		"ssh.rootPassword": StatusCrit, "ssh.passwordAuth": StatusWarn, "smb.protocol": StatusInfo,
		"nfs.publicClient": StatusWarn, "tls.expiry": StatusWarn, "tls.selfSigned": StatusInfo,
		"accounts.recoveryOnly": StatusWarn, "login.blocked": StatusWarn,
	}
	m := byID(fs)
	for id, st := range want {
		if m[id].Status != st {
			t.Errorf("%s: %s, want %s", id, m[id].Status, st)
		}
	}
	if fs[0].ID != "ssh.rootPassword" {
		t.Errorf("worst first: %v", fs[0])
	}
	if m["tls.expiry"].Params["days"] != "10" || m["nfs.publicClient"].Params["exports"] != "data → 203.0.113.7" {
		t.Errorf("params: %v %v", m["tls.expiry"].Params, m["nfs.publicClient"].Params)
	}
	for _, x := range fs {
		if x.ID != "login.blocked" && x.Fix == "" {
			t.Errorf("%s has no fix link", x.ID)
		}
	}
}

func TestEvaluateEdges(t *testing.T) {
	m := byID(Evaluate(Input{Now: now,
		SSH: &SSH{Running: false, RootLogin: "yes", PasswordAuth: true},
		SMB: &SMB{Enabled: false, MinProtocol: smb.ProtoSMB2},
		NFS: &NFS{Enabled: false, Exports: []nfs.Export{{Share: "x", Clients: []nfs.Client{{Host: "8.8.8.8"}}}}},
		TLS: cert(now, false), Accounts: &Accounts{NASAdmins: 2, RecoveryAdmin: true},
	}))
	for id, st := range map[string]string{"ssh.rootPassword": StatusOK, "ssh.passwordAuth": StatusOK, "smb.protocol": StatusOK,
		"nfs.publicClient": StatusOK, "tls.expiry": StatusCrit, "accounts.recoveryOnly": StatusInfo} {
		if m[id].Status != st {
			t.Errorf("%s: %s, want %s", id, m[id].Status, st)
		}
	}
	if pw := byID(Evaluate(Input{Now: now, SSH: &SSH{Running: true, PasswordAuth: true}}))["ssh.passwordAuth"]; pw.Status != StatusInfo {
		t.Errorf("password auth without SFTP users: %s", pw.Status)
	}
	unknown := byID(Evaluate(Input{Now: now}))
	for _, id := range []string{"ssh.rootPassword", "smb.protocol", "nfs.publicClient", "tls.expiry", "accounts.recoveryOnly"} {
		if unknown[id].Status != StatusUnknown {
			t.Errorf("%s without input: %s", id, unknown[id].Status)
		}
	}
}
