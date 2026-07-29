package emailfeedback

import (
	"errors"
	"net/http"
)

func (handler *httpHandler) createInvitation(w http.ResponseWriter, r *http.Request) {
	var input InvitationInput
	if !decodeJSON(w, r, &input) {
		return
	}
	values, fields := ValidateInvitation(input)
	if len(fields) > 0 {
		writeValidation(w, fields)
		return
	}
	resolved, query, claims, ok := requestState(w, r)
	if !ok {
		return
	}
	invitation, created, err := handler.options.Store.AcceptInvitation(
		r.Context(), query, resolved.ID, claims.Subject, values,
		handler.options.InvitationTokens, handler.now(),
	)
	if err != nil {
		writeInvitationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeInvitationJSON(w, status, invitation)
}

func writeInvitationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeNotFound(w)
	case errors.Is(err, ErrInvitationConflict):
		writeConflict(w, "external_event_id was already used with different input")
	case errors.Is(err, ErrCampaignInactive):
		writeValidation(w, FieldErrors{
			"campaign_id": "must identify an active, ready email feedback campaign",
		})
	case errors.Is(err, ErrLocaleUnavailable):
		writeValidation(w, FieldErrors{
			"locale": "must identify a published campaign localization",
		})
	default:
		writeServiceUnavailable(w)
	}
}
