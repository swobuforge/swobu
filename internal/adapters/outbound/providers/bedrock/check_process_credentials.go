package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/processcreds"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const checkProcessOutputLimit = 64 << 10

var errCheckProcess = errors.New("AWS credential process failed or exceeded check limits")

// boundedCheckProcess preserves the SDK's configured AWS command semantics
// (including its shell contract), but closes stdin, discards stderr and bounds
// stdout before decoding. It never includes process output in an error.
type boundedCheckProcess struct{ command string }

func (p boundedCheckProcess) Retrieve(ctx context.Context) (aws.Credentials, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd, err := (processcreds.DefaultNewCommandBuilder{Args: []string{p.command}}).NewCommand(ctx)
	if err != nil {
		return aws.Credentials{}, errCheckProcess
	}
	cmd.Stdin = nil
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	output := &checkProcessOutput{cancel: cancel}
	cmd.Stdout = output
	if err := cmd.Run(); err != nil || output.exceeded {
		return aws.Credentials{}, errCheckProcess
	}
	var response processcreds.CredentialProcessResponse
	if json.Unmarshal(output.body, &response) != nil || response.Version != 1 || response.AccessKeyID == "" || response.SecretAccessKey == "" {
		return aws.Credentials{}, errCheckProcess
	}
	credentials := aws.Credentials{Source: processcreds.ProviderName, AccessKeyID: response.AccessKeyID, SecretAccessKey: response.SecretAccessKey, SessionToken: response.SessionToken, AccountID: response.AccountID}
	if response.Expiration != nil {
		credentials.CanExpire = true
		credentials.Expires = *response.Expiration
	}
	return credentials, nil
}

type checkProcessOutput struct {
	body     []byte
	exceeded bool
	cancel   context.CancelFunc
}

func (b *checkProcessOutput) Write(p []byte) (int, error) {
	if b.exceeded || len(p) > checkProcessOutputLimit-len(b.body) {
		b.exceeded = true
		b.cancel()
		return 0, errCheckProcess
	}
	b.body = append(b.body, p...)
	return len(p), nil
}

// boundSelectedProcess changes only a chain the SDK actually selected as a
// credential-process chain. Profile precedence stays SDK-owned. Rebuild its
// source-profile role wrappers with the SDK provider and resolved role options;
// other credential sources retain the SDK's original provider unchanged.
func boundSelectedProcess(cfg *aws.Config) error {
	sources, ok := cfg.Credentials.(aws.CredentialProviderSource)
	if !ok {
		return nil
	}
	selected := false
	for _, source := range sources.ProviderSources() {
		if source == aws.CredentialSourceProcess {
			selected = true
		}
	}
	if !selected {
		return nil
	}
	for _, source := range cfg.ConfigSources {
		if shared, ok := source.(config.SharedConfig); ok {
			provider, err := boundedProfileChain(*cfg, &shared)
			if err != nil {
				return err
			}
			// Checks are one-shot. SDK CredentialsCache suppresses cancellation
			// during refresh; direct providers keep process/role work joined to
			// the requesting check instead of leaving a detached refresh behind.
			cfg.Credentials = provider
			return nil
		}
	}
	return errCheckProcess
}

func boundedProfileChain(cfg aws.Config, shared *config.SharedConfig) (aws.CredentialsProvider, error) {
	var provider aws.CredentialsProvider
	if shared.Source != nil {
		var err error
		provider, err = boundedProfileChain(cfg, shared.Source)
		if err != nil {
			return nil, err
		}
	} else if shared.CredentialProcess != "" {
		provider = boundedCheckProcess{command: shared.CredentialProcess}
	} else {
		return nil, errCheckProcess
	}
	if shared.RoleARN != "" {
		cfg.Credentials = provider
		provider = stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), shared.RoleARN, func(options *stscreds.AssumeRoleOptions) {
			options.RoleSessionName = shared.RoleSessionName
			if shared.RoleDurationSeconds != nil && *shared.RoleDurationSeconds > 15*time.Minute {
				options.Duration = *shared.RoleDurationSeconds
			}
			if shared.ExternalID != "" {
				options.ExternalID = aws.String(shared.ExternalID)
			}
			if shared.MFASerial != "" {
				options.SerialNumber = aws.String(shared.MFASerial)
			}
		})
	}
	return provider, nil
}

var _ aws.CredentialsProvider = boundedCheckProcess{}
