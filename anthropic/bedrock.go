package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/makarski/teamscope/config"
)

// bedrockAnthropicVersion is the Bedrock-specific counterpart of the
// Anthropic API's "anthropic-version" header; it goes in the request body
// instead, since Bedrock's InvokeModel has no room for custom headers.
const bedrockAnthropicVersion = "bedrock-2023-05-31"

// bedrockBackend calls Claude through Amazon Bedrock's InvokeModel API.
// Auth is handled entirely by the AWS SDK's default credential chain (env
// vars, shared config/profile, or an IAM role) — no token is stored here.
type bedrockBackend struct {
	client *bedrockruntime.Client
	model  string
}

func newBedrockBackend(cfg *config.Bedrock) (*bedrockBackend, error) {
	var optFns []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		optFns = append(optFns, awsconfig.WithRegion(cfg.Region))
	}
	if cfg.Profile != "" {
		optFns = append(optFns, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), optFns...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	return &bedrockBackend{
		client: bedrockruntime.NewFromConfig(awsCfg),
		model:  cfg.Model,
	}, nil
}

type bedrockRequest struct {
	AnthropicVersion string    `json:"anthropic_version"`
	MaxTokens        int       `json:"max_tokens"`
	Messages         []message `json:"messages"`
}

func (b *bedrockBackend) complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	body, err := json.Marshal(bedrockRequest{
		AnthropicVersion: bedrockAnthropicVersion,
		MaxTokens:        maxTokens,
		Messages:         []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: marshal bedrock request: %w", err)
	}

	out, err := b.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     &b.model,
		ContentType: strPtr("application/json"),
		Body:        body,
	})
	if err != nil {
		return "", fmt.Errorf("anthropic: bedrock invoke: %w", err)
	}

	return decodeReply(out.Body)
}

func strPtr(s string) *string { return &s }
