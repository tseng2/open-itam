package store

import (
	"context"
	"testing"
	"time"

	"itagent/internal/server/model"
)

func TestGetProtectionModuleDefaultsToDisabled(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	m, err := s.GetProtectionModule(ctx, model.ProtectionModuleQuit)
	if err != nil {
		t.Fatalf("get unconfigured module: %v", err)
	}
	if m.ModuleKey != model.ProtectionModuleQuit {
		t.Fatalf("expected module key %q, got %q", model.ProtectionModuleQuit, m.ModuleKey)
	}
	if m.Enabled {
		t.Fatal("unconfigured module must default to disabled")
	}
	if m.PasswordHash != "" {
		t.Fatal("unconfigured module must have empty password hash")
	}
}

func TestPutProtectionModuleUpsert(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.PutProtectionModule(ctx, model.ProtectionModule{
		ModuleKey:    model.ProtectionModuleUninstall,
		Enabled:      true,
		PasswordHash: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$a2V5",
	}); err != nil {
		t.Fatalf("put module: %v", err)
	}

	m, err := s.GetProtectionModule(ctx, model.ProtectionModuleUninstall)
	if err != nil {
		t.Fatalf("get module: %v", err)
	}
	if !m.Enabled || m.PasswordHash == "" {
		t.Fatalf("expected enabled module with hash, got enabled=%v hash=%q", m.Enabled, m.PasswordHash)
	}

	if err := s.PutProtectionModule(ctx, model.ProtectionModule{
		ModuleKey: model.ProtectionModuleUninstall,
		Enabled:   false,
	}); err != nil {
		t.Fatalf("put module again: %v", err)
	}
	m, err = s.GetProtectionModule(ctx, model.ProtectionModuleUninstall)
	if err != nil {
		t.Fatalf("get module after update: %v", err)
	}
	if m.Enabled {
		t.Fatal("module should be disabled after update")
	}
}

func TestPutProtectionModuleRejectsUnknownKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.PutProtectionModule(ctx, model.ProtectionModule{ModuleKey: "bogus"}); err == nil {
		t.Fatal("expected error for unknown module key")
	}
}

func TestCreateUninstallCodeBindsDeviceAndTTL(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	uc, err := s.CreateUninstallCode(ctx, "dev001", model.UninstallCodeTTL)
	if err != nil {
		t.Fatalf("create uninstall code: %v", err)
	}
	if uc.Code == "" {
		t.Fatal("expected non-empty code")
	}
	if uc.DeviceID != "dev001" {
		t.Fatalf("expected code bound to dev001, got %q", uc.DeviceID)
	}
	if uc.UsedAt != nil {
		t.Fatal("fresh code must not be marked used")
	}
	if time.Until(uc.ExpiresAt) > model.UninstallCodeTTL || time.Until(uc.ExpiresAt) <= 0 {
		t.Fatalf("expected expiry within ttl, got %v", time.Until(uc.ExpiresAt))
	}
}

func TestCreateUninstallCodeRejectsEmptyDevice(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateUninstallCode(ctx, "", model.UninstallCodeTTL); err == nil {
		t.Fatal("expected error for empty device_id")
	}
}

func TestVerifyUninstallCodeSuccessMarksUsed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	uc, err := s.CreateUninstallCode(ctx, "dev001", model.UninstallCodeTTL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.VerifyUninstallCode(ctx, "dev001", uc.Code); err != nil {
		t.Fatalf("verify valid code: %v", err)
	}
	if err := s.VerifyUninstallCode(ctx, "dev001", uc.Code); err != ErrUnauthorized {
		t.Fatalf("replay must be rejected (single use), got %v", err)
	}
}

func TestVerifyUninstallCodeRejectsWrongCodeAndDevice(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	uc, err := s.CreateUninstallCode(ctx, "dev001", model.UninstallCodeTTL)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.VerifyUninstallCode(ctx, "dev001", "99999999"); err != ErrUnauthorized {
		t.Fatalf("wrong code must be rejected, got %v", err)
	}
	if err := s.VerifyUninstallCode(ctx, "dev002", uc.Code); err != ErrUnauthorized {
		t.Fatalf("code bound to another device must be rejected, got %v", err)
	}
	if err := s.VerifyUninstallCode(ctx, "dev001", ""); err != ErrUnauthorized {
		t.Fatalf("empty code must be rejected, got %v", err)
	}
}

func TestVerifyUninstallCodeRejectsExpired(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	uc, err := s.CreateUninstallCode(ctx, "dev001", -time.Minute)
	if err != nil {
		t.Fatalf("create with negative ttl: %v", err)
	}
	if err := s.VerifyUninstallCode(ctx, "dev001", uc.Code); err != ErrUnauthorized {
		t.Fatalf("expired code must be rejected, got %v", err)
	}
}
