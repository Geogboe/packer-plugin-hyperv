package common

import "testing"

func TestPowershellDirectConfigPrepare(t *testing.T) {
	t.Run("missing username", func(t *testing.T) {
		cfg := PowershellDirectConfig{Password: "secret"}
		errs := cfg.Prepare()
		if len(errs) != 1 {
			t.Fatalf("expected a single error, got %d", len(errs))
		}
	})

	t.Run("missing password", func(t *testing.T) {
		cfg := PowershellDirectConfig{Username: "packer"}
		errs := cfg.Prepare()
		if len(errs) != 1 {
			t.Fatalf("expected a single error, got %d", len(errs))
		}
	})

	t.Run("valid configuration", func(t *testing.T) {
		cfg := PowershellDirectConfig{VMName: "existing", Username: "packer", Password: "secret"}
		errs := cfg.Prepare()
		if len(errs) != 0 {
			t.Fatalf("expected zero errors, got %d", len(errs))
		}

		communicatorCfg := cfg.CommunicatorConfig()
		if communicatorCfg.VMName != cfg.VMName {
			t.Fatalf("expected vm name %q, got %q", cfg.VMName, communicatorCfg.VMName)
		}
		if communicatorCfg.Username != cfg.Username {
			t.Fatalf("expected username %q, got %q", cfg.Username, communicatorCfg.Username)
		}
		if communicatorCfg.Password != cfg.Password {
			t.Fatalf("expected password %q, got %q", cfg.Password, communicatorCfg.Password)
		}
	})
}
