package requestpath

import (
	"context"

	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

type AttemptExecutorFn func(context.Context, ExecutionAttempt) AttemptOutcome
type AttemptMiddleware func(AttemptExecutorFn) AttemptExecutorFn

type chainedAttemptPipeline struct {
	invoke AttemptExecutorFn
}

func (k chainedAttemptPipeline) Execute(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
	return k.invoke(ctx, attempt)
}

func chainAttemptPipeline(terminal AttemptExecutorFn, middlewares ...AttemptMiddleware) AttemptExecutorFn {
	chain := terminal
	for i := len(middlewares) - 1; i >= 0; i-- {
		chain = middlewares[i](chain)
	}
	return chain
}

func (o RequestHandler) defaultAttemptPipeline() AttemptPipeline {
	return chainedAttemptPipeline{
		invoke: chainAttemptPipeline(
			o.terminalAttempt(),
			o.runtimeEvidenceMiddleware(),
			timeoutMiddleware(),
			continuationMiddleware(),
			o.toolChoicePolicyMiddleware(),
		),
	}
}

func (o RequestHandler) executeAttempt(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
	if o.attemptPipeline == nil {
		return AttemptOutcome{
			Err: compatibility.InternalError("attempt pipeline is not configured"),
		}
	}
	return o.attemptPipeline.Execute(ctx, attempt)
}

func (o RequestHandler) terminalAttempt() AttemptExecutorFn {
	return func(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
		req := ports.NewExecuteRequest(attempt.Request, attempt.Contract, attempt.Route.Target)
		resp, err := o.providers.Execute(ctx, req)
		return AttemptOutcome{
			Response: resp,
			Err:      err,
		}
	}
}

func (o RequestHandler) runtimeEvidenceMiddleware() AttemptMiddleware {
	return func(next AttemptExecutorFn) AttemptExecutorFn {
		return func(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
			if o.evidence != nil {
				event, err := newInflightEvidenceEvent(
					attempt.Intent.RequestID,
					attempt.Intent.EndpointName,
					attempt.Route.Target,
					attempt.Intent.Provenance,
					attempt.Intent.RequestedModel,
					attempt.Route.EffectiveModel,
					attempt.Route.ResolutionMode,
				)
				emitEvidenceEventIfValid(ctx, o.evidence, event, err)
			}

			outcome := next(ctx, attempt)
			if outcome.Err != nil {
				if o.evidence != nil {
					event, eventErr := newErrorEvidenceEvent(
						attempt.Intent.RequestID,
						attempt.Intent.EndpointName,
						attempt.Route.Target,
						attempt.Intent.Provenance,
						outcome.Err,
						attempt.Intent.RequestedModel,
						attempt.Route.EffectiveModel,
						attempt.Route.ResolutionMode,
					)
					emitEvidenceEventIfValid(ctx, o.evidence, event, eventErr)
				}
				return outcome
			}

			if o.evidence != nil {
				metadata := outcome.Response.Metadata()
				attemptCount := metadata.AttemptCount
				if attemptCount <= 0 {
					attemptCount = 1
				}
				event, eventErr := newSuccessEvidenceEvent(
					attempt.Intent.RequestID,
					attempt.Intent.EndpointName,
					attempt.Route.Target,
					attempt.Intent.Provenance,
					attemptCount,
					metadata.ContinuityRecovered,
					metadata.ContinuityRecoveryTrigger,
					attempt.Intent.RequestedModel,
					attempt.Route.EffectiveModel,
					attempt.Route.ResolutionMode,
					tokenUsageFromExecuteResponse(outcome.Response),
				)
				emitEvidenceEventIfValid(ctx, o.evidence, event, eventErr)
				if outcome.Response.DeliveryMode() == compatibility.DeliveryModeStreaming && outcome.Response.Stream() != nil {
					wrapped := wrapEvidenceStreamWithUsageReconciliation(
						ctx,
						o.evidence,
						outcome.Response.Stream(),
						attempt.Intent.RequestID,
						attempt.Intent.EndpointName,
						attempt.Route.Target,
						attempt.Intent.Provenance,
						attemptCount,
						metadata.ContinuityRecovered,
						metadata.ContinuityRecoveryTrigger,
						attempt.Intent.RequestedModel,
						attempt.Route.EffectiveModel,
						attempt.Route.ResolutionMode,
					)
					outcome.Response = ports.NewStreamingExecuteResponse(wrapped).WithMetadata(outcome.Response.Metadata())
				}
			}
			return outcome
		}
	}
}

func timeoutMiddleware() AttemptMiddleware {
	return func(next AttemptExecutorFn) AttemptExecutorFn {
		return func(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
			return next(ctx, attempt)
		}
	}
}

func continuationMiddleware() AttemptMiddleware {
	return func(next AttemptExecutorFn) AttemptExecutorFn {
		return func(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
			prepared, err := attempt.Continuation.PrepareRequest(
				ctx,
				compatibility.NewContinuationNamespace(attempt.Intent.EndpointName.String()),
				attempt.Route.Target.ProtocolKind,
				attempt.Request,
			)
			if err != nil {
				return AttemptOutcome{Err: err}
			}
			executedRequest := prepared
			preparedAttempt := attempt
			preparedAttempt.Request = prepared
			preparedAttempt.Index = attempt.Index

			outcome := next(ctx, preparedAttempt)
			if outcome.Err != nil {
				typed, ok := prepared.(compatibility.GenerationCanonicalRequest)
				if !ok || !compatibility.IsPreviousResponseNotFoundBackendError(outcome.Err) {
					return outcome
				}
				fallback, ok := fullThreadResponsesRequest(typed)
				if !ok {
					return outcome
				}
				retryAttempt := preparedAttempt
				retryAttempt.Request = fallback
				retryAttempt.Index = preparedAttempt.Index + 1
				retryOutcome := next(ctx, retryAttempt)
				if retryOutcome.Err != nil {
					return retryOutcome
				}
				retryMetadata := retryOutcome.Response.Metadata()
				retryMetadata.AttemptCount = 2
				retryMetadata.ContinuityRecovered = true
				retryMetadata.ContinuityRecoveryTrigger = "previous_response_not_found"
				outcome = AttemptOutcome{
					Response: retryOutcome.Response.WithMetadata(retryMetadata),
				}
				executedRequest = fallback
			}

			metadata := outcome.Response.Metadata()
			if metadata.AttemptCount <= 0 {
				metadata.AttemptCount = 1
				outcome.Response = outcome.Response.WithMetadata(metadata)
			}

			namespace := compatibility.NewContinuationNamespace(attempt.Intent.EndpointName.String())
			switch outcome.Response.DeliveryMode() {
			case compatibility.DeliveryModeBuffered:
				if err := attempt.Continuation.CaptureBuffered(ctx, namespace, executedRequest, outcome.Response.Output()); err != nil {
					return AttemptOutcome{Err: err}
				}
			case compatibility.DeliveryModeStreaming:
				stream, wrapErr := attempt.Continuation.WrapStream(ctx, namespace, executedRequest, outcome.Response.Stream())
				if wrapErr != nil {
					return AttemptOutcome{Err: wrapErr}
				}
				outcome.Response = ports.NewStreamingExecuteResponse(stream).WithMetadata(outcome.Response.Metadata())
			}
			return outcome
		}
	}
}

func (o RequestHandler) toolChoicePolicyMiddleware() AttemptMiddleware {
	return func(next AttemptExecutorFn) AttemptExecutorFn {
		return func(ctx context.Context, attempt ExecutionAttempt) AttemptOutcome {
			typed, ok := attempt.Request.(compatibility.GenerationCanonicalRequest)
			if !ok {
				return next(ctx, attempt)
			}
			outcome := next(ctx, attempt)
			if outcome.Err != nil &&
				attempt.Capabilities.ToolChoice.ImmediateDowngradeRetry &&
				typed.ToolMode() == compatibility.ToolModeRequired &&
				compatibility.IsBackendErrorClass(outcome.Err, compatibility.BackendErrorClassToolChoiceUnsupported) {
				retryAttempt := attempt
				retryAttempt.Request = withResponseToolChoiceMode(typed, compatibility.ToolModeAuto)
				retryAttempt.Index = attempt.Index + 1
				retryOutcome := next(ctx, retryAttempt)
				if retryOutcome.Err != nil {
					return retryOutcome
				}
				metadata := retryOutcome.Response.Metadata()
				if metadata.AttemptCount <= 0 {
					metadata.AttemptCount = 2
				} else {
					metadata.AttemptCount++
				}
				return AttemptOutcome{
					Response: retryOutcome.Response.WithMetadata(metadata),
				}
			}
			return outcome
		}
	}
}

func withResponseToolChoiceMode(request compatibility.GenerationCanonicalRequest, mode compatibility.ToolMode) compatibility.GenerationCanonicalRequest {
	return compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model:                request.Model(),
		Thread:               request.Thread(),
		LastTurn:             request.LastTurn(),
		PreviousResponseID:   request.PreviousResponseID(),
		ConversationID:       request.ConversationID(),
		ToolMode:             mode,
		PromptCacheKey:       request.PromptCacheKey(),
		PromptCacheRetention: request.PromptCacheRetention(),
	})
}

func fullThreadResponsesRequest(request compatibility.GenerationCanonicalRequest) (compatibility.GenerationCanonicalRequest, bool) {
	thread := request.Thread()
	if len(thread) == 0 {
		return compatibility.GenerationCanonicalRequest{}, false
	}
	return compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model:                request.Model(),
		Thread:               thread,
		ToolMode:             request.ToolMode(),
		PromptCacheKey:       request.PromptCacheKey(),
		PromptCacheRetention: request.PromptCacheRetention(),
	}), true
}
