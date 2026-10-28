package feedback

import (
	"testing"

	"github.com/fidibeki/app.fidibeki/backend/internal/moderation"
)

func TestFinalizeTenantFeedbackRedactsFlaggedReporterContent(t *testing.T) {
	t.Parallel()
	raw := "an abusive raw report"
	cleaned := "a cleaned but still moderated report"
	comment := "more abusive context"
	suggested := "Checkout feedback needs review"
	item := Item{
		Status: StatusOpen, Title: raw, Description: &raw, Text: &raw, TextCleaned: &cleaned,
		AISuggestedTitle: &suggested, Comment: &comment,
		Responses:           map[string]any{"details": raw, "reason": "slow"},
		TextModerationFlags: []string{"profanity"},
	}

	finalizeTenantFeedback(&item, nil)

	if item.Title != suggested || item.Description == nil || *item.Description != moderation.Placeholder ||
		item.Text == nil || *item.Text != moderation.Placeholder ||
		item.TextCleaned == nil || *item.TextCleaned != moderation.Placeholder ||
		item.Comment == nil || *item.Comment != moderation.Placeholder ||
		item.Responses["details"] != moderation.Placeholder || item.Responses["reason"] != "slow" {
		t.Fatalf("moderated feedback = %#v", item)
	}
}

func TestFinalizeTenantFeedbackRedactsSpamWithoutAIFlags(t *testing.T) {
	t.Parallel()
	raw := "bulk spam"
	item := Item{Status: StatusSpam, Title: raw, Description: &raw, Text: &raw, Responses: map[string]any{"details": raw}}
	finalizeTenantFeedback(&item, nil)
	if item.Title != moderation.Placeholder || item.Responses["details"] != moderation.Placeholder {
		t.Fatalf("spam feedback = %#v", item)
	}
}

func TestModerateAttachmentTranscription(t *testing.T) {
	t.Parallel()
	transcription := "unsafe transcript"
	attachment := AttachmentDetail{Transcription: &transcription, ModerationFlags: []string{"harassment"}}
	moderateAttachment(&attachment)
	if attachment.Transcription == nil || *attachment.Transcription != moderation.Placeholder {
		t.Fatalf("moderated attachment = %#v", attachment)
	}
}
