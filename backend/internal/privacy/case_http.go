package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/api"
	"github.com/acidiney/app.fidibeki/backend/internal/email"
	appmiddleware "github.com/acidiney/app.fidibeki/backend/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (handler *HTTPHandler) createCase(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	var body struct {
		SubjectType     string         `json:"subject_type"`
		RightType       string         `json:"right_type"`
		ControllerRole  string         `json:"controller_role"`
		SubjectEmail    string         `json:"subject_email"`
		SubjectMemberID string         `json:"subject_member_id"`
		OwnerMemberID   string         `json:"owner_member_id"`
		Scope           map[string]any `json:"scope"`
	}
	if !decodeCaseJSON(w, r, &body) {
		return
	}
	subjectType, err := ParseSubjectType(body.SubjectType)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	rightType, err := ParseRightType(body.RightType)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	controllerRole, err := ParseControllerRole(body.ControllerRole)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	privacyCase, token, shouldEmail, err := handler.options.Store.CreateCase(
		r.Context(), query, resolved.ID, claims.Subject, r.Header.Get("Idempotency-Key"),
		CreateCaseInput{
			SubjectType: subjectType, RightType: rightType, ControllerRole: controllerRole,
			SubjectEmail: body.SubjectEmail, SubjectMemberID: body.SubjectMemberID,
			OwnerMemberID: body.OwnerMemberID, Scope: body.Scope,
		}, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	if shouldEmail {
		message, composeErr := privacyCaseVerificationMessage(
			privacyCase, resolved.Name, resolved.Slug, token, handler.options,
		)
		if composeErr != nil || handler.options.Email == nil ||
			!appmiddleware.AfterTenantCommit(r.Context(), func(ctx context.Context) error {
				return handler.options.Email.Enqueue(ctx, message)
			}) {
			if composeErr == nil {
				composeErr = errors.New("privacy case verification email is unavailable")
			}
			handler.writeError(w, r, composeErr)
			return
		}
	}
	api.WriteJSON(w, http.StatusAccepted, map[string]any{
		"case": privacyCase, "verification_email_queued": shouldEmail,
	})
}

func (handler *HTTPHandler) listCases(w http.ResponseWriter, r *http.Request) {
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	cases, err := handler.options.Store.ListCases(r.Context(), query, resolved.ID, 100)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"cases": cases})
}

func (handler *HTTPHandler) caseDashboard(w http.ResponseWriter, r *http.Request) {
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	dashboard, err := handler.options.Store.CaseDashboard(
		r.Context(), query, resolved.ID, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"dashboard": dashboard})
}

func (handler *HTTPHandler) getCase(w http.ResponseWriter, r *http.Request) {
	caseID, ok := privacyCaseID(w, r)
	if !ok {
		return
	}
	resolved, query, _, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	privacyCase, err := handler.options.Store.GetCase(r.Context(), query, resolved.ID, caseID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	events, err := handler.options.Store.ListCaseEvents(r.Context(), query, resolved.ID, caseID)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"case": privacyCase, "events": events})
}

func (handler *HTTPHandler) transitionCase(w http.ResponseWriter, r *http.Request) {
	caseID, ok := privacyCaseID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	var body struct {
		Status           string `json:"status"`
		DecisionSummary  string `json:"decision_summary"`
		LegalReviewNotes string `json:"legal_review_notes"`
		ExceptionReason  string `json:"exception_reason"`
	}
	if !decodeCaseJSON(w, r, &body) {
		return
	}
	status, err := ParseCaseStatus(body.Status)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	privacyCase, err := handler.options.Store.TransitionCase(
		r.Context(), query, resolved.ID, caseID, claims.Subject,
		r.Header.Get("Idempotency-Key"), TransitionCaseInput{
			Status: status, DecisionSummary: body.DecisionSummary,
			LegalReviewNotes: body.LegalReviewNotes, ExceptionReason: body.ExceptionReason,
		}, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"case": privacyCase})
}

func (handler *HTTPHandler) setCaseRestriction(w http.ResponseWriter, r *http.Request) {
	caseID, ok := privacyCaseID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var body struct {
		Active bool   `json:"active"`
		Reason string `json:"reason"`
	}
	if !decodeCaseJSON(w, r, &body) {
		return
	}
	resolved, query, claims, ok := privacyRequestState(w, r)
	if !ok {
		return
	}
	restriction, err := handler.options.Store.SetCaseRestriction(
		r.Context(), query, resolved.ID, caseID, claims.Subject,
		r.Header.Get("Idempotency-Key"), body.Active, body.Reason, handler.now(),
	)
	if err != nil {
		handler.writeError(w, r, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"restriction": restriction})
}

func decodeCaseJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest, "request body is invalid", nil)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		api.WriteError(w, http.StatusBadRequest, api.CodeBadRequest,
			"request body must contain one JSON value", nil)
		return false
	}
	return true
}

func privacyCaseID(w http.ResponseWriter, r *http.Request) (string, bool) {
	caseID := strings.TrimSpace(chi.URLParam(r, "caseID"))
	if _, err := uuid.Parse(caseID); err != nil {
		api.WriteError(w, http.StatusNotFound, api.CodeNotFound, "privacy case not found", nil)
		return "", false
	}
	return caseID, true
}

func privacyCaseVerificationMessage(
	privacyCase PrivacyCase,
	workspaceName string,
	workspaceSlug string,
	token string,
	options HTTPOptions,
) (email.Message, error) {
	verificationURL, err := privacyCaseURL(
		options.PublicBaseURL, options.BaseDomain, workspaceSlug, token,
	)
	if err != nil {
		return email.Message{}, err
	}
	logoURL, err := email.BrandLogoURL(options.PublicBaseURL, options.BaseDomain)
	if err != nil {
		return email.Message{}, err
	}
	if privacyCase.SubjectEmail == nil {
		return email.Message{}, errors.New("privacy case reporter email is unavailable")
	}
	return email.Compose(email.Message{
		From: options.From, To: *privacyCase.SubjectEmail,
		Subject:        "Confirm your Fidibeki data-rights request",
		IdempotencyKey: "privacy-case-verification:" + privacyCase.ID,
	}, email.Content{
		Preheader: "Confirm your data-rights request",
		Heading:   "Confirm your request",
		Workspace: workspaceName,
		Body: []string{
			workspaceName + " recorded data-rights request " + privacyCase.PublicID + ".",
			"Use the link below within 48 hours. The request will not proceed until control of this email address is confirmed.",
		},
		Action:        &email.Link{Label: "Confirm request", URL: verificationURL},
		ShowActionURL: true,
		Footer:        "This message confirms identity only. It does not make an automatic legal determination.",
		LogoURL:       logoURL,
	})
}
