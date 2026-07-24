package breachregister

import (
	"net/http"
	"strconv"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *httpHandler) now() time.Time {
	if h.options.Now != nil {
		return h.options.Now()
	}
	return time.Now()
}

func exportFormatFromRequest(r *http.Request) (ExportFormat, error) {
	return ParseExportFormat(r.URL.Query().Get("format"))
}

func (h *httpHandler) exportControllerNotification(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	notificationID := chi.URLParam(r, "notificationID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	if _, err := uuid.Parse(notificationID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	format, err := exportFormatFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	incident, _, _, _, _, notifications, err := h.options.Store.GetIncident(r.Context(), incidentID)
	if err != nil {
		writeError(w, err)
		return
	}
	var found *ControllerNotification
	for index := range notifications {
		if notifications[index].ID == notificationID {
			found = &notifications[index]
			break
		}
	}
	if found == nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	export := newNotificationExport(h.now(), incident.PublicID, *found)
	filename := "breach-" + incident.PublicID + "-notification-" +
		string(export.Notification.NotificationKind) + "-" + strconv.Itoa(export.Notification.Sequence)
	writeExport(w, r, format, filename, export, renderNotificationMarkdown)
}

func (h *httpHandler) exportPostIncidentReport(w http.ResponseWriter, r *http.Request) {
	incidentID := chi.URLParam(r, "incidentID")
	if _, err := uuid.Parse(incidentID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "breach incident not found", nil)
		return
	}
	format, err := exportFormatFromRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}
	incident, chronology, assessments, actions, actionEvents, notifications, err :=
		h.options.Store.GetIncident(r.Context(), incidentID)
	if err != nil {
		writeError(w, err)
		return
	}
	report := newPostIncidentReport(
		h.now(), incident, chronology, assessments, actions, actionEvents, notifications,
	)
	filename := "breach-" + incident.PublicID + "-post-incident-report"
	writeExport(w, r, format, filename, report, renderPostIncidentReportMarkdown)
}

// writeExport renders value as JSON or, via markdown, as Markdown, sets safe
// download headers, and writes the body -- unless the request is a HEAD,
// which gets identical headers with no body so GET/HEAD stay consistent.
func writeExport[T any](
	w http.ResponseWriter,
	r *http.Request,
	format ExportFormat,
	filenameStem string,
	value T,
	markdown func(T) string,
) {
	var body []byte
	var contentType, extension string
	switch format {
	case ExportFormatMarkdown:
		contentType = "text/markdown; charset=utf-8"
		extension = ".md"
		body = []byte(markdown(value))
	default:
		encoded, err := renderJSON(value)
		if err != nil {
			api.WriteError(w, http.StatusServiceUnavailable, api.CodeServiceUnavailable, "service unavailable", nil)
			return
		}
		contentType = "application/json"
		extension = ".json"
		body = encoded
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filenameStem+extension+"\"")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}
