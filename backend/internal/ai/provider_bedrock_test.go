package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/aiprovider"
)

func TestBedrockRequiresExactExternalIDAndSignsWithAssumedCredentials(t *testing.T) {
	c := aiprovider.Connection{Region: "eu-west-1", RoleARN: "arn:aws:iam::123456789012:role/customer", ExternalID: "workspace-specific"}
	caller := AWSCaller{AccessKey: "caller-key", SecretKey: "caller-secret", ARN: "arn:aws:iam::987654321098:user/fidibeki"}
	for _, wildcard := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact", true: "wildcard_rejected"}[wildcard], func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				data, _ := io.ReadAll(r.Body)
				values, _ := url.ParseQuery(string(data))
				if !strings.Contains(r.Header.Get("Authorization"), "Credential=caller-key/") {
					t.Fatal("STS did not use explicit caller")
				}
				status := 200
				body := ""
				if values.Get("Action") == "GetCallerIdentity" {
					body = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult><Arn>` + caller.ARN + `</Arn><Account>987654321098</Account><UserId>fixture</UserId></GetCallerIdentityResult></GetCallerIdentityResponse>`
				} else if values.Get("ExternalId") == c.ExternalID || (wildcard && values.Get("ExternalId") != "") {
					body = `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult><Credentials><AccessKeyId>assumed-key</AccessKeyId><SecretAccessKey>assumed-secret</SecretAccessKey><SessionToken>assumed-token</SessionToken><Expiration>` + time.Now().Add(time.Hour).Format(time.RFC3339) + `</Expiration></Credentials></AssumeRoleResult></AssumeRoleResponse>`
				} else {
					status = 403
					body = `<ErrorResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>Denied</Message></Error></ErrorResponse>`
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/xml"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			sign, err := caller.signer(context.Background(), c, true, client)
			if wildcard {
				var failure *aiprovider.Failure
				if !errors.As(err, &failure) || failure.Code != "aws_external_id_required" {
					t.Fatalf("wildcard accepted: %v", err)
				}
				return
			}
			if err != nil || calls != 4 {
				t.Fatalf("trust validation: %v calls=%d", err, calls)
			}
			request, _ := http.NewRequest("POST", "https://bedrock-runtime.eu-west-1.amazonaws.com/model/fixture/converse", strings.NewReader("{}"))
			if err = sign(context.Background(), request, []byte("{}")); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(request.Header.Get("Authorization"), "Credential=assumed-key/") || request.Header.Get("X-Amz-Security-Token") != "assumed-token" {
				t.Fatal("inference did not use assumed role")
			}
		})
	}
	if _, err := (AWSCaller{}).signer(context.Background(), c, false, http.DefaultClient); err == nil {
		t.Fatal("ambient AWS credentials accepted")
	}
}
