package ci

import "testing"

func TestTrustContract(t *testing.T) {
	privileged := Job{
		Name:                "artifact",
		Events:              []EventKind{EventPullRequest, EventMergeGroup, EventKind("pull_request_target"), EventKind("repository_dispatch"), EventPush, EventRelease, EventManual},
		Privileged:          true,
		RequiresEnvironment: "release",
	}
	tests := []struct {
		name  string
		event Event
		want  bool
	}{
		{"fork pull request", Event{Kind: EventPullRequest, Fork: true, TrustedRef: true, Environment: "release"}, false},
		{"trusted pull request", Event{Kind: EventPullRequest, TrustedRef: true, Environment: "release"}, false},
		{"trusted pull request target", Event{Kind: EventKind("pull_request_target"), TrustedRef: true, Environment: "release"}, false},
		{"untrusted ref", Event{Kind: EventPush, Environment: "release"}, false},
		{"wrong environment", Event{Kind: EventPush, TrustedRef: true, Environment: "staging"}, false},
		{"trusted protected push", Event{Kind: EventPush, TrustedRef: true, Environment: "release"}, true},
		{"trusted release", Event{Kind: EventRelease, TrustedRef: true, Environment: "release"}, true},
		{"trusted manual run", Event{Kind: EventManual, TrustedRef: true, Environment: "release"}, true},
		{"merge queue is not artifact authority", Event{Kind: EventMergeGroup, TrustedRef: true, Environment: "release"}, false},
		{"unknown event is rejected", Event{Kind: EventKind("repository_dispatch"), TrustedRef: true, Environment: "release"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanRun(privileged, test.event); got != test.want {
				t.Fatalf("CanRun() = %t, want %t", got, test.want)
			}
		})
	}

	validation := Job{Name: "validate", Events: []EventKind{EventPullRequest, EventMergeGroup, EventPush, EventKind("unknown")}}
	for _, event := range []Event{
		{Kind: EventPullRequest, Fork: true},
		{Kind: EventPullRequest},
		{Kind: EventMergeGroup},
		{Kind: EventPush},
	} {
		if !CanRun(validation, event) {
			t.Errorf("credential-free validation must accept event %q", event.Kind)
		}
	}
	if CanRun(validation, Event{Kind: EventKind("unknown")}) {
		t.Fatal("credential-free validation must reject unknown event kinds")
	}
}
