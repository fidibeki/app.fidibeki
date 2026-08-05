package feedback

import "testing"

func TestSessionFailureDetailUsesOnlySafeBoundedMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code      string
		retryable bool
		message   string
	}{
		{
			code:    "contract_violation",
			message: "The recorder format was not supported. The rejected replay data was deleted.",
		},
		{
			code:    "unsafe_payload",
			message: "The recorded data did not meet the replay safety requirements and was deleted.",
		},
		{
			code:    "upload_expired",
			message: "Part of the recording expired before its upload could be completed.",
		},
		{
			code: "processing_failed", retryable: true,
			message: "Replay processing failed after several attempts. The recorded data is still available.",
		},
		{
			code:    "unavailable",
			message: "No usable replay data is available for this session.",
		},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			failure := sessionFailureDetail(test.code, test.retryable)
			if failure == nil || string(failure.Code) != test.code || failure.Message != test.message ||
				failure.Retryable != test.retryable {
				t.Fatalf("sessionFailureDetail() = %#v", failure)
			}
		})
	}
	if failure := sessionFailureDetail("database said customer@example.com", true); failure != nil {
		t.Fatalf("unknown failure = %#v, want nil", failure)
	}
}
