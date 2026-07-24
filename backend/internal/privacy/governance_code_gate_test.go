package privacy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const (
	dependencyApplication = "application"
	dependencyBuildTest   = "build_test"
	dependencyProvider    = "provider"
)

type dependencyClassification struct {
	Class       string
	ProviderIDs []string
}

type providerCodeSignal struct {
	Path     string
	Contains string
}

var governedGoDependencies = map[string]dependencyClassification{
	"github.com/Eyevinn/mp4ff":                                     {Class: dependencyApplication},
	"github.com/getsentry/sentry-go":                               {Class: dependencyProvider, ProviderIDs: []string{"sentry_error_telemetry"}},
	"github.com/go-chi/chi/v5":                                     {Class: dependencyApplication},
	"github.com/go-redis/redis_rate/v10":                           {Class: dependencyProvider, ProviderIDs: []string{"redis_asynq"}},
	"github.com/golang-jwt/jwt/v5":                                 {Class: dependencyApplication},
	"github.com/golang-migrate/migrate/v4":                         {Class: dependencyApplication},
	"github.com/google/uuid":                                       {Class: dependencyApplication},
	"github.com/hibiken/asynq":                                     {Class: dependencyProvider, ProviderIDs: []string{"redis_asynq"}},
	"github.com/jackc/pgx/v5":                                      {Class: dependencyProvider, ProviderIDs: []string{"postgresql_runtime"}},
	"github.com/joho/godotenv":                                     {Class: dependencyApplication},
	"github.com/minio/minio-go/v7":                                 {Class: dependencyProvider, ProviderIDs: []string{"s3_compatible_object_storage"}},
	"github.com/redis/go-redis/v9":                                 {Class: dependencyProvider, ProviderIDs: []string{"redis_asynq"}},
	"github.com/stripe/stripe-go/v85":                              {Class: dependencyProvider, ProviderIDs: []string{"stripe_billing_api"}},
	"github.com/svix/svix-webhooks":                                {Class: dependencyApplication},
	"github.com/testcontainers/testcontainers-go":                  {Class: dependencyBuildTest},
	"github.com/testcontainers/testcontainers-go/modules/postgres": {Class: dependencyBuildTest},
	"golang.org/x/image":                                           {Class: dependencyApplication},
	"golang.org/x/net":                                             {Class: dependencyApplication},
	"golang.org/x/text":                                            {Class: dependencyApplication},
	"google.golang.org/genai":                                      {Class: dependencyProvider, ProviderIDs: []string{"google_gemini_api"}},
}

var governedNPMDependencies = map[string]map[string]dependencyClassification{
	"widget/package.json": {
		"dependencies:@sentry/browser":        {Class: dependencyProvider, ProviderIDs: []string{"sentry_error_telemetry"}},
		"dependencies:re2js":                  {Class: dependencyApplication},
		"dependencies:rrweb":                  {Class: dependencyApplication},
		"devDependencies:@playwright/test":    {Class: dependencyBuildTest},
		"devDependencies:@vitest/coverage-v8": {Class: dependencyBuildTest},
		"devDependencies:esbuild":             {Class: dependencyBuildTest},
		"devDependencies:happy-dom":           {Class: dependencyBuildTest},
		"devDependencies:typescript":          {Class: dependencyBuildTest},
		"devDependencies:vitest":              {Class: dependencyBuildTest},
	},
	"web/package.json": {
		"dependencies:@lucide/vue":         {Class: dependencyApplication},
		"dependencies:@nuxtjs/i18n":        {Class: dependencyApplication},
		"dependencies:nuxt":                {Class: dependencyApplication},
		"dependencies:re2js":               {Class: dependencyApplication},
		"dependencies:rrweb-player":        {Class: dependencyApplication},
		"dependencies:vue":                 {Class: dependencyApplication},
		"devDependencies:@playwright/test": {Class: dependencyBuildTest},
		"devDependencies:typescript":       {Class: dependencyBuildTest},
		"devDependencies:vitest":           {Class: dependencyBuildTest},
		"devDependencies:vue-tsc":          {Class: dependencyBuildTest},
	},
	"marketing/package.json": {
		"dependencies:@lucide/vue":         {Class: dependencyApplication},
		"dependencies:nuxt":                {Class: dependencyApplication},
		"dependencies:vue":                 {Class: dependencyApplication},
		"devDependencies:@playwright/test": {Class: dependencyBuildTest},
		"devDependencies:typescript":       {Class: dependencyBuildTest},
		"devDependencies:vitest":           {Class: dependencyBuildTest},
		"devDependencies:vue-tsc":          {Class: dependencyBuildTest},
	},
}

var governedHTTPCallSites = map[string][]string{
	"backend/internal/billing/turnstile.go": {"cloudflare_turnstile"},
	"backend/internal/email/resend.go":      {"resend_transactional_email"},
	"backend/internal/integration/delivery.go": {
		"slack_incoming_webhook",
		"microsoft_teams_incoming_webhook",
		"customer_generic_webhook",
	},
}

var governedProviderCodeSignals = map[string][]providerCodeSignal{
	"postgresql_runtime": {
		{Path: "backend/internal/db/pools.go", Contains: "pgxpool.NewWithConfig"},
	},
	"redis_asynq": {
		{Path: "backend/internal/cache/redis.go", Contains: "redis.NewClient"},
		{Path: "backend/internal/worker/worker.go", Contains: "asynq.NewServer"},
	},
	"s3_compatible_object_storage": {
		{Path: "backend/internal/storage/storage.go", Contains: "minio.New"},
	},
	"resend_transactional_email": {
		{Path: "backend/internal/email/resend.go", Contains: "defaultResendEndpoint"},
	},
	"email_capture_filesystem": {
		{Path: "backend/internal/email/capture.go", Contains: "type CaptureSender struct"},
	},
	"google_gemini_api": {
		{Path: "backend/internal/ai/gemini.go", Contains: `"google.golang.org/genai"`},
	},
	"stripe_billing_api": {
		{Path: "backend/internal/billing/stripe.go", Contains: `"github.com/stripe/stripe-go/v85"`},
	},
	"sentry_error_telemetry": {
		{Path: "backend/internal/observability/sentry.go", Contains: `"github.com/getsentry/sentry-go"`},
		{Path: "widget/src/modules/error-reporting.ts", Contains: "from '@sentry/browser'"},
	},
	"slack_incoming_webhook": {
		{Path: "backend/internal/integration/models.go", Contains: `KindSlack   Kind = "slack"`},
	},
	"microsoft_teams_incoming_webhook": {
		{Path: "backend/internal/integration/models.go", Contains: `KindTeams   Kind = "teams"`},
	},
	"customer_generic_webhook": {
		{Path: "backend/internal/integration/models.go", Contains: `KindWebhook Kind = "webhook"`},
	},
	"cloudflare_turnstile": {
		{Path: "backend/internal/billing/turnstile.go", Contains: "challenges.cloudflare.com/turnstile/v0/siteverify"},
		{Path: "web/components/TurnstileWidget.client.vue", Contains: "challenges.cloudflare.com/turnstile/v0/api.js"},
	},
}

func TestGovernanceProviderCodeReleaseGate(t *testing.T) {
	t.Parallel()
	root := governanceRepositoryRoot(t)
	providerIDs := governanceProviderIDs()

	goDependencies, err := readDirectGoDependencies(filepath.Join(root, "backend/go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDependencyInventory(goDependencies, governedGoDependencies, providerIDs); err != nil {
		t.Fatalf("Go dependency governance: %v", err)
	}

	for manifestPath, governed := range governedNPMDependencies {
		dependencies, err := readNPMDependencies(filepath.Join(root, manifestPath))
		if err != nil {
			t.Fatalf("%s: %v", manifestPath, err)
		}
		if err := validateDependencyInventory(dependencies, governed, providerIDs); err != nil {
			t.Fatalf("%s dependency governance: %v", manifestPath, err)
		}
	}

	callSites, err := outboundHTTPCallSites(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHTTPCallSites(callSites, governedHTTPCallSites, providerIDs); err != nil {
		t.Fatalf("HTTP call-site governance: %v", err)
	}
	if err := validateProviderCodeSignals(root, providerIDs, governedProviderCodeSignals); err != nil {
		t.Fatalf("provider code evidence: %v", err)
	}
}

func TestGovernanceDependencyInventoryRejectsUnclassifiedAndStaleEntries(t *testing.T) {
	t.Parallel()
	providers := map[string]struct{}{"provider": {}}
	governed := map[string]dependencyClassification{
		"known": {Class: dependencyProvider, ProviderIDs: []string{"provider"}},
		"stale": {Class: dependencyApplication},
	}
	err := validateDependencyInventory(
		map[string]struct{}{"known": {}, "new-sdk": {}},
		governed,
		providers,
	)
	if err == nil || !strings.Contains(err.Error(), `unclassified dependency "new-sdk"`) ||
		!strings.Contains(err.Error(), `stale dependency classification "stale"`) {
		t.Fatalf("validateDependencyInventory error = %v, want unclassified and stale errors", err)
	}
}

func TestGovernanceHTTPCallSitesRejectUnapprovedAndStaleEntries(t *testing.T) {
	t.Parallel()
	providers := map[string]struct{}{"provider": {}}
	err := validateHTTPCallSites(
		map[string][]string{
			"backend/internal/new/provider.go": {"NewRequestWithContext"},
		},
		map[string][]string{
			"backend/internal/old/provider.go": {"provider"},
		},
		providers,
	)
	if err == nil || !strings.Contains(err.Error(), "unapproved outbound HTTP call") ||
		!strings.Contains(err.Error(), "stale outbound HTTP approval") {
		t.Fatalf("validateHTTPCallSites error = %v, want unapproved and stale errors", err)
	}
}

func TestGovernanceProviderCodeSignalsRequireEveryProvider(t *testing.T) {
	t.Parallel()
	err := validateProviderCodeSignals(
		t.TempDir(),
		map[string]struct{}{"registered": {}, "missing": {}},
		map[string][]providerCodeSignal{
			"registered": {{Path: "adapter.go", Contains: "client"}},
			"unknown":    {{Path: "unknown.go", Contains: "client"}},
		},
	)
	if err == nil || !strings.Contains(err.Error(), `provider "missing" has no code evidence`) ||
		!strings.Contains(err.Error(), `unknown provider "unknown"`) {
		t.Fatalf("validateProviderCodeSignals error = %v, want missing and unknown provider errors", err)
	}
}

func governanceRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve governance test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "../../.."))
}

func governanceProviderIDs() map[string]struct{} {
	providers := make(map[string]struct{})
	for _, provider := range providerGovernanceRegistry() {
		providers[provider.ProviderID] = struct{}{}
	}
	return providers
}

func readDirectGoDependencies(path string) (map[string]struct{}, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Go module: %w", err)
	}
	defer file.Close()

	dependencies := make(map[string]struct{})
	inRequireBlock := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "require (":
			inRequireBlock = true
			continue
		case inRequireBlock && line == ")":
			inRequireBlock = false
			continue
		case strings.HasPrefix(line, "require ") && !inRequireBlock:
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		case !inRequireBlock:
			continue
		}
		if strings.Contains(line, "// indirect") {
			continue
		}
		fields := strings.Fields(strings.SplitN(line, "//", 2)[0])
		if len(fields) < 2 {
			return nil, fmt.Errorf("parse direct Go dependency line %q", line)
		}
		dependencies[fields[0]] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Go module: %w", err)
	}
	return dependencies, nil
}

func readNPMDependencies(path string) (map[string]struct{}, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read package manifest: %w", err)
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, fmt.Errorf("decode package manifest: %w", err)
	}
	dependencies := make(map[string]struct{}, len(manifest.Dependencies)+len(manifest.DevDependencies))
	for dependency := range manifest.Dependencies {
		dependencies["dependencies:"+dependency] = struct{}{}
	}
	for dependency := range manifest.DevDependencies {
		dependencies["devDependencies:"+dependency] = struct{}{}
	}
	return dependencies, nil
}

func validateDependencyInventory(
	actual map[string]struct{},
	governed map[string]dependencyClassification,
	providerIDs map[string]struct{},
) error {
	var validationErrors []error
	for dependency := range actual {
		if _, ok := governed[dependency]; !ok {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("unclassified dependency %q", dependency),
			)
		}
	}
	for dependency, classification := range governed {
		if _, ok := actual[dependency]; !ok {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("stale dependency classification %q", dependency),
			)
		}
		switch classification.Class {
		case dependencyApplication, dependencyBuildTest:
			if len(classification.ProviderIDs) != 0 {
				validationErrors = append(
					validationErrors,
					fmt.Errorf("non-provider dependency %q references providers", dependency),
				)
			}
		case dependencyProvider:
			if len(classification.ProviderIDs) == 0 {
				validationErrors = append(
					validationErrors,
					fmt.Errorf("provider dependency %q has no provider", dependency),
				)
			}
			for _, providerID := range classification.ProviderIDs {
				if _, ok := providerIDs[providerID]; !ok {
					validationErrors = append(
						validationErrors,
						fmt.Errorf("dependency %q references unknown provider %q", dependency, providerID),
					)
				}
			}
		default:
			validationErrors = append(
				validationErrors,
				fmt.Errorf("dependency %q has unknown class %q", dependency, classification.Class),
			)
		}
	}
	return errorsJoin(validationErrors)
}

func outboundHTTPCallSites(root string) (map[string][]string, error) {
	callSites := make(map[string][]string)
	err := filepath.WalkDir(filepath.Join(root, "backend"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		source, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		httpAliases := make(map[string]struct{})
		for _, imported := range source.Imports {
			if imported.Path.Value != `"net/http"` {
				continue
			}
			name := "http"
			if imported.Name != nil {
				name = imported.Name.Name
			}
			if name == "." {
				return fmt.Errorf("%s uses an unsupported dot import for net/http", path)
			}
			httpAliases[name] = struct{}{}
		}
		if len(httpAliases) == 0 {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		ast.Inspect(source, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if _, ok := httpAliases[identifier.Name]; !ok {
				return true
			}
			switch selector.Sel.Name {
			case "NewRequest", "NewRequestWithContext", "Get", "Head", "Post", "PostForm":
				callSites[relative] = append(callSites[relative], selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	return callSites, err
}

func validateHTTPCallSites(
	actual map[string][]string,
	governed map[string][]string,
	providerIDs map[string]struct{},
) error {
	var validationErrors []error
	for path, calls := range actual {
		if _, ok := governed[path]; !ok {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("unapproved outbound HTTP call in %q (%s)", path, strings.Join(calls, ", ")),
			)
		}
	}
	for path, providers := range governed {
		if _, ok := actual[path]; !ok {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("stale outbound HTTP approval %q", path),
			)
		}
		if len(providers) == 0 {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("outbound HTTP approval %q has no provider", path),
			)
		}
		for _, providerID := range providers {
			if _, ok := providerIDs[providerID]; !ok {
				validationErrors = append(
					validationErrors,
					fmt.Errorf("outbound HTTP approval %q references unknown provider %q", path, providerID),
				)
			}
		}
	}
	return errorsJoin(validationErrors)
}

func validateProviderCodeSignals(
	root string,
	providerIDs map[string]struct{},
	signals map[string][]providerCodeSignal,
) error {
	var validationErrors []error
	for providerID := range providerIDs {
		if len(signals[providerID]) == 0 {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("provider %q has no code evidence", providerID),
			)
		}
	}
	for providerID, providerSignals := range signals {
		if _, ok := providerIDs[providerID]; !ok {
			validationErrors = append(
				validationErrors,
				fmt.Errorf("code evidence references unknown provider %q", providerID),
			)
			continue
		}
		seen := make(map[providerCodeSignal]struct{}, len(providerSignals))
		for _, signal := range providerSignals {
			if strings.TrimSpace(signal.Path) == "" || strings.TrimSpace(signal.Contains) == "" {
				validationErrors = append(
					validationErrors,
					fmt.Errorf("provider %q has incomplete code evidence", providerID),
				)
				continue
			}
			if _, duplicate := seen[signal]; duplicate {
				validationErrors = append(
					validationErrors,
					fmt.Errorf("provider %q has duplicate code evidence for %q", providerID, signal.Path),
				)
				continue
			}
			seen[signal] = struct{}{}
			encoded, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(signal.Path)))
			if err != nil {
				validationErrors = append(
					validationErrors,
					fmt.Errorf("provider %q code evidence %q: %w", providerID, signal.Path, err),
				)
				continue
			}
			if !strings.Contains(string(encoded), signal.Contains) {
				validationErrors = append(
					validationErrors,
					fmt.Errorf(
						"provider %q code evidence %q no longer contains %q",
						providerID, signal.Path, signal.Contains,
					),
				)
			}
		}
	}
	return errorsJoin(validationErrors)
}

func errorsJoin(values []error) error {
	values = slices.DeleteFunc(values, func(value error) bool { return value == nil })
	if len(values) == 0 {
		return nil
	}
	var messages []string
	for _, value := range values {
		messages = append(messages, value.Error())
	}
	slices.Sort(messages)
	return fmt.Errorf("%s", strings.Join(messages, "; "))
}
