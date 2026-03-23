package main

import "testing"

func TestVersionFromArchiveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		archive   string
		want      string
		expectErr bool
	}{
		{
			name:    "darwin tarball",
			archive: "dist/minecraft-ping_2.0.3_Darwin_arm64.tar.gz",
			want:    "2.0.3",
		},
		{
			name:    "snapshot windows zip",
			archive: "dist/minecraft-ping_2.0.3-SNAPSHOT-d526d04_Windows_amd64.zip",
			want:    "2.0.3-SNAPSHOT-d526d04",
		},
		{
			name:      "unexpected prefix",
			archive:   "dist/other_2.0.3_Linux_amd64.tar.gz",
			expectErr: true,
		},
		{
			name:      "unexpected suffix",
			archive:   "dist/minecraft-ping_2.0.3_linux_amd64.deb",
			expectErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := versionFromArchiveName(test.archive)
			if test.expectErr {
				if err == nil {
					t.Fatalf("versionFromArchiveName(%q) returned %q, want error", test.archive, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("versionFromArchiveName(%q) error: %v", test.archive, err)
			}
			if got != test.want {
				t.Fatalf("versionFromArchiveName(%q) = %q, want %q", test.archive, got, test.want)
			}
		})
	}
}

func TestVersionLine(t *testing.T) {
	t.Parallel()

	if got, want := versionLine("2.0.3"), "minecraft-ping 2.0.3"; got != want {
		t.Fatalf("versionLine() = %q, want %q", got, want)
	}
}
