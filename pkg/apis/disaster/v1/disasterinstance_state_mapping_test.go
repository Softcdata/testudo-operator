package v1

import "testing"

func TestCurrentStateFromFSM(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		fsm  string
		want DisasterCurrentState
	}{
		{name: "pending maps to running", fsm: FsmStatePending, want: CurrentStateRunning},
		{name: "initializing maps to running", fsm: FsmStateInitializing, want: CurrentStateRunning},
		{name: "protected maps to running", fsm: FsmStateProtected, want: CurrentStateRunning},
		{name: "paused maps to paused", fsm: FsmStatePaused, want: CurrentStatePaused},
		{name: "failing over maps to transitioning", fsm: FsmStateFailingOver, want: CurrentStateTransitioning},
		{name: "failing back maps to transitioning", fsm: FsmStateFailingBack, want: CurrentStateTransitioning},
		{name: "active maps to active", fsm: FsmStateActive, want: CurrentStateActive},
		{name: "config error maps to failed", fsm: FsmStateConfigError, want: CurrentStateFailed},
		{name: "failed maps to failed", fsm: FsmStateFailed, want: CurrentStateFailed},
		{name: "unknown maps to unknown", fsm: "UnknownState", want: CurrentStateUnknown},
		{name: "empty maps to unknown", fsm: "", want: CurrentStateUnknown},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CurrentStateFromFSM(tc.fsm)
			if got != tc.want {
				t.Fatalf("unexpected currentState for fsm=%s: got=%s want=%s", tc.fsm, got, tc.want)
			}
		})
	}
}

func TestIsCurrentStateConsistent(t *testing.T) {
	t.Parallel()

	if !IsCurrentStateConsistent(FsmStateProtected, string(CurrentStateRunning)) {
		t.Fatalf("expected protected/running to be consistent")
	}
	if IsCurrentStateConsistent(FsmStateActive, string(CurrentStateRunning)) {
		t.Fatalf("expected active/running to be inconsistent")
	}
	if !IsCurrentStateConsistent(FsmStateActive, string(CurrentStateActive)) {
		t.Fatalf("expected active/active to be consistent")
	}
}
