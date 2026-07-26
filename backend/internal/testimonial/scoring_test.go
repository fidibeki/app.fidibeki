package testimonial

import (
	"math"
	"slices"
	"testing"
)

// glowing is a realistic strong candidate: praise, a concrete outcome, inside the
// ideal length band. Individual tests mutate one field at a time so a failure
// points at one signal.
func glowing() Input {
	rating := 5
	sentiment := 0.9
	return Input{
		Text: "We rolled Fidibeki out across three teams and it cut our feedback triage " +
			"from 3 days to about 20 minutes per week. Support answered every question " +
			"within an hour and the migration lost nothing.",
		Rating:           &rating,
		FeedbackStatus:   "open",
		SentimentScore:   &sentiment,
		ModerationStatus: ModerationCompleted,
		Actionability:    "not_actionable",
		AuthorReachable:  true,
		Language:         "en",
		SupportedLocales: []string{"en", "pt"},
	}
}

func TestScoreRejectsIneligibleFeedback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Input)
		reason string
	}{
		{
			name:   "spam is never a testimonial",
			mutate: func(in *Input) { in.FeedbackStatus = "spam" },
			reason: ReasonNotOpen,
		},
		{
			name:   "archived feedback is out of the queue",
			mutate: func(in *Input) { in.FeedbackStatus = "archived" },
			reason: ReasonNotOpen,
		},
		{
			// The gate that matters most: a glowing quote containing an order
			// number or an email address must never be suggested for publication.
			name:   "pii flag blocks publication outright",
			mutate: func(in *Input) { in.ModerationFlags = []string{"pii"} },
			reason: ReasonModerationFlagged,
		},
		{
			name:   "hate flag blocks publication outright",
			mutate: func(in *Input) { in.ModerationFlags = []string{"hate"} },
			reason: ReasonModerationFlagged,
		},
		{
			name:   "text below the floor cannot carry a testimonial",
			mutate: func(in *Input) { in.Text = "Nice!" },
			reason: ReasonTextTooShort,
		},
		{
			// Without this gate a sarcastic 1-star with good length and
			// specificity ranks as a top suggestion.
			name:   "one star means the author is unhappy",
			mutate: func(in *Input) { rating := 1; in.Rating = &rating },
			reason: ReasonLowRating,
		},
		{
			name:   "two stars means the author is unhappy",
			mutate: func(in *Input) { rating := 2; in.Rating = &rating },
			reason: ReasonLowRating,
		},
		{
			name:   "already converted",
			mutate: func(in *Input) { in.HasTestimonial = true },
			reason: ReasonAlreadyTestimonial,
		},
		{
			name:   "dismissed means never suggest again",
			mutate: func(in *Input) { in.Dismissed = true },
			reason: ReasonDismissed,
		},
		{
			name:   "a language we cannot render is not a candidate",
			mutate: func(in *Input) { in.Language = "ja" },
			reason: ReasonUnsupportedLanguage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := glowing()
			test.mutate(&in)
			got := Score(in)
			if got.Eligibility != Ineligible {
				t.Fatalf("eligibility = %q, want %q (assessment %#v)", got.Eligibility, Ineligible, got)
			}
			if got.BaseScore != 0 {
				t.Errorf("base score = %v, want 0 for an ineligible candidate", got.BaseScore)
			}
			if len(got.Reasons) != 0 {
				t.Errorf("reasons = %#v, want none for an ineligible candidate", got.Reasons)
			}
			if !slices.Contains(got.IneligibleReasons, test.reason) {
				t.Errorf("ineligible reasons = %#v, want to contain %q", got.IneligibleReasons, test.reason)
			}
		})
	}
}

func TestScoreAcceptsAStrongCandidate(t *testing.T) {
	t.Parallel()
	got := Score(glowing())
	if got.Eligibility != Eligible {
		t.Fatalf("eligibility = %q, want %q (reasons %#v)", got.Eligibility, Eligible, got.IneligibleReasons)
	}
	if got.BaseScore <= 0.7 {
		t.Errorf("base score = %v, want a strong candidate to clear 0.7", got.BaseScore)
	}
	if got.ScoreVersion != ScoreVersion {
		t.Errorf("score version = %d, want %d", got.ScoreVersion, ScoreVersion)
	}
}

// SentimentScore is validated to [-1, 1] by backend/internal/ai. Feeding it in
// raw would treat a complaint as a mildly positive signal, so the transform to
// [0, 1] is explicit and pinned here.
func TestScoreTransformsSentimentFromTheAIRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  float64
		want float64
	}{
		{name: "most negative maps to zero", raw: -1, want: 0},
		{name: "neutral maps to the midpoint", raw: 0, want: 0.5},
		{name: "most positive maps to one", raw: 1, want: 1},
		{name: "negative stays below the midpoint", raw: -0.8, want: 0.1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := glowing()
			in.SentimentScore = &test.raw
			got := Score(in).Signals[SignalSentiment]
			if math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("sentiment signal = %v, want %v", got, test.want)
			}
		})
	}
}

// The sarcasm case: five stars with hostile tone must not outrank five stars
// with warm tone.
func TestScoreDoesNotRewardSarcasticPraise(t *testing.T) {
	t.Parallel()
	warm, hostile := glowing(), glowing()
	positive, negative := 0.9, -0.8
	warm.SentimentScore, hostile.SentimentScore = &positive, &negative
	warmScore, hostileScore := Score(warm).BaseScore, Score(hostile).BaseScore
	if hostileScore >= warmScore {
		t.Fatalf("hostile tone scored %v, warm tone %v; hostile must rank lower", hostileScore, warmScore)
	}
}

func TestScoreRewardsConcreteOutcomes(t *testing.T) {
	t.Parallel()
	vague, concrete := glowing(), glowing()
	vague.Text = "We really like this product and the team behind it is genuinely lovely to deal with."
	concrete.Text = "We cut our triage time from 3 days to 20 minutes, a 95% reduction, in the first week."
	vagueAssessment, concreteAssessment := Score(vague), Score(concrete)
	if concreteAssessment.Signals[SignalSpecificity] <= vagueAssessment.Signals[SignalSpecificity] {
		t.Fatalf("specificity: concrete = %v, vague = %v; concrete must be higher",
			concreteAssessment.Signals[SignalSpecificity], vagueAssessment.Signals[SignalSpecificity])
	}
	if concreteAssessment.BaseScore <= vagueAssessment.BaseScore {
		t.Errorf("base score: concrete = %v, vague = %v", concreteAssessment.BaseScore, vagueAssessment.BaseScore)
	}
}

// Portuguese is a first-class locale here, so specificity detection cannot be
// English-only.
func TestScoreDetectsConcreteOutcomesInPortuguese(t *testing.T) {
	t.Parallel()
	in := glowing()
	in.Language = "pt"
	in.Text = "Reduzimos a triagem de feedback de 3 dias para 20 minutos por semana, " +
		"menos 90% do tempo, e a equipa respondeu a tudo em menos de uma hora."
	if got := Score(in); got.Signals[SignalSpecificity] == 0 {
		t.Fatalf("specificity = 0 for a Portuguese quote full of concrete numbers (assessment %#v)", got)
	}
}

// Vague praise must score zero specificity in every language we serve.
//
// The first implementation counted bare "to", "from", "more" and "less" as
// comparison markers. Those words appear in most English sentences and in
// far fewer Portuguese ones, so English fluff scored 0.286 while equivalent
// Portuguese fluff scored 0.000 — a systematic ranking bias in favour of
// English, in the exact signal that was supposed to be language-neutral.
func TestScoreGivesVaguePraiseNoSpecificityInEitherLanguage(t *testing.T) {
	t.Parallel()
	vague := map[string]string{
		"en plain":      "We really like this product and the team behind it is genuinely lovely.",
		"en with to":    "We really like this product and it is lovely to deal with the team.",
		"en with more":  "We like it more than the alternative and everyone here is very happy.",
		"en with from":  "Everything we wanted from a tool, and the people are a pleasure as well.",
		"pt plain":      "Gostamos mesmo deste produto e a equipa por tras dele e muito simpatica.",
		"pt with mais":  "Gostamos mais deste produto do que do anterior e a equipa e simpatica.",
		"pt with menos": "Menos complicado do que esperavamos e a equipa toda e realmente simpatica.",
	}
	for name, text := range vague {
		in := glowing()
		in.Text = text
		if got := Score(in).Signals[SignalSpecificity]; got != 0 {
			t.Errorf("%s: specificity = %v, want 0 for vague praise (%q)", name, got, text)
		}
	}
}

// The parity guarantee, asserted across a range of shapes rather than claimed.
//
// The "de" of the Portuguese range pattern is a far commoner word than the
// English "from", so parity has to be checked on sentences that merely contain
// numbers as well as on genuine before-and-after claims — otherwise the fix for
// the English bias just introduces a Portuguese one.
func TestScoreSpecificityIsLanguageNeutral(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name       string
		english    string
		portuguese string
	}{
		{
			name:       "before and after claim",
			english:    "Onboarding fell from 3 days to 20 minutes across every single team.",
			portuguese: "O onboarding caiu de 3 dias para 20 minutos em todas as equipas.",
		},
		{
			name:       "counts without a range",
			english:    "There are 4 of us and we handle 300 reports a week without trouble.",
			portuguese: "Somos 4 pessoas e tratamos de 300 relatorios por semana sem problemas.",
		},
		{
			name:       "a bare number in vague praise",
			english:    "We like 2 things about it and the support team is genuinely great.",
			portuguese: "Gostamos de 2 coisas nele e a equipa de suporte e mesmo excelente.",
		},
		{
			name:       "tool consolidation",
			english:    "We moved from 2 tools to 1 and the team is happier about it now.",
			portuguese: "Passamos de 2 ferramentas para 1 e a equipa esta mais contente agora.",
		},
	}
	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			english, portuguese := glowing(), glowing()
			english.Text, portuguese.Text = pair.english, pair.portuguese
			englishSignal := Score(english).Signals[SignalSpecificity]
			portugueseSignal := Score(portuguese).Signals[SignalSpecificity]
			if englishSignal != portugueseSignal {
				t.Fatalf("specificity: english = %v, portuguese = %v; equivalent quotes must score equally",
					englishSignal, portugueseSignal)
			}
			if englishSignal == 0 {
				t.Fatalf("specificity = 0 for a quote carrying real numbers; the signal is not firing")
			}
		})
	}
}

// The range pattern must recognise a before-and-after pair and not fire on
// sentences that merely contain two numbers.
func TestScoreRecognisesBeforeAndAfterRanges(t *testing.T) {
	t.Parallel()
	ranged := "Onboarding fell from 3 days to 20 minutes across every single team."
	counted := "There are 4 of us and we handle 300 reports a week without trouble."
	if !rangePattern.MatchString(ranged) {
		t.Errorf("range pattern did not match a genuine before-and-after claim: %q", ranged)
	}
	if rangePattern.MatchString(counted) {
		t.Errorf("range pattern fired on a sentence with two unrelated numbers: %q", counted)
	}
}

func TestScoreFavoursTheIdealLengthBand(t *testing.T) {
	t.Parallel()
	terse, ideal, rambling := glowing(), glowing(), glowing()
	terse.Text = "Genuinely good product, saved us 2 days a week already."
	ideal.Text = glowing().Text
	rambling.Text = repeat(glowing().Text, 8)
	terseSignal := Score(terse).Signals[SignalLength]
	idealSignal := Score(ideal).Signals[SignalLength]
	ramblingSignal := Score(rambling).Signals[SignalLength]
	if idealSignal <= terseSignal {
		t.Errorf("length signal: ideal = %v, terse = %v; ideal must be higher", idealSignal, terseSignal)
	}
	if idealSignal <= ramblingSignal {
		t.Errorf("length signal: ideal = %v, rambling = %v; ideal must be higher", idealSignal, ramblingSignal)
	}
}

func TestScorePenalisesProfanityWithoutGatingIt(t *testing.T) {
	t.Parallel()
	clean := glowing()
	sweary := glowing()
	sweary.ModerationFlags = []string{"profanity"}
	cleanAssessment, swearyAssessment := Score(clean), Score(sweary)
	if swearyAssessment.Eligibility != Eligible {
		t.Fatalf("profanity must be a penalty, not a gate; eligibility = %q", swearyAssessment.Eligibility)
	}
	if swearyAssessment.BaseScore >= cleanAssessment.BaseScore {
		t.Errorf("base score: sweary = %v, clean = %v; sweary must rank lower",
			swearyAssessment.BaseScore, cleanAssessment.BaseScore)
	}
}

func TestScorePenalisesAmbivalentRatings(t *testing.T) {
	t.Parallel()
	ambivalent := glowing()
	three := 3
	ambivalent.Rating = &three
	got := Score(ambivalent)
	if got.Eligibility != Eligible {
		t.Fatalf("three stars is ambivalence, not hostility; eligibility = %q", got.Eligibility)
	}
	if got.BaseScore >= Score(glowing()).BaseScore {
		t.Errorf("three stars scored %v, five stars %v", got.BaseScore, Score(glowing()).BaseScore)
	}
}

// Ranking signals may be missing — the AI pipeline may have deferred on budget
// or failed. A candidate must still rank on the deterministic signals rather
// than vanish. This is only safe because moderation itself is gated separately.
func TestScoreRanksWithoutOptionalAISignals(t *testing.T) {
	t.Parallel()
	in := glowing()
	in.SentimentScore = nil
	in.Actionability = ""
	got := Score(in)
	if got.Eligibility != Eligible {
		t.Fatalf("eligibility = %q, want %q without optional AI signals", got.Eligibility, Eligible)
	}
	if got.Signals[SignalSentiment] != 0.5 {
		t.Errorf("sentiment signal = %v, want the neutral midpoint when unknown", got.Signals[SignalSentiment])
	}
	if got.BaseScore <= 0 {
		t.Errorf("base score = %v, want a usable score without optional AI signals", got.BaseScore)
	}
}

// An empty ModerationFlags slice means one of two very different things: the
// moderation pass ran and found nothing, or it never ran at all. Treating them
// alike makes the PII gate silently inoperative — and it is inoperative exactly
// where it matters, because feedbacks.text_process_status is set to 'skipped'
// for every Starter-plan tenant (migration 000025), so their moderation flags
// are permanently empty.
func TestScoreRefusesToRankUnmoderatedText(t *testing.T) {
	t.Parallel()
	for _, status := range []string{ModerationPending, ModerationProcessing, ModerationFailed, ModerationSkipped, ""} {
		in := glowing()
		in.ModerationStatus = status
		got := Score(in)
		if got.Eligibility != Ineligible {
			t.Errorf("moderation status %q: eligibility = %q, want %q — unmoderated text must never be suggested",
				status, got.Eligibility, Ineligible)
		}
		if !slices.Contains(got.IneligibleReasons, ReasonAwaitingModeration) {
			t.Errorf("moderation status %q: ineligible reasons = %#v, want to contain %q",
				status, got.IneligibleReasons, ReasonAwaitingModeration)
		}
	}
}

func TestScoreExplainsItselfWithRankedReasons(t *testing.T) {
	t.Parallel()
	got := Score(glowing())
	if len(got.Reasons) == 0 || len(got.Reasons) > MaxReasons {
		t.Fatalf("reasons = %#v, want between 1 and %d", got.Reasons, MaxReasons)
	}
	if got.Reasons[0] != ReasonHighRating {
		t.Errorf("reasons = %#v, want the strongest contributor (%q) first", got.Reasons, ReasonHighRating)
	}
	seen := map[string]bool{}
	for _, reason := range got.Reasons {
		if seen[reason] {
			t.Errorf("reasons = %#v, contains a duplicate", got.Reasons)
		}
		seen[reason] = true
	}
}

func TestScoreStaysWithinTheUnitInterval(t *testing.T) {
	t.Parallel()
	best := glowing()
	worst := glowing()
	worst.ModerationFlags = []string{"profanity"}
	three := 3
	worst.Rating = &three
	negative := -1.0
	worst.SentimentScore = &negative
	worst.AuthorReachable = false
	worst.Actionability = "actionable"
	worst.Text = repeat("padding to clear the floor without saying anything useful at all. ", 30)
	for _, in := range []Input{best, worst} {
		got := Score(in)
		if got.BaseScore < 0 || got.BaseScore > 1 {
			t.Errorf("base score = %v, want within [0,1] (assessment %#v)", got.BaseScore, got)
		}
	}
}

// A regional tag must satisfy a gate expressed with its primary subtag,
// otherwise every pt-PT and pt-AO candidate is silently dropped.
func TestScoreMatchesLocalesOnThePrimarySubtag(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"pt", "pt-PT", "pt-AO", "PT-pt"} {
		in := glowing()
		in.Language = language
		in.SupportedLocales = []string{"en", "pt"}
		if got := Score(in); got.Eligibility != Eligible {
			t.Errorf("language %q: eligibility = %q, want %q (%#v)",
				language, got.Eligibility, Eligible, got.IneligibleReasons)
		}
	}
}

func TestScoreSkipsTheLocaleGateWhenNothingIsKnown(t *testing.T) {
	t.Parallel()
	in := glowing()
	in.Language = ""
	if got := Score(in); got.Eligibility != Eligible {
		t.Fatalf("an unknown language must not gate; eligibility = %q (%#v)", got.Eligibility, got.IneligibleReasons)
	}
	in = glowing()
	in.SupportedLocales = nil
	if got := Score(in); got.Eligibility != Eligible {
		t.Fatalf("no configured locales must not gate; eligibility = %q (%#v)", got.Eligibility, got.IneligibleReasons)
	}
}

func TestScoreReportsEveryIneligibilityReason(t *testing.T) {
	t.Parallel()
	in := glowing()
	in.FeedbackStatus = "spam"
	in.ModerationFlags = []string{"pii"}
	in.Dismissed = true
	got := Score(in)
	for _, reason := range []string{ReasonNotOpen, ReasonModerationFlagged, ReasonDismissed} {
		if !slices.Contains(got.IneligibleReasons, reason) {
			t.Errorf("ineligible reasons = %#v, want to contain %q", got.IneligibleReasons, reason)
		}
	}
}

func repeat(value string, times int) string {
	out := ""
	for range times {
		out += value + " "
	}
	return out
}
