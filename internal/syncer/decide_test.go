package syncer

import (
	"testing"

	"github.com/cayovalsamis/game-save-sync/internal/state"
)

func TestDecide(t *testing.T) {
	synced := &state.Game{CloudHead: "backup-1", LiveFingerprint: "fp1"}
	cases := []struct {
		name        string
		st          *state.Game
		fingerprint string
		head        string
		want        step
	}{
		{"never seen, nothing anywhere", nil, "", "", stepNone},
		{"never seen, only local saves", nil, "fp1", "", stepPushFresh},
		{"never seen, only cloud saves", nil, "", "backup-1", stepPull},
		{"never seen, saves on both sides", nil, "fp1", "backup-1", stepBoth},
		{"unchanged", synced, "fp1", "backup-1", stepNone},
		{"played here", synced, "fp2", "backup-1", stepPush},
		{"played on other device", synced, "fp1", "backup-2", stepPull},
		{"played on both", synced, "fp2", "backup-2", stepBoth},
		{"local saves deleted", synced, "", "backup-1", stepPull},
		{"local saves deleted, cloud gone too", synced, "", "", stepForget},
		{"cloud copy deleted", synced, "fp1", "", stepPushFresh},
		{"cloud deleted and played here", synced, "fp2", "", stepPushFresh},
		{"played on other device, local saves deleted", synced, "", "backup-2", stepPull},
		{"synced while both empty", &state.Game{}, "", "", stepNone},
	}
	for _, c := range cases {
		if got := decide(c.st, c.fingerprint, c.head); got != c.want {
			t.Errorf("%s: decide = %v, want %v", c.name, got, c.want)
		}
	}
}
