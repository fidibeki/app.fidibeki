package emailfeedback

import (
	"errors"
	"net/http"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/acidiney/app.fidibeki/backend/internal/feedback"
)

type collectionValidationError struct {
	fields []feedback.FieldError
}

func (err *collectionValidationError) Error() string {
	return "email feedback collection is invalid"
}

func (handler *publicHTTPHandler) loadCollection(w http.ResponseWriter, r *http.Request) {
	resolved, tokenHash, ok := handler.resolvePublicInvitation(w, r)
	if !ok {
		return
	}
	var collection InvitationCollection
	if err := handler.withTenant(r.Context(), resolved.TenantID, func(query db.Querier) error {
		loaded, err := handler.options.Store.LoadInvitationCollection(
			r.Context(), query, resolved, tokenHash, false,
		)
		if err != nil {
			return err
		}
		if !loaded.AvailableAt(handler.now()) {
			return ErrInvitationPublicToken
		}
		collection = loaded
		return nil
	}); err != nil {
		handler.writePublicError(w, r, err)
		return
	}
	writeInvitationNoStore(w)
	api.WriteJSON(w, http.StatusOK, map[string]any{
		"collection": collectionView(collection),
	})
}

func (handler *publicHTTPHandler) submitCollection(w http.ResponseWriter, r *http.Request) {
	var request feedback.EmailSubmissionRequest
	if !decodeJSON(w, r, &request) {
		writeInvitationNoStore(w)
		return
	}
	resolved, tokenHash, ok := handler.resolvePublicInvitation(w, r)
	if !ok {
		return
	}
	now := handler.now()
	var result feedback.CoreSubmissionResult
	err := handler.withTenant(r.Context(), resolved.TenantID, func(query db.Querier) error {
		collection, err := handler.options.Store.LoadInvitationCollection(
			r.Context(), query, resolved, tokenHash, true,
		)
		if err != nil {
			return err
		}
		if !collection.AvailableAt(now) {
			return ErrInvitationPublicToken
		}
		configuration := collection.Configuration()
		values, fields := feedback.ValidateEmailSubmissionAt(
			request, configuration, collection.ID,
			collection.WidgetRuleID, collection.ContextURL, collection.Locale, now,
		)
		if len(fields) > 0 {
			return &collectionValidationError{fields: fields}
		}
		core := handler.options.FeedbackCore
		core.Now = func() time.Time { return now }
		result, err = core.Submit(r.Context(), query, feedback.CoreSubmissionRequest{
			Configuration: configuration,
			Values:        values,
			Ownership: feedback.SubmissionOwnership{
				Source: "email", AllowExisting: false,
			},
		})
		if err != nil {
			return err
		}
		return handler.options.Store.MarkInvitationSubmitted(
			r.Context(), query, collection, result.Submission.ID, now,
		)
	})
	if err != nil {
		handler.writeCollectionError(w, r, err)
		return
	}
	writeInvitationNoStore(w)
	api.WriteJSON(w, http.StatusCreated, map[string]any{
		"feedback_public_id": result.Submission.PublicID,
		"submitted":          true,
	})
}

func (handler *publicHTTPHandler) writeCollectionError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	var validation *collectionValidationError
	var planLimit *feedback.PlanLimitError
	switch {
	case errors.Is(err, ErrInvitationPublicToken):
		handler.writePublicUnavailable(w)
	case errors.As(err, &validation):
		writeInvitationNoStore(w)
		api.WriteError(
			w, http.StatusUnprocessableEntity, api.CodeValidation,
			"feedback is invalid", validation.fields,
		)
	case errors.As(err, &planLimit):
		writeInvitationNoStore(w)
		api.WriteError(
			w, http.StatusPaymentRequired, api.CodePlanLimitExceeded,
			"monthly feedback limit reached",
			map[string]int{"limit": planLimit.Limit, "usage": planLimit.Usage},
		)
	default:
		handler.writePublicError(w, r, err)
	}
}
