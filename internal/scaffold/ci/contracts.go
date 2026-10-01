// Package ci defines the trust boundary for generated continuous integration.
package ci

// EventKind identifies an event that can start a generated job.
type EventKind string

const (
	EventPullRequest EventKind = "pull_request"
	EventMergeGroup  EventKind = "merge_group"
	EventPush        EventKind = "push"
	EventRelease     EventKind = "release"
	EventManual      EventKind = "workflow_dispatch"
)

// Event contains the trusted context used when deciding whether a job may run.
// TrustedRef means the ref was resolved by the root workflow from protected
// repository policy; a pull request's head ref is never trusted.
type Event struct {
	Kind        EventKind
	Fork        bool
	TrustedRef  bool
	Environment string
}

// Job describes a generated job's trust requirements.
type Job struct {
	Name                string
	Events              []EventKind
	Privileged          bool
	RequiresEnvironment string
}

// CanRun reports whether event satisfies a job's event and trust conditions.
// Privileged jobs require a non-fork event, a root-verified trusted ref and an
// explicitly named protected environment.
func CanRun(job Job, event Event) bool {
	if !knownEvent(event.Kind) {
		return false
	}
	if !contains(job.Events, event.Kind) {
		return false
	}
	if !job.Privileged {
		return true
	}
	if !privilegedEvent(event.Kind) {
		return false
	}
	if event.Fork || !event.TrustedRef || job.RequiresEnvironment == "" {
		return false
	}
	return event.Environment == job.RequiresEnvironment
}

func knownEvent(event EventKind) bool {
	switch event {
	case EventPullRequest, EventMergeGroup, EventPush, EventRelease, EventManual:
		return true
	default:
		return false
	}
}

func privilegedEvent(event EventKind) bool {
	switch event {
	case EventPush, EventRelease, EventManual:
		return true
	default:
		return false
	}
}

func contains(events []EventKind, event EventKind) bool {
	for _, candidate := range events {
		if candidate == event {
			return true
		}
	}
	return false
}
