package baselineview

import (
	"html/template"
	"time"
)

var templates = template.Must(template.New("baseline").Funcs(template.FuncMap{
	"timeUTC": func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
	"activityLabel": func(s string) string {
		switch s {
		case "case.changed":
			return "Case record changed"
		case "checklist.changed":
			return "Supporting checklist changed"
		case "procedure.changed":
			return "Procedure text changed"
		case "draft.saved":
			return "Unsent draft saved"
		}
		return ""
	},
}).Parse(`
{{define "header"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}} — Continuity</title><link rel="stylesheet" href="/assets/base.css"><style>#continuity-baseline-content{overflow-wrap:anywhere}#continuity-baseline-content form{grid-template-columns:minmax(0,1fr)}#continuity-baseline-content button{max-width:100%;white-space:normal;overflow-wrap:anywhere}#continuity-baseline-content pre{white-space:pre-wrap;overflow-wrap:anywhere}#continuity-baseline-content input,#continuity-baseline-content select,#continuity-baseline-content textarea{max-width:100%;box-sizing:border-box}#continuity-baseline-content textarea{width:100%}#continuity-baseline-content select,#continuity-baseline-content textarea{font:inherit}</style></head><body><a href="#main-content">Skip to content</a><main id="main-content" tabindex="-1">{{end}}
{{define "footer"}}</main></body></html>{{end}}
{{define "start"}}<section id="continuity-baseline-content" aria-labelledby="baseline-heading"><nav class="site-header" aria-label="Continuity"><a href="/continuity/properties">Properties</a> <a href="/continuity/cases">Cases</a> <a href="/continuity/applications">Applications</a> <a href="/continuity/procedures">Procedures</a> <a href="/continuity/sources">Sources</a> <a href="/continuity/guide">Guide</a> <a href="/continuity/activity">Activity</a></nav><h1 id="baseline-heading">{{.Title}}</h1>{{end}}
{{define "end"}}{{if .Next}}<a href="{{.Next}}">Try next page</a>{{end}}</section>{{end}}
{{define "properties"}}<p>This supplied page may not include every property.</p><form method="get" action="/continuity/properties"><label for="property-query">Search properties</label><input id="property-query" name="q" maxlength="120" value="{{.Data.Query}}"><input type="hidden" name="limit" value="{{.Data.Limit}}"><button type="submit">Search</button></form>{{if .Data.Properties}}<ul>{{range .Data.Properties}}<li><a href="/continuity/properties/{{.ID}}">{{.Name}}</a> — {{.Area}}; recorded occupancy: {{.Occupancy}}</li>{{end}}</ul>{{else}}<p>No properties in this page.</p>{{end}}{{end}}
{{define "property"}}<dl><dt>Area</dt><dd>{{.Data.Area}}</dd><dt>Owner label</dt><dd>{{.Data.OwnerLabel}}</dd><dt>Occupant label</dt><dd>{{.Data.OccupantLabel}}</dd><dt>Recorded occupancy</dt><dd>{{.Data.Occupancy}}</dd><dt>Recorded inspection date</dt><dd>{{if .Data.InspectionDate}}{{.Data.InspectionDate}}{{else}}Not recorded{{end}}</dd></dl><p>Labels and dates are supplied records, not proof of authority or completed work.</p><a href="/continuity/cases">Open case register</a> <a href="/continuity/applications">Open application register</a> <a href="/continuity/properties">Back to properties</a>{{end}}
{{define "cases"}}<p>This supplied page may not include every case.</p>{{if .Data.Cases}}<ul>{{range .Data.Cases}}<li><a href="/continuity/cases/{{.ID}}">{{.Title}}</a> — recorded status: {{.Status}}; revision {{.Revision}}</li>{{end}}</ul>{{else}}<p>No cases in this page.</p>{{end}}{{end}}
{{define "source-links"}}<ul>{{range .}}<li><a href="/continuity/sources/{{.}}">Source {{.}}</a></li>{{end}}</ul>{{end}}
{{define "case"}}{{with .Data}}<p>Revision {{.Case.Revision}}. Status is an operator record, not approval or proof of external work.</p><a href="/continuity/properties/{{.Case.PropertyID}}">Property record</a><h2>Supplied source references</h2>{{template "source-links" .Case.SourceIDs}}
<p>Recorded status: {{.Case.Status}}. Editing is unavailable in this read-only baseline.</p>
<h2>Unsent draft</h2><p>This local text is not sent. No external action is performed.</p>{{if .Draft}}<p>Saved against case revision {{.Draft.CaseRevision}}.</p>{{if lt .Draft.CaseRevision .Case.Revision}}<p role="status">The case changed after this draft was saved. Review the current case; saving remains unavailable in this read-only baseline.</p>{{end}}<pre>{{"\n"}}{{.Draft.Body}}</pre><h3>Saved draft references</h3>{{template "source-links" .Draft.SourceIDs}}{{else}}<p>No saved draft was supplied.</p>{{end}}
<p>Saving draft edits is unavailable in this read-only baseline.</p><a href="/continuity/cases">Back to cases</a>{{end}}{{end}}
{{define "applications"}}<p>This supplied page may not include every application.</p>{{if .Data.Applications}}<ul>{{range .Data.Applications}}<li><a href="/continuity/applications/{{.ID}}">Application {{.ID}}</a> — revision {{.Revision}}</li>{{end}}</ul>{{else}}<p>No applications in this page.</p>{{end}}{{end}}
{{define "application"}}{{with .Data}}<p>Revision {{.Application.Revision}}. Supporting records do not decide an application.</p><p>Editing is unavailable in this read-only baseline.</p><a href="/continuity/properties/{{.Application.PropertyID}}">Property record</a><ul>{{range .Application.Items}}<li>{{if .HumanDecision}}<p>{{.Label}}</p><p>Final human decision is disabled. No automated decision or submission is available.</p><button type="button" disabled>Human decision unavailable</button>{{else}}<p>Recorded complete status: {{if .Done}}complete{{else}}not recorded complete{{end}}.</p>{{end}}</li>{{end}}</ul><a href="/continuity/applications">Back to applications</a>{{end}}{{end}}
{{define "procedures"}}<p>This supplied page may not include every procedure.</p>{{if .Data.Procedures}}<ul>{{range .Data.Procedures}}<li><a href="/continuity/procedures/{{.ID}}">{{.Title}}</a> — revision {{.Revision}}</li>{{end}}</ul>{{else}}<p>No procedures in this page.</p>{{end}}{{end}}
{{define "procedure"}}{{with .Data}}<p>Revision {{.Procedure.Revision}}. Editing this plain text does not change executable policy.</p><pre>{{"\n"}}{{.Procedure.Body}}</pre><p>Editing is unavailable in this read-only baseline.</p><a href="/continuity/procedures">Back to procedures</a>{{end}}{{end}}
{{define "activity"}}<p>Supplied activity records, shown in identifier order. This page is not a complete audit inventory.</p>{{if .Data.Activity}}<ol>{{range .Data.Activity}}<li><p>{{activityLabel .Action}} — revision {{.Revision}}; <time datetime="{{timeUTC .At}}">{{timeUTC .At}}</time></p><p>Recorded actor: {{.ActorID}}. Record: {{.ResourceID}}.</p></li>{{end}}</ol>{{else}}<p>No activity in this page.</p>{{end}}{{end}}
{{define "guide"}}{{with .Data}}<form method="get" action="/continuity/guide"><label for="guide-topic">Guide topic</label><select id="guide-topic" name="topic"><option value="attention"{{if eq .Request.Topic "attention"}} selected{{end}}>Attention in supplied cases</option><option value="explain_case"{{if eq .Request.Topic "explain_case"}} selected{{end}}>Explain a case (case required)</option><option value="spending_authority"{{if eq .Request.Topic "spending_authority"}} selected{{end}}>Spending authority</option><option value="handover"{{if eq .Request.Topic "handover"}} selected{{end}}>Handover</option><option value="owner_draft"{{if eq .Request.Topic "owner_draft"}} selected{{end}}>Owner update preview (case required)</option></select><label for="guide-case">Case (only for case explanation or owner update)</label><select id="guide-case" name="case_id"><option value="">No case</option>{{range .Cases}}<option value="{{.ID}}"{{if eq $.Data.Request.CaseID .ID}} selected{{end}}>{{.Title}}</option>{{end}}</select><button type="submit">Show guide</button></form><h2>{{.Response.Title}}</h2><p>{{.Response.Summary}}</p>{{if .Response.Items}}<ul>{{range .Response.Items}}<li><a href="/continuity/cases/{{.CaseID}}">Case record</a><p>{{.Text}}</p>{{template "source-links" .SourceIDs}}</li>{{end}}</ul>{{end}}{{if .Response.Draft}}<h3>Unsent preview</h3><pre>{{"\n"}}{{.Response.Draft.Body}}</pre><p>This preview has not been saved or sent. Open the case and explicitly save a draft after review.</p>{{end}}{{end}}{{end}}
{{define "error"}}<p role="alert">{{.Data}}</p><a href="/continuity/properties">Return to property register</a>{{end}}
`))
