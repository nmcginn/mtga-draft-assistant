package version

import (
	"strings"
	"testing"
)

func TestUserAgentNamesTheAppItsVersionAndWhereToFindUs(t *testing.T) {
	ua := UserAgent()
	if !strings.HasPrefix(ua, Name+"/"+Version+" ") {
		t.Errorf("UserAgent() = %q, want it to start with the product token %q", ua, Name+"/"+Version)
	}
	if !strings.Contains(ua, homepage) {
		t.Errorf("UserAgent() = %q, want it to carry the homepage %q", ua, homepage)
	}
}

func TestUserAgentProductTokenHasNoSpaces(t *testing.T) {
	// RFC 9110: a product token is a single token; a space would split it and
	// make the version unreadable to the server's logs.
	token, _, _ := strings.Cut(UserAgent(), " ")
	if token != Name+"/"+Version {
		t.Errorf("product token = %q, want %q", token, Name+"/"+Version)
	}
}
