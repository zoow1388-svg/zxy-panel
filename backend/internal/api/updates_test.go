// SPDX-License-Identifier: AGPL-3.0-only
package api

import "testing"

func TestComparePanelVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.7.6.0-base-stable-agent-xray", "0.7.5.9.1-qr-flow-compatibility-fix-agent-xray", 1},
		{"0.7.6.2-clean-release-fix-agent-xray", "0.7.6.0-base-stable-agent-xray", 1},
		{"v0.7.6.2", "0.7.6.2-clean-release-fix-agent-xray", 0},
		{"0.7.5.9.1", "0.7.6.0", -1},
		{"0.7.6", "0.7.6.0", 0},
	}
	for _, tc := range cases {
		got := comparePanelVersions(tc.a, tc.b)
		if got < 0 {
			got = -1
		} else if got > 0 {
			got = 1
		}
		if got != tc.want {
			t.Fatalf("comparePanelVersions(%q, %q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUpdateAvailableDoesNotAllowDowngrade(t *testing.T) {
	if isRemoteVersionNewer("0.7.5.9.1-qr-flow-compatibility-fix-agent-xray", "0.7.6.0-base-stable-agent-xray") {
		t.Fatal("older remote version must not be treated as update")
	}
	if !isRemoteVersionNewer("0.7.6.2-clean-release-fix-agent-xray", "0.7.6.0-base-stable-agent-xray") {
		t.Fatal("newer remote version should be treated as update")
	}
}
