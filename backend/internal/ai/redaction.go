package ai

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	RedactionLiteral       = "[REDACTED]"
	RedactionPolicyVersion = "2026-07-24"
)

var ErrUnsafeAIResult = errors.New("AI result could not satisfy the redaction policy")

type redactionRule struct {
	category string
	pattern  *regexp.Regexp
}

var redactionRules = []redactionRule{
	{category: "credential", pattern: regexp.MustCompile(
		`(?i)\b(?:api[_ -]?key|password|passwd|secret|access[_ -]?token|refresh[_ -]?token|authorization|bearer)\b\s*[:=]?\s*[^\s,;]+`,
	)},
	{category: "email", pattern: regexp.MustCompile(
		`(?i)\b[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`,
	)},
	{category: "financial_identifier", pattern: regexp.MustCompile(
		`(?i)\b[A-Z]{2}\d{2}(?:[\s-]?[A-Z0-9]){11,30}\b|\b(?:\d[ -]?){13,19}\b`,
	)},
	{category: "account_identifier", pattern: regexp.MustCompile(
		`(?i)\b(?:account|conta|customer|client|tax|nif|ssn|passport|document|government)[_ -]?(?:id|number|no|n[uú]mero)?\b\s*[:=#-]?\s*[A-Z0-9][A-Z0-9._/-]{3,}`,
	)},
	{category: "phone", pattern: regexp.MustCompile(
		`(?i)(?:\+?\d[\d\s().-]{7,}\d)`,
	)},
	{category: "network_address", pattern: regexp.MustCompile(
		`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b`,
	)},
	{category: "postal_address", pattern: regexp.MustCompile(
		`(?i)\b(?:address|morada|endere[cç]o)\b\s*[:=]\s*[^,;\n.!?]{4,120}`,
	)},
	{category: "name", pattern: regexp.MustCompile(
		`(?i)\b(?:full[_ -]?name|name|nome|my name is|o meu nome [ée])\b\s*[:=]?\s*[\p{L}][\p{L}'-]+(?:\s+[\p{L}][\p{L}'-]+){1,3}`,
	)},
	{category: "health", pattern: sensitiveStatementPattern(
		`health|medical|diagnos(?:is|ed)|cancer|diabetes|hiv|pregnan(?:t|cy)|sa[uú]de|doen[cç]a|diagn[oó]stico`,
	)},
	{category: "political", pattern: sensitiveStatementPattern(
		`political|politic(?:s|al)|party member|voting|pol[ií]tic[oa]|partido|voto`,
	)},
	{category: "religion", pattern: sensitiveStatementPattern(
		`religion|religious|christian|muslim|jewish|faith|religi[aã]o|cat[oó]lic[oa]|mu[cç]ulman[oa]|f[eé]`,
	)},
	{category: "sexuality", pattern: sensitiveStatementPattern(
		`sexual orientation|sexuality|gay|lesbian|bisexual|transgender|orienta[cç][aã]o sexual|homossexual|transg[eé]nero`,
	)},
	{category: "biometric", pattern: sensitiveStatementPattern(
		`biometric|fingerprint|face scan|voiceprint|biom[eé]tric[oa]|impress[aã]o digital|reconhecimento facial`,
	)},
	{category: "employment", pattern: sensitiveStatementPattern(
		`employment|employer|salary|fired|unemployed|emprego|empregador|sal[aá]rio|despedido|desempregado`,
	)},
	{category: "child", pattern: sensitiveStatementPattern(
		`child|minor|underage|my son|my daughter|crian[cç]a|menor|meu filho|minha filha`,
	)},
}

func sensitiveStatementPattern(keywords string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)[^.!?\n]{0,100}\b(?:` + keywords + `)\b[^.!?\n]{0,100}`)
}

func MinimizeProviderText(value string) (string, map[string]int) {
	return redactText(value)
}

func ProtectResult(result Result) (Result, error) {
	manifest := make(map[string]int)
	mergeRedactions(manifest, result.Redactions)
	fields := []*string{
		&result.Transcription,
		&result.CleanedText,
		&result.SuggestedTitle,
		&result.Category,
	}
	for _, field := range fields {
		protected, counts := redactText(*field)
		*field = protected
		mergeRedactions(manifest, counts)
	}
	for index := range result.ModerationFlags {
		protected, counts := redactText(result.ModerationFlags[index])
		result.ModerationFlags[index] = protected
		mergeRedactions(manifest, counts)
	}
	for _, field := range fields {
		if containsSensitiveValue(*field) {
			return Result{}, permanent(ErrUnsafeAIResult)
		}
	}
	for _, flag := range result.ModerationFlags {
		if containsSensitiveValue(flag) {
			return Result{}, permanent(ErrUnsafeAIResult)
		}
	}
	if err := result.Normalize(); err != nil {
		return Result{}, permanent(fmt.Errorf("%w: invalid derived result", ErrUnsafeAIResult))
	}
	result.Redactions = manifest
	return result, nil
}

func redactText(value string) (string, map[string]int) {
	manifest := make(map[string]int)
	protected := value
	for _, rule := range redactionRules {
		matches := rule.pattern.FindAllStringIndex(protected, -1)
		if len(matches) == 0 {
			continue
		}
		manifest[rule.category] += len(matches)
		protected = rule.pattern.ReplaceAllString(protected, RedactionLiteral)
	}
	protected = strings.TrimSpace(protected)
	return protected, manifest
}

func containsSensitiveValue(value string) bool {
	for _, rule := range redactionRules {
		if rule.pattern.MatchString(value) {
			return true
		}
	}
	return false
}

func mergeRedactions(target, source map[string]int) {
	for category, count := range source {
		target[category] += count
	}
}
