package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ajent-social/amos/examples/continuity/domain"
	"github.com/ajent-social/amos/examples/continuity/guide"
	"github.com/google/uuid"
)

// This suite requires the operator-provided same-database fixture. Synthetic
// account/session setup is followed by actual native middleware and private
// baseline reads, not a listener, mutation HTTP or a complete signup journey.
func TestPrivateBaselineReadRuntimeRequiredService(t *testing.T) {
	f := privateReadFixture(t)
	if f == nil {
		return
	} // The unchanged helper runs this exact name in its W1 child.

	type records struct{ workspace, property, caseID, application, procedure, activity, source uuid.UUID }
	own := records{f.workspaceID, testID(t), testID(t), testID(t), testID(t), testID(t), f.sourceID}
	foreign := records{testID(t), testID(t), testID(t), testID(t), testID(t), testID(t), testID(t)}
	draftSource := testID(t)
	const scoped = `installation_id=$1 AND application_id=$2 AND environment_id=$3 AND workspace_id=$4`
	args := func(workspace uuid.UUID, tail ...any) []any {
		return append([]any{f.cfg.InstallationID, f.cfg.ApplicationID, f.cfg.EnvironmentID, workspace}, tail...)
	}
	// Register after the base helper, before seeding: LIFO removes dependent rows
	// before its source/workspace/person cleanup, including after partial failure.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := f.core.database.WithTx(ctx, nil, func(tx *sql.Tx) error {
			for _, workspace := range []uuid.UUID{own.workspace, foreign.workspace} {
				for _, table := range []string{"continuity_activity", "continuity_drafts", "continuity_applications", "continuity_cases", "continuity_procedures", "continuity_properties", "continuity_sources"} {
					if _, err := tx.ExecContext(ctx, `DELETE FROM public.`+table+` WHERE `+scoped, args(workspace)...); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Error("exact-owned baseline runtime cleanup failed")
		}
	})
	marshal := func(value any) []byte {
		t.Helper()
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal("synthetic baseline JSON setup failed")
		}
		return b
	}
	exec := func(t *testing.T, query string, values ...any) {
		t.Helper()
		err := f.core.database.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
			result, err := tx.ExecContext(f.ctx, query, values...)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return errors.New("expected one owned row")
			}
			return nil
		})
		if err != nil {
			t.Fatal("owned baseline runtime DML failed")
		}
	}
	const insertSource = `INSERT INTO public.continuity_sources(installation_id,application_id,environment_id,workspace_id,id,kind,title,body,sha256) VALUES($1,$2,$3,$4,$5,'document','Synthetic reference',$6,$7)`
	const insertCase = `INSERT INTO public.continuity_cases(installation_id,application_id,environment_id,workspace_id,id,property_id,title,status,revision,source_ids) VALUES($1,$2,$3,$4,$5,$6,$7,'awaiting_owner',1,$8)`
	const sourceBody = "Synthetic reference <script>untrusted</script>"
	digest := sha256.Sum256([]byte(sourceBody))
	digestText := hex.EncodeToString(digest[:])
	exec(t, insertSource, args(foreign.workspace, foreign.source, sourceBody, digestText)...)
	exec(t, insertSource, args(own.workspace, draftSource, sourceBody, digestText)...)
	for _, row := range []records{own, foreign} {
		items := marshal([]domain.ChecklistItem{
			{ID: testID(t).String(), Label: "Supporting record"},
			{ID: testID(t).String(), Label: "Final human decision", HumanDecision: true},
		})
		exec(t, `INSERT INTO public.continuity_properties(installation_id,application_id,environment_id,workspace_id,id,name,area,owner_label,occupant_label,occupancy,inspection_date) VALUES($1,$2,$3,$4,$5,'Baseline 50%_ property','North','Synthetic owner','Synthetic occupant','occupied','2024-02-29')`, args(row.workspace, row.property)...)
		exec(t, insertCase, args(row.workspace, row.caseID, row.property, "Baseline <script>case</script>", marshal([]string{row.source.String()}))...)
		exec(t, `INSERT INTO public.continuity_applications(installation_id,application_id,environment_id,workspace_id,id,property_id,revision,items) VALUES($1,$2,$3,$4,$5,$6,1,$7)`, args(row.workspace, row.application, row.property, items)...)
		exec(t, `INSERT INTO public.continuity_procedures(installation_id,application_id,environment_id,workspace_id,id,title,body,revision) VALUES($1,$2,$3,$4,$5,'Baseline procedure','Plain <script>procedure</script>',1)`, args(row.workspace, row.procedure)...)
		// The activity actor is a synthetic display label, never read authority.
		exec(t, `INSERT INTO public.continuity_activity(installation_id,application_id,environment_id,workspace_id,id,actor_id,resource_id,action,revision) VALUES($1,$2,$3,$4,$5,$6,$7,'case.changed',1)`, args(row.workspace, row.activity, testID(t), row.caseID)...)
	}

	read := func(t *testing.T, q baselineQuery, cancelAfterAdmission bool) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		var out []byte
		var resultErr error
		called := false
		handler := f.core.sessions.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called = true
			switch q.Page {
			case pageCase, pageApplication, pageProcedure:
				token, ok := f.core.sessions.CSRFToken(r)
				if !ok {
					t.Fatal("same-instance admitted form token unavailable")
				}
				q.CSRFToken = token
			}
			if cancelAfterAdmission {
				cancel()
			}
			out, resultErr = f.core.baseline(r.Context(), q)
			if resultErr == nil && q.CSRFToken != "" && !bytes.Contains(out, []byte(`name="_csrf" value="`+q.CSRFToken+`"`)) {
				t.Error("detail omitted the native session form token")
			}
		}))
		r := httptest.NewRequest(http.MethodGet, f.cfg.Origin+"/continuity", nil).WithContext(ctx)
		r.AddCookie(&http.Cookie{Name: "__Host-amos_session", Value: f.token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if !called {
			t.Fatalf("native baseline admission status=%d", w.Code)
		}
		return out, resultErr
	}
	success := func(t *testing.T, q baselineQuery, want ...string) []byte {
		t.Helper()
		b, err := read(t, q, false)
		if err != nil || len(b) == 0 {
			t.Fatalf("baseline read failed: %v", err)
		}
		for _, s := range want {
			if !bytes.Contains(b, []byte(s)) {
				t.Errorf("baseline output missing expected content %q", s)
			}
		}
		for _, id := range []uuid.UUID{foreign.property, foreign.caseID, foreign.application, foreign.procedure, foreign.activity, foreign.source} {
			if bytes.Contains(b, []byte(id.String())) {
				t.Error("foreign record disclosed")
			}
		}
		if bytes.Contains(b, []byte("<script>")) {
			t.Error("stored text rendered as executable markup")
		}
		if q.Fragment == bytes.Contains(b, []byte("<html")) {
			t.Error("incorrect full/fragment composition")
		}
		if q.Page != pageCase && q.Page != pageApplication && q.Page != pageProcedure && bytes.Contains(b, []byte(`name="_csrf"`)) {
			t.Error("token supplied to non-form page")
		}
		return b
	}
	noOutput := func(t *testing.T, q baselineQuery, want error) {
		t.Helper()
		b, err := read(t, q, false)
		if b != nil || !errors.Is(err, want) {
			t.Fatalf("expected no output and %v, got bytes=%d error=%v", want, len(b), err)
		}
	}

	for _, tc := range []struct {
		name string
		q    baselineQuery
		want string
	}{
		{"properties", baselineQuery{Page: pageProperties, Query: "50%_", Limit: 10}, own.property.String()},
		{"property", baselineQuery{Page: pageProperty, ID: own.property.String()}, "Synthetic occupant"},
		{"cases", baselineQuery{Page: pageCases, Limit: 10}, own.caseID.String()},
		{"case", baselineQuery{Page: pageCase, ID: own.caseID.String()}, "&lt;script&gt;case&lt;/script&gt;"},
		{"applications", baselineQuery{Page: pageApplications, Limit: 10}, own.application.String()},
		{"application", baselineQuery{Page: pageApplication, ID: own.application.String()}, "Final human decision is disabled"},
		{"procedures", baselineQuery{Page: pageProcedures, Limit: 10}, own.procedure.String()},
		{"procedure", baselineQuery{Page: pageProcedure, ID: own.procedure.String()}, "&lt;script&gt;procedure&lt;/script&gt;"},
		{"activity", baselineQuery{Page: pageActivity, Limit: 10}, own.caseID.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			success(t, tc.q, tc.want)
			q := tc.q
			q.Fragment = true
			success(t, q, tc.want)
		})
	}
	t.Run("list cursor produces bounded empty page", func(t *testing.T) {
		for _, tc := range []struct {
			page  baselinePage
			id    uuid.UUID
			empty string
		}{
			{pageProperties, own.property, "No properties in this page."},
			{pageCases, own.caseID, "No cases in this page."},
			{pageApplications, own.application, "No applications in this page."},
			{pageProcedures, own.procedure, "No procedures in this page."},
			{pageActivity, own.activity, "No activity in this page."},
		} {
			success(t, baselineQuery{Page: tc.page, After: tc.id.String(), Limit: 1}, tc.empty)
		}
	})
	for _, tc := range []struct {
		topic  guide.Topic
		caseID string
		limit  int
		want   string
	}{
		{guide.Attention, "", 10, "Open cases in the supplied selection"},
		{guide.ExplainCase, own.caseID.String(), 0, "Case explanation"},
		{guide.SpendingAuthority, "", 0, "This response grants no authority."},
		{guide.Handover, "", 10, "This bounded briefing does not certify succession readiness."},
		{guide.OwnerDraft, own.caseID.String(), 0, "This preview has not been saved or sent."},
	} {
		t.Run("guide "+string(tc.topic), func(t *testing.T) {
			q := baselineQuery{Page: pageGuide, Topic: tc.topic, CaseID: tc.caseID, Limit: tc.limit}
			b := success(t, q, tc.want)
			if tc.topic != guide.SpendingAuthority && !bytes.Contains(b, []byte(own.source.String())) {
				t.Error("guide omitted source reference")
			}
			q.Fragment = true
			success(t, q, tc.want)
		})
	}
	t.Run("missing saved draft is ordinary", func(t *testing.T) {
		success(t, baselineQuery{Page: pageCase, ID: own.caseID.String()}, "No saved draft was supplied.")
	})
	t.Run("stored saved and stale draft", func(t *testing.T) {
		exec(t, `INSERT INTO public.continuity_drafts(installation_id,application_id,environment_id,workspace_id,case_id,case_revision,body,source_ids) VALUES($1,$2,$3,$4,$5,1,$6,$7)`, args(own.workspace, own.caseID, "Unsent <script>draft</script>", marshal([]string{draftSource.String()}))...)
		q := baselineQuery{Page: pageCase, ID: own.caseID.String()}
		b := success(t, q, "Saved against case revision 1.", "Unsent &lt;script&gt;draft&lt;/script&gt;", draftSource.String())
		if bytes.Contains(b, []byte("The case changed after this draft was saved")) {
			t.Error("current saved draft labelled stale")
		}
		exec(t, `UPDATE public.continuity_cases SET revision=2 WHERE `+scoped+` AND id=$5`, args(own.workspace, own.caseID)...)
		success(t, q, "Saved against case revision 1.", "The case changed after this draft was saved", `name="expected" value="2"`)
	})
	t.Run("draft failure does not become missing draft", func(t *testing.T) {
		exec(t, `UPDATE public.continuity_sources SET sha256=$6 WHERE `+scoped+` AND id=$5`, args(own.workspace, draftSource, strings.Repeat("0", 64))...)
		t.Cleanup(func() {
			exec(t, `UPDATE public.continuity_sources SET sha256=$6 WHERE `+scoped+` AND id=$5`, args(own.workspace, draftSource, digestText)...)
		})
		// The case's own source is intact; only the saved draft reference is corrupt.
		success(t, baselineQuery{Page: pageCases, Limit: 10}, own.caseID.String())
		noOutput(t, baselineQuery{Page: pageCase, ID: own.caseID.String()}, errUnavailable)
	})
	t.Run("missing historical draft source is not an absent draft", func(t *testing.T) {
		exec(t, `DELETE FROM public.continuity_sources WHERE `+scoped+` AND id=$5`, args(own.workspace, draftSource)...)
		t.Cleanup(func() { exec(t, insertSource, args(own.workspace, draftSource, sourceBody, digestText)...) })
		// The current case still resolves. The saved draft exists, but its
		// historical source does not; that must suppress the entire detail.
		success(t, baselineQuery{Page: pageCases, Limit: 10}, own.caseID.String())
		noOutput(t, baselineQuery{Page: pageCase, ID: own.caseID.String()}, errUnavailable)
	})
	t.Run("missing and foreign details", func(t *testing.T) {
		for _, tc := range []struct {
			page    baselinePage
			foreign uuid.UUID
		}{
			{pageProperty, foreign.property}, {pageCase, foreign.caseID}, {pageApplication, foreign.application}, {pageProcedure, foreign.procedure},
		} {
			noOutput(t, baselineQuery{Page: tc.page, ID: testID(t).String()}, errNotFound)
			noOutput(t, baselineQuery{Page: tc.page, ID: tc.foreign.String()}, errNotFound)
		}
		for _, topic := range []guide.Topic{guide.ExplainCase, guide.OwnerDraft} {
			for _, id := range []uuid.UUID{testID(t), foreign.caseID} {
				noOutput(t, baselineQuery{Page: pageGuide, Topic: topic, CaseID: id.String()}, errNotFound)
			}
		}
	})
	t.Run("unadmitted context has no output", func(t *testing.T) {
		b, err := f.core.baseline(f.ctx, baselineQuery{Page: pageProperties, Limit: 10})
		if b != nil || !errors.Is(err, errDenied) {
			t.Fatal("unadmitted baseline read disclosed output")
		}
	})
	t.Run("canceled original admitted request has no output", func(t *testing.T) {
		b, err := read(t, baselineQuery{Page: pageCases, Limit: 10}, true)
		if b != nil || !errors.Is(err, errUnavailable) {
			t.Fatal("canceled admitted baseline request disclosed output")
		}
	})
	t.Run("stored case source corruption has no output", func(t *testing.T) {
		// Preserve this helper-owned source's exact original digest for restoration.
		var original string
		err := f.core.database.WithTx(f.ctx, nil, func(tx *sql.Tx) error {
			return tx.QueryRowContext(f.ctx, `SELECT sha256 FROM public.continuity_sources WHERE `+scoped+` AND id=$5`, args(own.workspace, own.source)...).Scan(&original)
		})
		if err != nil {
			t.Fatal("owned source digest read failed")
		}
		t.Cleanup(func() {
			exec(t, `UPDATE public.continuity_sources SET sha256=$6 WHERE `+scoped+` AND id=$5`, args(own.workspace, own.source, original)...)
		})
		exec(t, `UPDATE public.continuity_sources SET sha256=$6 WHERE `+scoped+` AND id=$5`, args(own.workspace, own.source, strings.Repeat("0", 64))...)
		for _, q := range []baselineQuery{
			{Page: pageCases, Limit: 10}, {Page: pageCase, ID: own.caseID.String()},
			{Page: pageGuide, Topic: guide.Attention, Limit: 10}, {Page: pageGuide, Topic: guide.Handover, Limit: 10},
			{Page: pageGuide, Topic: guide.ExplainCase, CaseID: own.caseID.String()}, {Page: pageGuide, Topic: guide.OwnerDraft, CaseID: own.caseID.String()},
		} {
			noOutput(t, q, errUnavailable)
		}
	})
	t.Run("guide distinct source boundary rejects without truncation", func(t *testing.T) {
		// Existing Case/Cases validate each case's <=32 references first. These
		// checks concern the later <=100 retained guide bodies, not zero prior reads.
		ids := make([]string, 100)
		for i := range ids {
			id := testID(t)
			ids[i] = id.String()
			exec(t, insertSource, args(own.workspace, id, sourceBody, digestText)...)
		}
		cases := make([]uuid.UUID, 4)
		for i := range cases {
			cases[i] = testID(t)
			end := (i + 1) * 32
			if end > 99 {
				end = 99
			}
			exec(t, insertCase, args(own.workspace, cases[i], own.property, "Bounded guide case", marshal(ids[i*32:end]))...)
		}
		// 99 new distinct references plus the original case's source = exactly100.
		for _, topic := range []guide.Topic{guide.Attention, guide.Handover} {
			b := success(t, baselineQuery{Page: pageGuide, Topic: topic, Limit: 100}, own.source.String())
			for _, id := range ids[:99] {
				if !bytes.Contains(b, []byte(id)) {
					t.Error("guide silently omitted a retained source reference")
				}
			}
		}
		exec(t, `UPDATE public.continuity_cases SET source_ids=$6 WHERE `+scoped+` AND id=$5`, args(own.workspace, cases[3], marshal(ids[96:]))...)
		for _, topic := range []guide.Topic{guide.Attention, guide.Handover} {
			noOutput(t, baselineQuery{Page: pageGuide, Topic: topic, Limit: 100}, errUnavailable)
			// Bounded selection remains usable; this is not a complete inventory claim.
			success(t, baselineQuery{Page: pageGuide, Topic: topic, Limit: 1}, own.caseID.String())
		}
	})
}
