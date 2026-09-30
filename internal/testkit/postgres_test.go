package testkit

import "testing"

func TestDatabaseURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "missing", value: "", wantErr: true},
		{name: "invalid scheme", value: "https://db.example/test", wantErr: true},
		{name: "missing database", value: "postgres://db.example", wantErr: true},
		{name: "valid postgres URL", value: "postgres://localhost/test?sslmode=disable"},
		{name: "valid postgresql URL", value: "postgresql://localhost/test?sslmode=disable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(DatabaseURLEnv, test.value)
			got, err := DatabaseURL()
			if test.wantErr {
				if err == nil {
					t.Fatal("DatabaseURL() succeeded; want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DatabaseURL() error = %v", err)
			}
			if got != test.value {
				t.Fatalf("DatabaseURL() = %q, want %q", got, test.value)
			}
		})
	}
}
