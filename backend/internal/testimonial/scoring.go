// Package testimonial turns collected feedback into publishable testimonials.
//
// Scoring lives here as a pure function over signals the feedback pipeline has
// already computed. It performs no I/O and calls no model, so ranking is
// deterministic, explainable to the customer, and exhaustively testable. The AI
// pipeline enriches candidates with a suggested excerpt and tags, but ranking
// never depends on it: a tenant whose AI budget is exhausted still gets a
// usable Suggested queue.
package testimonial

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// ScoreVersion identifies the current weighting. Bump it when weights or gates
// change so stored assessments can be recomputed by a job rather than silently
// mixing two generations of scores in one ranking.
const ScoreVersion = 1

// MaxReasons caps how many contributors are surfaced to the customer. Three is
// enough to explain a suggestion and few enough to read at a glance.
const MaxReasons = 3

// minTextRunes is the floor below which no amount of enthusiasm makes a
// testimonial. "Nice!" is not social proof.
const minTextRunes = 40

type Eligibility string

const (
	Eligible   Eligibility = "eligible"
	Ineligible Eligibility = "ineligible"
)

// Signal keys, also used as the keys of Assessment.Signals so the stored
// jsonb is self-describing when someone asks why a candidate ranked where it did.
const (
	SignalRating        = "rating"
	SignalSentiment     = "sentiment"
	SignalLength        = "length"
	SignalSpecificity   = "specificity"
	SignalActionability = "actionability"
	SignalReachable     = "reachable"
)

// Reason keys are i18n message keys, not prose: the backoffice renders them in
// the member's locale.
const (
	ReasonHighRating      = "testimonials.reason.highRating"
	ReasonPositiveTone    = "testimonials.reason.positiveTone"
	ReasonIdealLength     = "testimonials.reason.idealLength"
	ReasonConcreteOutcome = "testimonials.reason.concreteOutcome"
	ReasonNotActionable   = "testimonials.reason.notActionable"
	ReasonAuthorReachable = "testimonials.reason.authorReachable"
)

const (
	ReasonNotOpen             = "testimonials.ineligible.feedbackNotOpen"
	ReasonModerationFlagged   = "testimonials.ineligible.moderationFlagged"
	ReasonTextTooShort        = "testimonials.ineligible.textTooShort"
	ReasonLowRating           = "testimonials.ineligible.lowRating"
	ReasonAlreadyTestimonial  = "testimonials.ineligible.alreadyTestimonial"
	ReasonDismissed           = "testimonials.ineligible.dismissed"
	ReasonUnsupportedLanguage = "testimonials.ineligible.unsupportedLanguage"
)

// Weights sum to 0.98 before penalties; the result is clamped to [0,1] rather
// than normalised, so a candidate that misses one signal is not silently
// promoted by the absence of its competitors.
const (
	weightRating        = 0.25
	weightSentiment     = 0.20
	weightLength        = 0.15
	weightSpecificity   = 0.20
	weightActionability = 0.08
	weightReachable     = 0.10

	penaltyAmbivalentRating = 0.10
	penaltyProfanity        = 0.12
)

// blockingFlags are the moderation flags that make publication unthinkable
// regardless of how glowing the text is. They mirror the vocabulary validated
// in backend/internal/ai. "profanity" is deliberately absent: it is a penalty.
var blockingFlags = map[string]struct{}{
	"pii": {}, "hate": {}, "harassment": {},
	"sexual": {}, "violence": {}, "self_harm": {},
}

// Input is everything scoring needs, already gathered by the caller. Pointer
// fields distinguish "absent" from "zero": a nil SentimentScore means the AI
// pipeline has not run, which is not the same as neutral sentiment measured.
type Input struct {
	Text             string
	Rating           *int
	FeedbackStatus   string
	SentimentScore   *float64
	ModerationFlags  []string
	Actionability    string
	AuthorReachable  bool
	Language         string
	SupportedLocales []string
	HasTestimonial   bool
	Dismissed        bool
}

// Assessment is the stored result. Signals and Reasons exist so the customer is
// never shown a bare number: an unexplained score is a score nobody trusts.
type Assessment struct {
	Eligibility       Eligibility
	IneligibleReasons []string
	BaseScore         float64
	Reasons           []string
	Signals           map[string]float64
	ScoreVersion      int
}

// Score ranks one feedback as a testimonial candidate.
//
// Hard gates run first and short-circuit the weighting entirely: an ineligible
// candidate carries no score, because a score would invite someone to surface it
// anyway.
func Score(in Input) Assessment {
	if reasons := gate(in); len(reasons) > 0 {
		return Assessment{
			Eligibility:       Ineligible,
			IneligibleReasons: reasons,
			Signals:           map[string]float64{},
			ScoreVersion:      ScoreVersion,
		}
	}

	signals := map[string]float64{
		SignalRating:        ratingSignal(in.Rating),
		SignalSentiment:     sentimentSignal(in.SentimentScore),
		SignalLength:        lengthSignal(in.Text),
		SignalSpecificity:   specificitySignal(in.Text),
		SignalActionability: actionabilitySignal(in.Actionability),
		SignalReachable:     boolSignal(in.AuthorReachable),
	}

	contributions := []struct {
		reason string
		value  float64
	}{
		{ReasonHighRating, signals[SignalRating] * weightRating},
		{ReasonPositiveTone, signals[SignalSentiment] * weightSentiment},
		{ReasonIdealLength, signals[SignalLength] * weightLength},
		{ReasonConcreteOutcome, signals[SignalSpecificity] * weightSpecificity},
		{ReasonNotActionable, signals[SignalActionability] * weightActionability},
		{ReasonAuthorReachable, signals[SignalReachable] * weightReachable},
	}

	score := 0.0
	for _, contribution := range contributions {
		score += contribution.value
	}
	if in.Rating != nil && *in.Rating == 3 {
		score -= penaltyAmbivalentRating
	}
	if hasFlag(in.ModerationFlags, "profanity") {
		score -= penaltyProfanity
	}

	// Sort by contribution so the customer sees why this candidate ranked, not
	// an arbitrary slice of true statements. Ties break on the declared order
	// above, which keeps output stable across runs.
	sort.SliceStable(contributions, func(i, j int) bool {
		return contributions[i].value > contributions[j].value
	})
	reasons := make([]string, 0, MaxReasons)
	for _, contribution := range contributions {
		if len(reasons) == MaxReasons || contribution.value <= 0 {
			break
		}
		reasons = append(reasons, contribution.reason)
	}

	return Assessment{
		Eligibility:  Eligible,
		BaseScore:    clamp(score),
		Reasons:      reasons,
		Signals:      signals,
		ScoreVersion: ScoreVersion,
	}
}

// gate collects every reason a candidate is ineligible rather than returning
// the first, so the backoffice can explain the full picture instead of playing
// whack-a-mole with one blocker at a time.
func gate(in Input) []string {
	var reasons []string
	if in.FeedbackStatus == "spam" || in.FeedbackStatus == "archived" {
		reasons = append(reasons, ReasonNotOpen)
	}
	for _, flag := range in.ModerationFlags {
		if _, blocked := blockingFlags[strings.ToLower(strings.TrimSpace(flag))]; blocked {
			reasons = append(reasons, ReasonModerationFlagged)
			break
		}
	}
	if len([]rune(strings.TrimSpace(in.Text))) < minTextRunes {
		reasons = append(reasons, ReasonTextTooShort)
	}
	if in.Rating != nil && *in.Rating <= 2 {
		reasons = append(reasons, ReasonLowRating)
	}
	if in.HasTestimonial {
		reasons = append(reasons, ReasonAlreadyTestimonial)
	}
	if in.Dismissed {
		reasons = append(reasons, ReasonDismissed)
	}
	if !localeSupported(in.Language, in.SupportedLocales) {
		reasons = append(reasons, ReasonUnsupportedLanguage)
	}
	return reasons
}

// localeSupported compares primary subtags, so a pt-PT or pt-AO quote satisfies
// a tenant configured for "pt". An unknown language or an unconfigured tenant
// never gates — we do not drop candidates over missing metadata.
func localeSupported(language string, supported []string) bool {
	language = primarySubtag(language)
	if language == "" || len(supported) == 0 {
		return true
	}
	for _, candidate := range supported {
		if primarySubtag(candidate) == language {
			return true
		}
	}
	return false
}

func primarySubtag(tag string) string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if index := strings.IndexAny(tag, "-_"); index >= 0 {
		tag = tag[:index]
	}
	return tag
}

func ratingSignal(rating *int) float64 {
	if rating == nil || *rating < 4 {
		return 0
	}
	return 1
}

// sentimentSignal maps the AI range to the unit interval.
//
// backend/internal/ai validates SentimentScore to [-1, 1]. Using it raw would
// make a complaint contribute a small positive amount instead of nothing, so
// the transform is explicit. An absent score is treated as neutral rather than
// as bad news, so a tenant without AI is not penalised for it.
func sentimentSignal(score *float64) float64 {
	if score == nil {
		return 0.5
	}
	return clamp((*score + 1) / 2)
}

// lengthSignal favours the band that actually fits a testimonial card. Below it
// a quote says nothing; above it nobody finishes reading.
func lengthSignal(text string) float64 {
	switch length := len([]rune(strings.TrimSpace(text))); {
	case length < 120:
		return ramp(float64(length), minTextRunes, 120)
	case length <= 600:
		return 1
	case length <= 1200:
		return 1 - 0.8*ramp(float64(length), 600, 1200)
	default:
		return 0.2
	}
}

var (
	// A digit anywhere is the cheapest reliable marker of a concrete claim.
	numberPattern = regexp.MustCompile(`\d`)
	// Duration and comparison vocabulary in both languages this platform
	// serves. English-only detection would quietly rank every Portuguese
	// testimonial below every English one.
	durationPattern = regexp.MustCompile(`(?i)\b(seconds?|minutes?|hours?|days?|weeks?|months?|years?|` +
		`segundos?|minutos?|horas?|dias?|semanas?|m[êe]s|meses|anos?)\b`)
	comparisonPattern = regexp.MustCompile(`(?i)\b(faster|slower|cheaper|better|worse|less|more|fewer|` +
		`from|to|instead|reduced|saved|cut|doubled|halved|` +
		`mais|menos|melhor|pior|r[áa]pido|reduzimos|reduziu|poupamos|poupou|passou|antes|depois|dobro|metade)\b`)
)

// specificitySignal rewards claims a reader can picture. "It cut triage from 3
// days to 20 minutes" is worth more than "we really like it", and the
// difference is mostly numbers, units and comparisons.
func specificitySignal(text string) float64 {
	markers := 0.0
	if numberPattern.MatchString(text) {
		markers += 1.5
	}
	if strings.ContainsRune(text, '%') {
		markers++
	}
	if durationPattern.MatchString(text) {
		markers++
	}
	if comparisonPattern.MatchString(text) {
		markers++
	}
	return clamp(markers / 3.5)
}

// actionabilitySignal inverts the triage meaning on purpose: actionable
// feedback is a bug report, and bug reports do not belong on a marketing page.
func actionabilitySignal(actionability string) float64 {
	if actionability == "not_actionable" {
		return 1
	}
	return 0
}

func boolSignal(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func hasFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if strings.EqualFold(strings.TrimSpace(flag), want) {
			return true
		}
	}
	return false
}

// ramp returns how far value has travelled from low to high, clamped to [0,1].
func ramp(value, low, high float64) float64 {
	if high <= low {
		return 0
	}
	return clamp((value - low) / (high - low))
}

func clamp(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

// TextRunes reports the trimmed rune count of a quote. Callers persisting
// candidate signals use it so the stored length matches what scoring measured,
// including for scripts where byte length and rune length diverge.
func TextRunes(text string) int {
	return len([]rune(strings.TrimFunc(text, unicode.IsSpace)))
}
