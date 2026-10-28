package breachregister

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/auth"
	appmiddleware "github.com/fidibeki/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const OperationsRootPath = "/admin/operations/privacy-incidents"

type operationsListRow struct {
	Incident      Incident
	DeadlineLabel string
	DeadlineTone  string
	ImpactLabel   string
}

type operationsListPage struct {
	GeneratedAt string
	Incidents   []operationsListRow
	Error       string
}

type operationsDetailPage struct {
	GeneratedAt         string
	Incident            Incident
	DeadlineLabel       string
	DeadlineTone        string
	ImpactLabel         string
	Chronology          []ChronologyEntry
	Assessments         []Assessment
	Actions             []ActionEvent
	ActionEvents        []ActionEvent
	Notifications       []ControllerNotification
	ProviderIntakes     []ProviderIncidentIntake
	PostIncidentJSONURL string
	PostIncidentMDURL   string
}

// RegisterPlatformOperationsRoutes exposes a read-only browser consumer of
// the breach register. The cookie remains path-scoped to /admin/operations;
// command routes continue to require bearer authentication and are not
// mounted here.
func RegisterPlatformOperationsRoutes(router chi.Router, options HTTPOptions, cookieName string) {
	handler := &httpHandler{options: options}
	requireRead := appmiddleware.RequirePlatformAdminCookie(
		options.JWT, cookieName, auth.PermissionPlatformBreachRead,
	)
	router.With(requireRead).Get(OperationsRootPath, handler.listOperationsIncidents)
	router.With(requireRead).Get(OperationsRootPath+"/{incidentID}", handler.getOperationsIncident)
	router.With(requireRead).Get(
		OperationsRootPath+"/{incidentID}/controller-notifications/{notificationID}/export",
		handler.exportControllerNotification,
	)
	router.With(requireRead).Head(
		OperationsRootPath+"/{incidentID}/controller-notifications/{notificationID}/export",
		handler.exportControllerNotification,
	)
	router.With(requireRead).Get(
		OperationsRootPath+"/{incidentID}/post-incident-report",
		handler.exportPostIncidentReport,
	)
	router.With(requireRead).Head(
		OperationsRootPath+"/{incidentID}/post-incident-report",
		handler.exportPostIncidentReport,
	)
}

func (h *httpHandler) listOperationsIncidents(w http.ResponseWriter, r *http.Request) {
	now := h.now().UTC()
	incidents, err := h.options.Store.ListIncidents(r.Context(), ListFilter{})
	page := operationsListPage{GeneratedAt: formatOperationsTime(now)}
	status := http.StatusOK
	if err != nil {
		status = http.StatusServiceUnavailable
		page.Error = "Incident data is temporarily unavailable."
	} else {
		page.Incidents = make([]operationsListRow, 0, len(incidents))
		for _, incident := range incidents {
			label, tone := operationsDeadline(now, incident.AuthorityNotificationDeadlineAt)
			page.Incidents = append(page.Incidents, operationsListRow{
				Incident: incident, DeadlineLabel: label, DeadlineTone: tone,
				ImpactLabel: operationsImpact(incident),
			})
		}
	}
	writeOperationsHTML(w, status, "incidents-list", page)
}

func (h *httpHandler) getOperationsIncident(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		writeOperationsError(w, http.StatusNotFound, "Incident unavailable",
			"The requested privacy incident could not be found.")
		return
	}
	incident, chronology, assessments, actions, actionEvents, notifications, providerIntakes, err :=
		h.options.Store.GetIncident(r.Context(), incidentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeOperationsError(w, http.StatusNotFound, "Incident unavailable",
				"The requested privacy incident could not be found.")
			return
		}
		writeOperationsError(w, http.StatusServiceUnavailable, "Incident data unavailable",
			"Incident data is temporarily unavailable.")
		return
	}
	now := h.now().UTC()
	deadlineLabel, deadlineTone := operationsDeadline(now, incident.AuthorityNotificationDeadlineAt)
	reportPath := OperationsRootPath + "/" + incident.ID + "/post-incident-report"
	writeOperationsHTML(w, http.StatusOK, "incident-detail", operationsDetailPage{
		GeneratedAt: now.Format("2006-01-02 15:04:05 UTC"),
		Incident:    incident, DeadlineLabel: deadlineLabel, DeadlineTone: deadlineTone,
		ImpactLabel: operationsImpact(incident), Chronology: chronology, Assessments: assessments,
		Actions: actions, ActionEvents: actionEvents, Notifications: notifications,
		ProviderIntakes:     providerIntakes,
		PostIncidentJSONURL: reportPath + "?format=json",
		PostIncidentMDURL:   reportPath + "?format=markdown",
	})
}

func writeOperationsError(w http.ResponseWriter, status int, title, message string) {
	writeOperationsHTML(w, status, "operations-error", struct {
		Title   string
		Message string
	}{Title: title, Message: message})
}

func writeOperationsHTML(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set(
		"Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'",
	)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = operationsPage.ExecuteTemplate(w, name, data)
}

func operationsDeadline(now, deadline time.Time) (string, string) {
	if deadline.IsZero() {
		return "Deadline unavailable", "neutral"
	}
	delta := deadline.Sub(now)
	if delta <= 0 {
		return "Clock elapsed " + operationsDuration(-delta) + " ago", "critical"
	}
	tone := "normal"
	if delta <= 24*time.Hour {
		tone = "warning"
	}
	return operationsDuration(delta) + " remaining", tone
}

func operationsDuration(value time.Duration) string {
	value = value.Round(time.Minute)
	if value < time.Hour {
		return strconv.Itoa(max(1, int(value/time.Minute))) + "m"
	}
	hours := int(value / time.Hour)
	days := hours / 24
	hours %= 24
	if days > 0 && hours > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if days > 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dh", hours)
}

func operationsImpact(incident Incident) string {
	impact := make([]string, 0, 3)
	if incident.ConfidentialityImpact {
		impact = append(impact, "Confidentiality")
	}
	if incident.IntegrityImpact {
		impact = append(impact, "Integrity")
	}
	if incident.AvailabilityImpact {
		impact = append(impact, "Availability")
	}
	if len(impact) == 0 {
		return "No impact dimension selected"
	}
	return strings.Join(impact, ", ")
}

func formatOperationsTime(value time.Time) string {
	if value.IsZero() {
		return "Not recorded"
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

func formatOperationsOptionalTime(value *time.Time) string {
	if value == nil {
		return "Not recorded"
	}
	return formatOperationsTime(*value)
}

func operationsOptionalString(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "Not recorded"
	}
	return *value
}

func operationsOptionalID(value *string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "Unassigned"
	}
	return *value
}

func operationsEstimate(minimum, maximum *int64) string {
	if minimum == nil || maximum == nil {
		return "Not estimated"
	}
	if *minimum == *maximum {
		return strconv.FormatInt(*minimum, 10)
	}
	return strconv.FormatInt(*minimum, 10) + "-" + strconv.FormatInt(*maximum, 10)
}

func operationsLabel(value any) string {
	return strings.ReplaceAll(fmt.Sprint(value), "_", " ")
}

func operationsCountLabel(count int, singular, plural string) string {
	label := plural
	if count == 1 {
		label = singular
	}
	return strconv.Itoa(count) + " " + label
}

func operationsStatusClass(value any) string {
	switch fmt.Sprint(value) {
	case "failed", "notification_required", "communication_required", "high_risk":
		return "critical"
	case "pending", "processing", "assessment_pending", "in_progress":
		return "warning"
	case "sent", "completed", "not_applicable", "notification_not_required",
		"communication_not_required", "not_high_risk":
		return "complete"
	default:
		return "neutral"
	}
}

func operationsNotificationExportURL(incidentID, notificationID string, format ExportFormat) string {
	return OperationsRootPath + "/" + incidentID + "/controller-notifications/" +
		notificationID + "/export?format=" + string(format)
}

var operationsPage = template.Must(template.New("breach-operations").Funcs(template.FuncMap{
	"time":                  formatOperationsTime,
	"optionalTime":          formatOperationsOptionalTime,
	"optionalString":        operationsOptionalString,
	"optionalID":            operationsOptionalID,
	"estimate":              operationsEstimate,
	"join":                  strings.Join,
	"label":                 operationsLabel,
	"countLabel":            operationsCountLabel,
	"statusClass":           operationsStatusClass,
	"notificationExportURL": operationsNotificationExportURL,
}).Parse(operationsPageDocument))

const operationsPageDocument = `
{{define "operations-styles"}}
<style>
  :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, sans-serif; color: #172321; background: #f4f7f6; }
  * { box-sizing: border-box; }
  body { min-width: 320px; margin: 0; }
  a { color: inherit; }
  .topbar { min-height: 58px; background: #fff; border-bottom: 1px solid #dbe3e0; }
  .topbar-inner { display: flex; width: min(1380px, 100%); min-height: 58px; align-items: center; gap: 20px; margin: 0 auto; padding: 0 28px; }
  .brand { display: flex; align-items: center; gap: 9px; font-size: 14px; font-weight: 750; text-decoration: none; }
  .brand i { display: grid; width: 29px; height: 29px; place-items: center; background: #172321; color: #fff; border-radius: 5px; font-style: normal; }
  nav { display: flex; align-self: stretch; gap: 4px; }
  nav a { display: flex; align-items: center; padding: 0 10px; border-bottom: 2px solid transparent; color: #65736f; font-size: 11px; font-weight: 700; text-decoration: none; }
  nav a[aria-current="page"] { border-color: #087f73; color: #172321; }
  .readonly { margin-left: auto; padding: 4px 8px; background: #e4f2ee; color: #087f73; border-radius: 4px; font-size: 10px; font-weight: 750; text-transform: uppercase; }
  main { width: min(1380px, 100%); margin: 0 auto; padding: 26px 28px 54px; }
  .heading { display: flex; align-items: end; justify-content: space-between; gap: 20px; margin-bottom: 20px; }
  .eyebrow { margin: 0 0 4px; color: #087f73; font-size: 10px; font-weight: 800; text-transform: uppercase; }
  h1 { margin: 0; font-size: 24px; letter-spacing: 0; }
  h2 { margin: 0; font-size: 13px; letter-spacing: 0; }
  .heading time, .subtle { color: #6a7874; font-size: 11px; }
  .back { display: inline-block; margin-bottom: 10px; color: #087f73; font-size: 11px; font-weight: 750; text-decoration: none; }
  .surface { margin-bottom: 18px; overflow: hidden; background: #fff; border: 1px solid #dbe3e0; border-radius: 6px; }
  .surface > header { display: flex; min-height: 46px; align-items: center; justify-content: space-between; gap: 12px; padding: 0 16px; border-bottom: 1px solid #e4e9e7; }
  .surface > header p { margin: 0; color: #6a7874; font-size: 10px; }
  .table-scroll { max-width: 100%; overflow-x: auto; }
  table { width: 100%; min-width: 760px; border-collapse: collapse; table-layout: fixed; }
  th { height: 36px; padding: 0 12px; background: #f8faf9; color: #6a7874; font-size: 9px; text-align: left; text-transform: uppercase; }
  td { min-height: 48px; padding: 10px 12px; border-top: 1px solid #e6ebe9; font-size: 11px; line-height: 1.45; vertical-align: top; overflow-wrap: anywhere; }
  tbody tr:hover { background: #f5f9f7; }
  td a { color: #087f73; font-weight: 750; text-decoration: none; }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 10px; }
  .empty { padding: 38px 24px; color: #6a7874; font-size: 12px; text-align: center; }
  .error { padding: 16px; background: #fbe7e9; color: #a72f3b; border: 1px solid #e9b8bd; border-radius: 5px; font-size: 12px; }
  .status { display: inline-flex; max-width: 100%; min-height: 22px; align-items: center; padding: 3px 7px; border-radius: 4px; background: #edf2f0; color: #4f5f5b; font-size: 9px; font-weight: 750; text-transform: uppercase; overflow-wrap: anywhere; }
  .status-normal, .status-complete { background: #e4f2ee; color: #087f73; }
  .status-warning { background: #fff0d7; color: #8b4b05; }
  .status-critical { background: #fbe7e9; color: #a72f3b; }
  .fact-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); background: #fff; border: 1px solid #dbe3e0; border-radius: 6px; margin-bottom: 18px; }
  .fact-grid > div { min-width: 0; min-height: 86px; padding: 15px 16px; border-right: 1px solid #e4e9e7; }
  .fact-grid > div:last-child { border-right: 0; }
  .fact-grid span { display: block; margin-bottom: 5px; color: #6a7874; font-size: 9px; font-weight: 750; text-transform: uppercase; }
  .fact-grid b { display: block; font-size: 13px; line-height: 1.45; overflow-wrap: anywhere; }
  .detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .detail-grid > div { min-width: 0; padding: 14px 16px; border-right: 1px solid #e4e9e7; border-bottom: 1px solid #e4e9e7; }
  .detail-grid > div:nth-child(even) { border-right: 0; }
  .detail-grid > div.full { grid-column: 1 / -1; border-right: 0; }
  .detail-grid dt { margin-bottom: 5px; color: #6a7874; font-size: 9px; font-weight: 750; text-transform: uppercase; }
  .detail-grid dd { margin: 0; font-size: 11px; line-height: 1.55; white-space: pre-wrap; overflow-wrap: anywhere; }
  .chips { display: flex; flex-wrap: wrap; gap: 5px; }
  .chip { padding: 3px 6px; background: #edf2f0; border-radius: 4px; font-size: 9px; font-weight: 700; }
  .timeline { margin: 0; padding: 0; list-style: none; }
  .timeline li { display: grid; grid-template-columns: 150px 120px minmax(0, 1fr); gap: 12px; padding: 13px 16px; border-top: 1px solid #e6ebe9; }
  .timeline li:first-child { border-top: 0; }
  .timeline time { color: #6a7874; font-size: 10px; }
  .timeline p { margin: 0; font-size: 11px; line-height: 1.55; white-space: pre-wrap; overflow-wrap: anywhere; }
  .references { display: block; margin-top: 5px; color: #6a7874; font-size: 9px; }
  .record { padding: 15px 16px; border-top: 1px solid #e6ebe9; }
  .record:first-of-type { border-top: 0; }
  .record-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
  .record-head b { font-size: 11px; }
  .record-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
  .record-grid > div { min-width: 0; }
  .record-grid span { display: block; margin-bottom: 4px; color: #6a7874; font-size: 9px; font-weight: 750; text-transform: uppercase; }
  .record-grid p { margin: 0; font-size: 10px; line-height: 1.5; white-space: pre-wrap; overflow-wrap: anywhere; }
  .exports { display: flex; flex-wrap: wrap; gap: 7px; }
  .button-link { display: inline-flex; min-height: 30px; align-items: center; padding: 0 9px; border: 1px solid #cbd6d2; border-radius: 5px; background: #fff; color: #31423e; font-size: 10px; font-weight: 750; text-decoration: none; }
  .button-link:hover { border-color: #087f73; color: #087f73; }
  @media (max-width: 820px) {
    .topbar-inner { align-items: flex-start; flex-wrap: wrap; gap: 0 14px; padding: 11px 14px 0; }
    .brand { min-height: 36px; }
    .readonly { min-height: 22px; margin-top: 7px; }
    nav { width: 100%; height: 42px; order: 3; overflow-x: auto; }
    nav a { flex: 0 0 auto; }
    main { padding: 20px 12px 42px; }
    .heading { align-items: flex-start; flex-direction: column; }
    .fact-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .fact-grid > div:nth-child(2) { border-right: 0; }
    .fact-grid > div:nth-child(-n+2) { border-bottom: 1px solid #e4e9e7; }
    .timeline li { grid-template-columns: 1fr; gap: 5px; }
    .record-grid { grid-template-columns: 1fr; }
  }
  @media (max-width: 520px) {
    h1 { font-size: 21px; }
    .fact-grid, .detail-grid { grid-template-columns: 1fr; }
    .fact-grid > div { border-right: 0; border-bottom: 1px solid #e4e9e7; }
    .fact-grid > div:last-child { border-bottom: 0; }
    .detail-grid > div, .detail-grid > div:nth-child(even) { border-right: 0; }
    .detail-grid > div.full { grid-column: auto; }
    .surface > header { align-items: flex-start; flex-direction: column; padding: 12px 14px; }
  }
</style>
{{end}}

{{define "operations-header"}}
<header class="topbar">
  <div class="topbar-inner">
    <a class="brand" href="/admin/queues/"><i>F</i> fidibeki operations</a>
    <nav aria-label="Platform operations">
      <a href="/admin/queues/">Queue operations</a>
      <a href="/admin/operations/privacy-incidents" aria-current="page">Privacy incidents</a>
    </nav>
    <span class="readonly">Read only</span>
  </div>
</header>
{{end}}

{{define "incidents-list"}}
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Privacy incidents · Fidibeki</title>{{template "operations-styles"}}</head>
<body>
  {{template "operations-header"}}
  <main>
    <div class="heading">
      <div><p class="eyebrow">Platform admin</p><h1>Privacy incidents</h1></div>
      <time>Refreshed {{.GeneratedAt}}</time>
    </div>
    {{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{else}}
    <section class="surface">
      <header><h2>Restricted breach register</h2><p>{{countLabel (len .Incidents) "incident" "incidents"}}</p></header>
      {{if .Incidents}}
      <div class="table-scroll">
        <table>
          <thead><tr><th style="width:14%">Incident</th><th style="width:16%">Awareness</th><th style="width:17%">Authority clock</th><th style="width:11%">Role</th><th style="width:22%">Impact</th><th style="width:10%">Tenants</th><th>Environment</th></tr></thead>
          <tbody>{{range .Incidents}}<tr>
            <td><a href="/admin/operations/privacy-incidents/{{.Incident.ID}}">{{.Incident.PublicID}}</a></td>
            <td><time>{{time .Incident.AwarenessAt}}</time></td>
            <td><span class="status status-{{.DeadlineTone}}">{{.DeadlineLabel}}</span></td>
            <td><span class="status status-neutral">{{label .Incident.ControllerRole}}</span></td>
            <td>{{.ImpactLabel}}</td><td>{{len .Incident.AffectedTenantIDs}}</td>
            <td>{{join .Incident.AffectedEnvironments ", "}}</td>
          </tr>{{end}}</tbody>
        </table>
      </div>
      {{else}}<div class="empty">No privacy incidents have been recorded.</div>{{end}}
    </section>
    {{end}}
  </main>
</body>
</html>
{{end}}

{{define "incident-detail"}}
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Incident.PublicID}} · Privacy incidents · Fidibeki</title>{{template "operations-styles"}}</head>
<body>
  {{template "operations-header"}}
  <main>
    <a class="back" href="/admin/operations/privacy-incidents">&larr; Privacy incidents</a>
    <div class="heading">
      <div><p class="eyebrow">Restricted breach record</p><h1>{{.Incident.PublicID}}</h1></div>
      <div class="exports"><a class="button-link" href="{{.PostIncidentJSONURL}}">Download JSON</a><a class="button-link" href="{{.PostIncidentMDURL}}">Download Markdown</a></div>
    </div>
    <section class="fact-grid" aria-label="Incident summary">
      <div><span>Awareness</span><b>{{time .Incident.AwarenessAt}}</b></div>
      <div><span>Authority deadline</span><b>{{time .Incident.AuthorityNotificationDeadlineAt}}</b></div>
      <div><span>Clock status</span><b><span class="status status-{{.DeadlineTone}}">{{.DeadlineLabel}}</span></b></div>
      <div><span>Controller role</span><b>{{label .Incident.ControllerRole}}</b></div>
    </section>

    <section class="surface">
      <header><h2>Incident facts</h2><p>Created {{time .Incident.CreatedAt}}</p></header>
      <dl class="detail-grid">
        <div><dt>Detection source</dt><dd>{{label .Incident.DetectionSource}}</dd></div>
        <div><dt>Impact dimensions</dt><dd>{{.ImpactLabel}}</dd></div>
        <div><dt>Affected environments</dt><dd><span class="chips">{{range .Incident.AffectedEnvironments}}<span class="chip">{{label .}}</span>{{end}}</span></dd></div>
        <div><dt>Affected systems</dt><dd><span class="chips">{{range .Incident.AffectedSystemCategories}}<span class="chip">{{label .}}</span>{{end}}</span></dd></div>
        <div><dt>Data categories</dt><dd><span class="chips">{{range .Incident.AffectedDataCategories}}<span class="chip">{{label .}}</span>{{end}}</span></dd></div>
        <div><dt>Subject categories</dt><dd><span class="chips">{{range .Incident.AffectedSubjectCategories}}<span class="chip">{{label .}}</span>{{end}}</span></dd></div>
        <div class="full"><dt>Affected tenant IDs</dt><dd class="mono">{{join .Incident.AffectedTenantIDs ", "}}</dd></div>
        <div class="full"><dt>Containment summary</dt><dd>{{.Incident.ContainmentSummary}}</dd></div>
        <div><dt>Created by platform admin</dt><dd class="mono">{{.Incident.CreatedByPlatformAdminID}}</dd></div>
        <div><dt>Internal incident ID</dt><dd class="mono">{{.Incident.ID}}</dd></div>
      </dl>
    </section>

    <section class="surface">
      <header><h2>Assessment history</h2><p>{{countLabel (len .Assessments) "snapshot" "snapshots"}}</p></header>
      {{if .Assessments}}{{range .Assessments}}<article class="record">
        <div class="record-head"><b>Assessment {{time .AssessedAt}}</b><span class="mono">{{.ID}}</span></div>
        <div class="record-grid">
          <div><span>Authority decision</span><p><span class="status status-{{statusClass .AuthorityNotificationDecision}}">{{label .AuthorityNotificationDecision}}</span><br>{{.AuthorityNotificationReasoning}}</p></div>
          <div><span>Risk to individuals</span><p><span class="status status-{{statusClass .HighRiskToIndividuals}}">{{label .HighRiskToIndividuals}}</span><br>{{.HighRiskReasoning}}</p></div>
          <div><span>Affected-person communication</span><p><span class="status status-{{statusClass .DataSubjectCommunicationDecision}}">{{label .DataSubjectCommunicationDecision}}</span><br>{{.DataSubjectCommunicationReasoning}}</p></div>
          <div><span>Estimated subjects</span><p>{{estimate .EstimatedAffectedSubjectsMin .EstimatedAffectedSubjectsMax}}</p></div>
          <div><span>Estimated records</span><p>{{estimate .EstimatedAffectedRecordsMin .EstimatedAffectedRecordsMax}}</p></div>
          <div><span>Assessed by</span><p class="mono">{{.AssessedByPlatformAdminID}}</p></div>
          <div><span>Likely consequences</span><p>{{optionalString .LikelyConsequences}}</p></div>
          <div><span>Recovery measures</span><p>{{optionalString .RecoveryMeasures}}</p></div>
          <div><span>Evidence references</span><p class="mono">{{if .EvidenceReferences}}{{join .EvidenceReferences ", "}}{{else}}None recorded{{end}}</p></div>
        </div>
      </article>{{end}}{{else}}<div class="empty">No controller assessment has been recorded.</div>{{end}}
    </section>

    <section class="surface">
      <header><h2>Current actions</h2><p>{{countLabel (len .Actions) "control or follow-up" "controls and follow-ups"}}</p></header>
      {{if .Actions}}<div class="table-scroll"><table>
        <thead><tr><th style="width:24%">Action</th><th style="width:12%">Kind</th><th style="width:14%">Status</th><th style="width:18%">Owner</th><th style="width:17%">Due</th><th>Evidence</th></tr></thead>
        <tbody>{{range .Actions}}<tr><td><b>{{.Title}}</b><br><span class="subtle">Revision {{.Revision}}</span></td><td>{{label .ActionKind}}</td><td><span class="status status-{{statusClass .Status}}">{{label .Status}}</span></td><td class="mono">{{optionalID .OwnerPlatformAdminID}}</td><td>{{optionalTime .DueAt}}</td><td class="mono">{{if .EvidenceReferences}}{{join .EvidenceReferences ", "}}{{else}}None{{end}}</td></tr>{{end}}</tbody>
      </table></div>{{else}}<div class="empty">No containment or post-incident actions are available.</div>{{end}}
    </section>

    <section class="surface">
      <header><h2>Action revision ledger</h2><p>{{countLabel (len .ActionEvents) "immutable revision" "immutable revisions"}}</p></header>
      {{if .ActionEvents}}<div class="table-scroll"><table>
        <thead><tr><th style="width:22%">Action ID</th><th style="width:10%">Revision</th><th style="width:14%">Status</th><th style="width:18%">Actor</th><th style="width:18%">Occurred</th><th>Outcome</th></tr></thead>
        <tbody>{{range .ActionEvents}}<tr><td class="mono">{{.ActionID}}</td><td>{{.Revision}}</td><td><span class="status status-{{statusClass .Status}}">{{label .Status}}</span></td><td class="mono">{{.ActorPlatformAdminID}}</td><td>{{time .OccurredAt}}</td><td>{{optionalString .OutcomeSummary}}</td></tr>{{end}}</tbody>
      </table></div>{{else}}<div class="empty">No action revisions are available.</div>{{end}}
    </section>

    <section class="surface">
      <header><h2>Controller notifications</h2><p>{{countLabel (len .Notifications) "frozen snapshot" "frozen snapshots"}}</p></header>
      {{if .Notifications}}{{range .Notifications}}<article class="record">
        <div class="record-head"><b>{{label .NotificationKind}} · {{.WorkspaceName}}</b><span class="status status-{{statusClass .Status}}">{{label .Status}}</span></div>
        <div class="record-grid">
          <div><span>Recipient</span><p>{{.RecipientEmail}}</p></div>
          <div><span>Tenant</span><p class="mono">{{.TenantID}}</p></div>
          <div><span>Sequence / template</span><p>{{.Sequence}} / v{{.TemplateVersion}}</p></div>
          <div><span>Summary</span><p>{{.Summary}}</p></div>
          <div><span>Measures taken</span><p>{{.MeasuresTaken}}</p></div>
          <div><span>Next update</span><p>{{optionalString .NextUpdate}}</p></div>
          <div><span>Delivery</span><p>{{.Attempts}} attempts · last {{optionalTime .LastAttemptAt}}<br>{{optionalString .LastError}}</p></div>
          <div><span>Provider evidence</span><p class="mono">{{optionalString .ProviderMessageID}}</p></div>
          <div><span>Exports</span><p><a class="button-link" href="{{notificationExportURL .BreachIncidentID .ID "json"}}">JSON</a> <a class="button-link" href="{{notificationExportURL .BreachIncidentID .ID "markdown"}}">Markdown</a></p></div>
        </div>
        {{if .DeliveryAttempts}}<div class="table-scroll"><table>
          <thead><tr><th>Attempt</th><th>Status</th><th>Attempted</th><th>Provider message</th><th>Error</th></tr></thead>
          <tbody>{{range .DeliveryAttempts}}<tr><td>{{.AttemptNumber}}</td><td><span class="status status-{{statusClass .Status}}">{{label .Status}}</span></td><td>{{time .AttemptedAt}}</td><td class="mono">{{optionalString .ProviderMessageID}}</td><td>{{optionalString .ErrorMessage}}</td></tr>{{end}}</tbody>
        </table></div>{{end}}
      </article>{{end}}{{else}}<div class="empty">No controller notification has been staged.</div>{{end}}
    </section>

    <section class="surface">
      <header><h2>External provider intakes</h2><p>{{countLabel (len .ProviderIntakes) "immutable notice" "immutable notices"}}</p></header>
      {{if .ProviderIntakes}}{{range .ProviderIntakes}}<article class="record">
        <div class="record-head"><b>{{.ProviderDisplayName}}</b><span class="mono">{{.ProviderNoticeReference}}</span></div>
        <div class="record-grid">
          <div><span>Provider / boundary</span><p>{{.ProviderID}} · {{label .BoundaryKind}}</p></div>
          <div><span>Governance snapshot</span><p>Registry v{{.GovernanceRegistryVersion}} · {{label .ApprovalStatus}} · {{label .ContractStatus}}</p></div>
          <div><span>Provider reported / received</span><p>{{time .ProviderReportedAt}}<br>{{time .ReceivedAt}}</p></div>
          <div><span>Summary</span><p>{{.Summary}}</p></div>
          <div><span>Data-flow categories</span><p>{{join .AffectedDataFlowCategories ", "}}</p></div>
          <div><span>Affected tenant IDs</span><p class="mono">{{join .AffectedTenantIDs ", "}}</p></div>
          <div><span>Primary retention category</span><p>{{.PrimaryRetentionCategory}}</p></div>
          <div><span>Evidence references</span><p class="mono">{{if .EvidenceReferences}}{{join .EvidenceReferences ", "}}{{else}}None recorded{{end}}</p></div>
          <div><span>Recorded by</span><p class="mono">{{.ActorPlatformAdminID}}</p></div>
        </div>
      </article>{{end}}{{else}}<div class="empty">No external provider notice has been recorded.</div>{{end}}
    </section>

    <section class="surface">
      <header><h2>Immutable chronology</h2><p>{{countLabel (len .Chronology) "entry" "entries"}}</p></header>
      {{if .Chronology}}<ol class="timeline">{{range .Chronology}}<li>
        <time>{{time .OccurredAt}}</time>
        <div><span class="status status-neutral">{{label .EntryKind}}</span></div>
        <p>{{.Summary}}{{if .EvidenceReferences}}<span class="references mono">Evidence: {{join .EvidenceReferences ", "}}</span>{{end}}</p>
      </li>{{end}}</ol>{{else}}<div class="empty">No chronology entry has been recorded.</div>{{end}}
    </section>
  </main>
</body>
</html>
{{end}}

{{define "operations-error"}}
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}} · Fidibeki</title>{{template "operations-styles"}}</head>
<body>
  {{template "operations-header"}}
  <main><div class="heading"><div><p class="eyebrow">Platform admin</p><h1>{{.Title}}</h1></div></div><p class="error" role="alert">{{.Message}}</p></main>
</body>
</html>
{{end}}
`
