package config

import "testing"

func TestParseAddrAcceptsOnlyLoopbackWithNumericPort(t *testing.T) {
	for _, raw := range []string{"127.0.0.1:0", "localhost:7926", "[::1]:65535"} {
		if _, err := ParseAddr(raw); err != nil {
			t.Errorf("ParseAddr(%q): %v", raw, err)
		}
	}
	for _, raw := range []string{"127.0.0.1", "0.0.0.0:7926", "192.168.1.4:7926", "remote.example:7926", "localhost:http", "localhost:-1", "localhost:65536", ":7926"} {
		if _, err := ParseAddr(raw); err == nil {
			t.Errorf("ParseAddr(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestResolveStartupConfigPrecedence(t *testing.T) {
	t.Setenv(EnvAddr, "127.0.0.1:8123")

	environment, err := ResolveStartupConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if environment.Addr != "127.0.0.1:8123" {
		t.Fatalf("environment address = %q", environment.Addr)
	}
	flag, err := ResolveStartupConfig("127.0.0.1:9000")
	if err != nil {
		t.Fatal(err)
	}
	if flag.Addr != "127.0.0.1:9000" {
		t.Fatalf("flag address = %q", flag.Addr)
	}
}

func TestDefaultAddrAndBaseURL(t *testing.T) {
	t.Setenv(EnvAddr, "")
	config, err := ResolveStartupConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if config.Addr != "127.0.0.1:7926" || BaseURL(config.Addr) != "http://127.0.0.1:7926" {
		t.Fatalf("startup config = %#v", config)
	}
}
