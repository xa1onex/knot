package builds

import "testing"

func TestValidateSourceURL(t *testing.T) {
	if err := validateSourceURL("https://git.example.com/app.git"); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceURL("knot-fake-git:ok"); err != nil {
		t.Fatal(err)
	}
	if err := validateSourceURL("https://user:token@git.example.com/app.git"); err == nil {
		t.Fatal("expected reject of credentials in url")
	}
	if err := validateSourceURL("git@github.com:org/app.git"); err == nil {
		t.Fatal("expected reject of ssh")
	}
}

func TestValidateRelPath(t *testing.T) {
	if _, err := validateRelPath("Dockerfile", "dockerfile"); err != nil {
		t.Fatal(err)
	}
	if _, err := validateRelPath("../Dockerfile", "dockerfile"); err == nil {
		t.Fatal("expected reject ..")
	}
	if _, err := validateRelPath("/etc/passwd", "dockerfile"); err == nil {
		t.Fatal("expected reject abs")
	}
}

func TestDefaultNameFromURL(t *testing.T) {
	if got := defaultNameFromURL("https://git.example.com/web-app.git"); got != "web-app" {
		t.Fatalf("got %q", got)
	}
}
