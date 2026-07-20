package media

import (
	"bytes"
	"image"
	"image/png"
	"net"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestPublicPolicyRejectsLocalAndPrivateDestinations(t *testing.T) {
	policy, _ := provider.DefaultImageFetchPolicy().NetworkPolicy()
	for _, host := range []string{"localhost", "service.local", "x.localhost"} {
		if err := authorizeHostname(host, policy); err == nil {
			t.Fatalf("hostname %q accepted", host)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "::1", "fc00::1", "fe80::1", "224.0.0.1"} {
		if authorizedIP(net.ParseIP(raw), "example.test", policy) {
			t.Fatalf("address %s accepted", raw)
		}
	}
}

func TestAllowlistExplicitlyOptsIntoPrivateWorkspaceMedia(t *testing.T) {
	policy, _ := provider.DefaultImageFetchPolicy().NetworkPolicy()
	policy.Access = provider.NetworkAllowlisted
	mediaInternal, _ := provider.NewHostPattern("media.internal")
	localhost, _ := provider.NewHostPattern("localhost")
	policy.AllowedHosts = []provider.HostPattern{mediaInternal, localhost}
	if err := authorizeHostname("localhost", policy); err != nil || !authorizedIP(net.ParseIP("127.0.0.1"), "localhost", policy) {
		t.Fatalf("explicit localhost allowlist was not honored: %v", err)
	}
	if err := authorizeHostname("other.internal", policy); err == nil {
		t.Fatal("unlisted private hostname was accepted")
	}
}

func TestValidateImageDecodesHeaderAndEnforcesDeclaredTypeAndDimensions(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	limits := provider.DefaultMediaLimits()
	got, err := provider.InspectImage(canonical.ImageMediaPNG, encoded.Bytes(), limits)
	if err != nil || got.Width != 3 || got.Height != 2 {
		t.Fatalf("validated image = %#v err=%v", got, err)
	}
	if _, err := provider.InspectImage(canonical.ImageMediaJPEG, encoded.Bytes(), limits); err == nil {
		t.Fatal("contradictory media declaration accepted")
	}
	limits.MaxImageDimension = 2
	if _, err := provider.InspectImage(canonical.ImageMediaPNG, encoded.Bytes(), limits); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
