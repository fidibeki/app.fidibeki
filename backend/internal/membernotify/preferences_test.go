package membernotify

import (
	"encoding/json"
	"testing"
)

func TestDefaultsAreLowNoise(t *testing.T) {
	t.Parallel()
	defaults := Defaults()
	if defaults.Assignments != ModeImmediate || defaults.Mentions != ModeImmediate {
		t.Fatalf(
			"directly addressed defaults = %q/%q, want immediate",
			defaults.Assignments, defaults.Mentions,
		)
	}
	if defaults.Comments != ModeOff {
		t.Fatalf("comments default = %q, want off", defaults.Comments)
	}
	for _, category := range []string{CategoryIssueChanges, CategoryFeedbackSummary} {
		if mode := defaults.Mode(category); mode == ModeImmediate {
			t.Fatalf("%s default = immediate, want a digest or off", category)
		}
	}
}

// A per-submission email is unreachable by configuration, not merely off by
// default: the feedback summary has no immediate mode at all.
func TestFeedbackSummaryCannotBeImmediate(t *testing.T) {
	t.Parallel()
	for _, mode := range SupportedModes(CategoryFeedbackSummary) {
		if mode == ModeImmediate {
			t.Fatal("feedback summary must not offer immediate delivery")
		}
	}
	_, fields := Apply(Defaults(), Patch{FeedbackSummary: stringPointer(ModeImmediate)})
	if fields[CategoryFeedbackSummary] == "" {
		t.Fatalf("immediate feedback summary was accepted, fields = %v", fields)
	}
}

func TestMandatoryClassesAreNotAddressable(t *testing.T) {
	t.Parallel()
	for _, class := range MandatoryClasses {
		if IsOptionalCategory(class) {
			t.Fatalf("mandatory class %q is exposed as an optional category", class)
		}
		if _, ok := patchFields[class]; ok {
			t.Fatalf("mandatory class %q is settable through a preference patch", class)
		}
	}
	var patch Patch
	if err := json.Unmarshal([]byte(`{"billing":"off"}`), &patch); err == nil {
		t.Fatal("a patch naming a mandatory class was accepted")
	}
}

func TestPatchRejectsUnknownFieldsAndInvalidValues(t *testing.T) {
	t.Parallel()
	var patch Patch
	if err := json.Unmarshal([]byte(`{"digest_hour":9,"unknown":true}`), &patch); err == nil {
		t.Fatal("unknown patch field was accepted")
	}
	cases := map[string]Patch{
		CategoryMentions: {Mentions: stringPointer("sometimes")},
		"digest_hour":    {DigestHour: intPointer(24)},
		"digest_weekday": {DigestWeekday: intPointer(0)},
	}
	for field, invalid := range cases {
		next, fields := Apply(Defaults(), invalid)
		if fields[field] == "" {
			t.Fatalf("%s accepted an invalid value, fields = %v", field, fields)
		}
		if next != Defaults() {
			t.Fatalf("%s applied a partial change on failure: %+v", field, next)
		}
	}
}

func TestApplyKeepsUntouchedFields(t *testing.T) {
	t.Parallel()
	current := Defaults()
	current.Comments = ModeWeekly
	next, fields := Apply(current, Patch{IssueChanges: stringPointer(ModeOff)})
	if len(fields) > 0 {
		t.Fatalf("valid patch reported fields %v", fields)
	}
	if next.Comments != ModeWeekly || next.Mentions != current.Mentions {
		t.Fatalf("untouched preferences changed: %+v", next)
	}
	if next.IssueChanges != ModeOff {
		t.Fatalf("issue changes = %q, want off", next.IssueChanges)
	}
}

func TestTimezonePatchDistinguishesAbsentFromNull(t *testing.T) {
	t.Parallel()
	var absent Patch
	if err := json.Unmarshal([]byte(`{"digest_hour":7}`), &absent); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if _, set, _ := TimezonePatch(absent); set {
		t.Fatal("absent timezone was treated as a change")
	}
	var cleared Patch
	if err := json.Unmarshal([]byte(`{"timezone":null}`), &cleared); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	value, set, failure := TimezonePatch(cleared)
	if !set || value != nil || failure != "" {
		t.Fatalf("explicit null = (%v, %t, %q), want a cleared override", value, set, failure)
	}
	var invalid Patch
	if err := json.Unmarshal([]byte(`{"timezone":"Mars/Olympus"}`), &invalid); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if _, _, failure := TimezonePatch(invalid); failure == "" {
		t.Fatal("an unknown timezone was accepted")
	}
	var valid Patch
	if err := json.Unmarshal([]byte(`{"timezone":"Africa/Luanda"}`), &valid); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	value, set, failure = TimezonePatch(valid)
	if !set || failure != "" || value == nil || *value != "Africa/Luanda" {
		t.Fatalf("valid timezone = (%v, %t, %q)", value, set, failure)
	}
}

func TestCategoriesForCadence(t *testing.T) {
	t.Parallel()
	preferences := Preferences{
		Assignments: ModeImmediate, Mentions: ModeDaily, Comments: ModeOff,
		IssueChanges: ModeDaily, FeedbackSummary: ModeWeekly,
	}
	daily := preferences.CategoriesFor(CadenceDaily)
	if len(daily) != 2 || daily[0] != CategoryMentions || daily[1] != CategoryIssueChanges {
		t.Fatalf("daily categories = %v", daily)
	}
	if !preferences.UsesCadence(CadenceImmediate) || !preferences.UsesCadence(CadenceWeekly) {
		t.Fatal("configured cadences were reported as unused")
	}
	silent := Preferences{
		Assignments: ModeOff, Mentions: ModeOff, Comments: ModeOff,
		IssueChanges: ModeOff, FeedbackSummary: ModeOff,
	}
	for _, cadence := range Cadences {
		if silent.UsesCadence(cadence) {
			t.Fatalf("fully disabled preferences still use the %s cadence", cadence)
		}
	}
}

func TestAuditDataRecordsOnlyChanges(t *testing.T) {
	t.Parallel()
	before := Defaults()
	before.Timezone = "UTC"
	after := before
	after.Comments = ModeDaily
	after.DigestHour = 18
	after.Timezone = "Europe/Lisbon"
	changes := AuditData(before, after)
	if len(changes) != 3 {
		t.Fatalf("audit changes = %v, want comments, digest_hour, and timezone", changes)
	}
	if _, ok := changes[CategoryMentions]; ok {
		t.Fatalf("unchanged category recorded: %v", changes)
	}
	if len(AuditData(before, before)) != 0 {
		t.Fatal("an unchanged save recorded changes")
	}
}

func stringPointer(value string) *string { return &value }

func intPointer(value int) *int { return &value }
