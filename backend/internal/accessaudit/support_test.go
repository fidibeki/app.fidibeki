package accessaudit

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSupportRequest(t *testing.T) {
	tests := []struct {
		name    string
		request SupportRequest
		want    SupportRequest
		wantErr bool
	}{
		{
			name:    "valid request is trimmed",
			request: SupportRequest{Reason: "  investigating reported checkout bug  ", CaseReference: " CASE-123 "},
			want:    SupportRequest{Reason: "investigating reported checkout bug", CaseReference: "CASE-123"},
		},
		{
			name:    "empty reason and case reference",
			request: SupportRequest{},
			wantErr: true,
		},
		{
			name:    "reason too short",
			request: SupportRequest{Reason: "too short", CaseReference: "CASE-123"},
			wantErr: true,
		},
		{
			name:    "case reference too short",
			request: SupportRequest{Reason: "investigating reported checkout bug", CaseReference: "ab"},
			wantErr: true,
		},
		{
			name:    "reason too long",
			request: SupportRequest{Reason: strings.Repeat("a", maxSupportReasonLength+1), CaseReference: "CASE-123"},
			wantErr: true,
		},
		{
			name:    "case reference too long",
			request: SupportRequest{Reason: "investigating reported checkout bug", CaseReference: strings.Repeat("a", maxSupportCaseReferenceLength+1)},
			wantErr: true,
		},
		{
			name:    "whitespace-only reason and case reference",
			request: SupportRequest{Reason: "                    ", CaseReference: "   "},
			wantErr: true,
		},
		{
			name:    "reason contains a control character",
			request: SupportRequest{Reason: "investigating\nreported checkout bug", CaseReference: "CASE-123"},
			wantErr: true,
		},
		{
			name:    "case reference contains a control character",
			request: SupportRequest{Reason: "investigating reported checkout bug", CaseReference: "CASE-\u007f123"},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ValidateSupportRequest(test.request)
			if test.wantErr {
				if !errors.Is(err, ErrSupportRequestInvalid) {
					t.Fatalf("ValidateSupportRequest() error = %v, want ErrSupportRequestInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateSupportRequest() unexpected error = %v", err)
			}
			if got != test.want {
				t.Fatalf("ValidateSupportRequest() = %+v, want %+v", got, test.want)
			}
		})
	}
}
