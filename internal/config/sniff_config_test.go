package config

import "testing"

func TestNormalizeShadowsocksSniffConfigs(t *testing.T) {
	got, err := normalizeShadowsocksSniffConfigs([]rawShadowsocksSniffConfig{{
		Name:     " landing ",
		Server:   " 203.0.113.10 ",
		Port:     39281,
		Cipher:   " AES-256-GCM ",
		Password: "secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(configs)=%d, want 1", len(got))
	}
	if got[0].Name != "landing" || got[0].Server != "203.0.113.10" {
		t.Fatalf("config trim result=%+v", got[0])
	}
	if got[0].Method != "aes-256-gcm" {
		t.Fatalf("method=%q, want normalized method", got[0].Method)
	}
}

func TestNormalizeShadowsocksSniffConfigsRejectsInvalidConfig(t *testing.T) {
	_, err := normalizeShadowsocksSniffConfigs([]rawShadowsocksSniffConfig{{
		Server: "203.0.113.10",
		Port:   70000,
		Method: "aes-256-gcm",
	}})
	if err == nil {
		t.Fatal("expected invalid sniff config error")
	}
}
