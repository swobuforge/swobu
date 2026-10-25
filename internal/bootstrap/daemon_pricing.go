package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/tariff"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

type pricingBinding struct {
	TargetID        string `json:"target_id"`
	TargetVersion   uint64 `json:"target_version"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	BaseURL         string `json:"base_url"`
	ExternalVersion string `json:"external_version"`
	Tariff          string `json:"tariff"`
	InputForecast   int64  `json:"input_forecast_tokens"`
	OutputForecast  int64  `json:"output_forecast_tokens"`
}

// daemonPricing uses explicit operator forecasts, never a tokenizer heuristic.
// Every quote is partial; forecasts cannot establish a maximum provider bill.
type daemonPricing struct {
	reader   *tariff.Reader
	bindings []pricingBinding
}

func openDaemonPricing(ctx context.Context) (*daemonPricing, error) {
	path := os.Getenv("SWOBU_PRICING_CONFIG")
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, errors.New("SWOBU_PRICING_CONFIG must be an absolute path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("pricing configuration could not be read")
	}
	var settings struct {
		Database string           `json:"models_database"`
		Bindings []pricingBinding `json:"bindings"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return nil, errors.New("invalid pricing configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid pricing configuration trailing data")
	}
	if !filepath.IsAbs(settings.Database) || len(settings.Bindings) == 0 {
		return nil, errors.New("pricing requires an absolute Models database and explicit bindings")
	}
	seen := map[string]bool{}
	for _, binding := range settings.Bindings {
		if binding.TargetID == "" || binding.TargetVersion == 0 || binding.Provider == "" || binding.Model == "" || binding.BaseURL == "" || binding.Tariff == "" || binding.InputForecast <= 0 || binding.OutputForecast <= 0 || seen[binding.TargetID] {
			return nil, errors.New("invalid or duplicate target pricing binding")
		}
		seen[binding.TargetID] = true
		for _, identity := range []string{binding.TargetID, binding.Provider, binding.Model, binding.BaseURL, binding.ExternalVersion, binding.Tariff} {
			if identity != strings.TrimSpace(identity) {
				return nil, errors.New("pricing binding identities must be exact")
			}
		}
	}
	reader, err := tariff.Open(ctx, settings.Database)
	if err != nil {
		return nil, errors.New("pricing Models database unavailable")
	}
	return &daemonPricing{reader: reader, bindings: settings.Bindings}, nil
}

func (pricing *daemonPricing) Estimate(target provider.TargetSnapshot, _ canonical.CanonicalRequest) (accounting.Quote, bool) {
	for _, binding := range pricing.bindings {
		if binding.TargetID != target.TargetID || binding.TargetVersion != target.TargetVersion || binding.Provider != target.ProviderSpec || binding.Model != target.Model || binding.BaseURL != target.BaseURL {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		version, err := pricing.reader.Select(ctx, tariff.Selection{
			Provider: binding.Provider, ExternalID: binding.Model, ExternalVersion: binding.ExternalVersion, Tariff: binding.Tariff, At: time.Now(), InputTokens: binding.InputForecast,
		})
		cancel()
		if err != nil {
			return accounting.Quote{}, false
		}
		quote, err := version.Quote(target.TargetID, target.TargetVersion, binding.InputForecast, binding.OutputForecast, "partial")
		return quote, err == nil
	}
	return accounting.Quote{}, false
}
