package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	"github.com/fidibeki/app.fidibeki/backend/internal/aiprovider"
)

// AWSCaller is configured explicitly by the deployment. Never load the host's
// ambient profile/instance credentials for a customer's inference request.
type AWSCaller struct{ AccessKey, SecretKey, SessionToken, ARN string }

func (a AWSCaller) signer(ctx context.Context, c aiprovider.Connection, validateTrust bool, client *http.Client) (func(context.Context, *http.Request, []byte) error, error) {
	if a.AccessKey == "" || a.SecretKey == "" || a.ARN == "" || c.ExternalID == "" {
		return nil, &aiprovider.Failure{Code: "aws_caller_not_configured"}
	}
	service := sts.NewFromConfig(aws.Config{Region: c.Region, Credentials: credentials.NewStaticCredentialsProvider(a.AccessKey, a.SecretKey, a.SessionToken), HTTPClient: client, RetryMaxAttempts: 1})
	if validateTrust {
		identity, err := service.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return nil, &aiprovider.Failure{Code: "aws_caller_unavailable"}
		}
		if aws.ToString(identity.Arn) != a.ARN || strings.HasSuffix(a.ARN, ":root") {
			return nil, &aiprovider.Failure{Code: "aws_caller_identity_mismatch"}
		}
		// Reject both missing and incorrect IDs. A wildcard condition is not
		// sufficient tenant isolation even if it requires an ID to be present.
		for _, externalID := range []*string{nil, aws.String(c.ExternalID + "-invalid")} {
			_, err = service.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: aws.String(c.RoleARN), ExternalId: externalID, RoleSessionName: aws.String("fidibeki-trust-test"), DurationSeconds: aws.Int32(900)})
			if err == nil {
				return nil, &aiprovider.Failure{Code: "aws_external_id_required"}
			}
			var apiErr smithy.APIError
			if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				return nil, &aiprovider.Failure{Code: "aws_trust_validation_failed"}
			}
		}
	}
	assumed, err := service.AssumeRole(ctx, &sts.AssumeRoleInput{RoleArn: aws.String(c.RoleARN), ExternalId: aws.String(c.ExternalID), RoleSessionName: aws.String("fidibeki-ai"), DurationSeconds: aws.Int32(900)})
	if err != nil || assumed.Credentials == nil {
		return nil, &aiprovider.Failure{Code: "aws_role_denied"}
	}
	cred := assumed.Credentials
	resolved := aws.Credentials{AccessKeyID: aws.ToString(cred.AccessKeyId), SecretAccessKey: aws.ToString(cred.SecretAccessKey), SessionToken: aws.ToString(cred.SessionToken), CanExpire: true, Expires: aws.ToTime(cred.Expiration)}
	signer := v4.NewSigner()
	return func(ctx context.Context, r *http.Request, body []byte) error {
		if time.Now().Add(time.Minute).After(resolved.Expires) {
			return &aiprovider.Failure{Code: "credential_expired", Retryable: true}
		}
		hash := sha256.Sum256(body)
		if err := signer.SignHTTP(ctx, resolved, r, hex.EncodeToString(hash[:]), "bedrock", c.Region, time.Now()); err != nil {
			return &aiprovider.Failure{Code: "aws_signing_failed"}
		}
		return nil
	}, nil
}
