package writerproof

import (
	"testing"
	"time"

	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/google/uuid"
)

func id(t *testing.T) uuid.UUID {
	t.Helper()
	v, e := uuid.NewV7()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func subject(t *testing.T) Subject {
	return Subject{Person: id(t), Realm: aw.Realm{Installation: id(t), Application: id(t), Environment: id(t)}, Epoch: 0}
}

func TestZeroAndUnknownEvidenceNeverIssues(t *testing.T) {
	var a *aw.Attempt
	if _, err := Password(a, Action(255), PasswordCheck{}); err == nil {
		t.Fatal("unknown password action")
	}
	if _, err := Challenge(a, PasswordSignIn, ChallengeCheck{}); err == nil {
		t.Fatal("password action became challenge")
	}
	if _, err := Actor(a, PasswordSignIn, ActorCheck{}); err == nil {
		t.Fatal("actor became credential producer")
	}
	if _, err := Enrollment(a, ActorCheck{}, Evidence{}, FactorCheck{}); err == nil {
		t.Fatal("zero enrollment")
	}
	if _, err := TOTP(a, MFABegin, ActorCheck{}, Evidence{}, FactorCheck{}); err == nil {
		t.Fatal("begin became TOTP")
	}
	if _, err := FlowBegin(a, FederationCallbackLogin, FlowCheck{}, nil); err == nil {
		t.Fatal("callback became begin")
	}
	if _, err := FlowCallback(a, FederationBeginLogin, FlowCheck{}, ProviderCheck{}, Subject{}, nil); err == nil {
		t.Fatal("begin became callback")
	}
	if _, err := Counter(a, ActorCheck{}, Evidence{}, FactorCheck{}, CounterTransition{}); err == nil {
		t.Fatal("zero counter")
	}
	if _, err := ForIssue(a, Evidence{}); err == nil {
		t.Fatal("zero evidence issued")
	}
	if err := (Issuance{}).Check(a); err == nil {
		t.Fatal("zero issuance checked")
	}
	if _, err := (Issuance{}).Credential(a); err == nil {
		t.Fatal("zero credential")
	}
	if _, err := Finalize(a, Evidence{}, time.Now()); err == nil {
		t.Fatal("zero finalized")
	}
	if _, err := (Permit{}).Outcome(); err == nil {
		t.Fatal("zero permit")
	}
	if (Permit{}).Matches(a, Issuance{}) || (Permit{}).MatchesRelease(aw.Release{}) {
		t.Fatal("zero terminal match")
	}
}

func TestClosedIssueVariantMap(t *testing.T) {
	for kind := passwordKind; kind <= counterKind; kind++ {
		for action := Register; action <= Assurance; action++ {
			d := &evidenceData{kind: kind, action: action}
			_, _, _, _, got := issueFields(d)
			want := kind == passwordKind && action == PasswordSignIn || kind == challengeKind && action == MagicConfirm || kind == totpKind && (action == MFAConfirm || action == MFAChallenge) || kind == providerKind && action == FederationCallbackLogin
			if got != want {
				t.Fatalf("kind %d action %d issuance = %v", kind, action, got)
			}
		}
	}
}

func TestEveryOriginalFinalBoundIsStrict(t *testing.T) {
	b := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := b.Add(time.Second)
	sub := subject(t)
	actor := ActorCheck{Subject: sub, SessionID: id(t), Digest: [32]byte{1}, Method: "email_password", AuthenticatedAt: b.Add(-time.Minute), IdleUntil: at.Add(time.Hour), AbsoluteUntil: at.Add(2 * time.Hour), Assurance: "aal1", AssuranceUntil: at.Add(2 * time.Hour)}
	primary := &evidenceData{kind: passwordKind, started: b, passwordCheck: PasswordCheck{VerifiedAt: b, ValidUntil: at.Add(time.Hour)}}
	for _, tc := range []struct {
		name  string
		data  *evidenceData
		bound time.Time
	}{
		{"password", &evidenceData{kind: passwordKind, started: b, passwordCheck: PasswordCheck{VerifiedAt: b, ValidUntil: at}}, at},
		{"challenge", &evidenceData{kind: challengeKind, started: b, challenge: ChallengeCheck{CreatedAt: b, VerifiedAt: b, ExpiresAt: at}}, at},
		{"actor idle", &evidenceData{kind: actorKind, started: b, actor: func() ActorCheck { v := actor; v.IdleUntil = at; return v }()}, at},
		{"actor assurance", &evidenceData{kind: actorKind, started: b, actor: func() ActorCheck { v := actor; v.Assurance = "aal2"; v.AssuranceUntil = at; return v }()}, at},
		{"pending", &evidenceData{kind: enrollmentKind, started: b, actor: actor, primary: primary, factor: FactorCheck{PendingUntil: at}}, at},
		{"totp window", &evidenceData{kind: totpKind, action: MFAChallenge, started: b, actor: actor, primary: primary, factor: FactorCheck{VerifiedAt: b, WindowUntil: at}}, at},
		{"counter window", &evidenceData{kind: counterKind, action: MFAChallenge, started: b, actor: actor, primary: primary, factor: FactorCheck{VerifiedAt: b}, counter: CounterTransition{UpdatedAt: b}}, b.Add(30 * time.Second)},
		{"flow", &evidenceData{kind: flowKind, action: FederationBeginLogin, started: b, flow: FlowCheck{CreatedAt: b, ExpiresAt: at}}, at},
		{"provider receipt", &evidenceData{kind: providerKind, action: FederationCallbackLogin, started: b, flow: FlowCheck{CreatedAt: b, ExpiresAt: at.Add(time.Hour)}, provider: ProviderCheck{ValidatedAt: b, ProviderValidUntil: at.Add(time.Hour), ReceiptUntil: at}}, at},
		{"provider validity", &evidenceData{kind: providerKind, action: FederationCallbackLogin, started: b, flow: FlowCheck{CreatedAt: b, ExpiresAt: at.Add(time.Hour)}, provider: ProviderCheck{ValidatedAt: b, ProviderValidUntil: at, ReceiptUntil: at.Add(time.Hour)}}, at},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.data.finalBounds(tc.bound.Add(-time.Microsecond)) {
				t.Fatal("valid instant before bound denied")
			}
			if tc.data.finalBounds(tc.bound) || tc.data.finalBounds(tc.bound.Add(time.Microsecond)) {
				t.Fatal("original evidence bound accepted equality or expiry")
			}
			if tc.data.finalBounds(b.Add(-time.Microsecond)) {
				t.Fatal("backward F accepted")
			}
		})
	}
}
