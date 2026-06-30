// SPDX-License-Identifier: AGPL-3.0-only
package api

import "testing"

func TestCompareVersionNumbers(t *testing.T) {
	tests := []struct {
		remote  string
		current string
		want    int
	}{
		{"0.7.6.1-zip-path-install-fix-agent-xray", "0.7.5.9.1-qr-flow-compatibility-fix-agent-xray", 1},
		{"0.7.5.9.1-qr-flow-compatibility-fix-agent-xray", "0.7.6.1-zip-path-install-fix-agent-xray", -1},
		{"0.7.6.1", "0.7.6", 1},
		{"v0.7.6.1", "0.7.6.1-zip-path-install-fix-agent-xray", 0},
	}
	for _, tt := range tests {
		got, ok := compareVersionNumbers(tt.remote, tt.current)
		if !ok {
			t.Fatalf("compareVersionNumbers(%q, %q) was not comparable", tt.remote, tt.current)
		}
		if got != tt.want {
			t.Fatalf("compareVersionNumbers(%q, %q)=%d, want %d", tt.remote, tt.current, got, tt.want)
		}
	}
}

func TestUpgradeAllowedMessageRejectsDowngrade(t *testing.T) {
	ok, _ := upgradeAllowedMessage("0.7.5.9.1-qr-flow-compatibility-fix-agent-xray")
	if ok {
		t.Fatal("older remote version should not be allowed as an upgrade")
	}
	ok, _ = upgradeAllowedMessage("0.7.6.1-zip-path-install-fix-agent-xray")
	if ok {
		t.Fatal("equal remote version should not generate an upgrade command")
	}
	ok, _ = upgradeAllowedMessage("0.7.6.2-next-fix-agent-xray")
	if !ok {
		t.Fatal("newer remote version should be allowed as an upgrade")
	}
}
