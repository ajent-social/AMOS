package federation_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/ajent-social/amos/identity/federation"
	"github.com/ajent-social/amos/identity/session"
	aw "github.com/ajent-social/amos/internal/authoritywriter"
	"github.com/ajent-social/amos/storage"
	"github.com/google/uuid"
)

type federationProfileProvider struct{}

func (federationProfileProvider) AuthorizationURL(context.Context, federation.Authorization) (string, error) {
	return "https://provider.example.test/authorize", nil
}

func (federationProfileProvider) Exchange(context.Context, string, string, string) (federation.Identity, error) {
	return federation.Identity{}, errors.New("unused")
}

func TestLegacyFederationNewRejectsWriterProfile(t *testing.T) {
	const helper = "AMOS_FEDERATION_W1_PROFILE_HELPER"
	if os.Getenv(helper) == "1" {
		root, err := aw.New(new(storage.RuntimeDB))
		if err != nil {
			t.Fatal(err)
		}
		if err := aw.ActivateW1(root); err != nil {
			t.Fatal(err)
		}
		sessions, err := session.NewWithWriter(root, session.Config{
			InstallationID: uuid.Must(uuid.NewV7()), ApplicationID: uuid.Must(uuid.NewV7()), EnvironmentID: uuid.Must(uuid.NewV7()),
			AllowedOrigins: []string{"https://app.example.com"}, CookieSecure: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = federation.New(federation.Config{
			DB: new(storage.DB), Sessions: sessions,
			InstallationID: uuid.Must(uuid.NewV7()), ApplicationID: uuid.Must(uuid.NewV7()), EnvironmentID: uuid.Must(uuid.NewV7()),
			SecureCookies: true, CallbackURL: "https://app.example.com/federation/callback",
			Providers: map[string]federation.Provider{"test": federationProfileProvider{}},
			Connections: map[string]federation.Connection{"test": {
				Provider: "test", ProviderConnectionID: uuid.Must(uuid.NewV7()), Issuer: "https://issuer.example.test",
			}},
		})
		if !errors.Is(err, federation.ErrInvalidInput) {
			t.Fatalf("legacy constructor accepted W1 profile: %v", err)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestLegacyFederationNewRejectsWriterProfile$")
	cmd.Env = append(os.Environ(), helper+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated W1 profile regression failed: %v\n%s", err, output)
	}
}
