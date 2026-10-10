// Package version names the running build, to the user and to every remote
// service the assistant talks to.
package version

// Version is replaced at release time with
// -ldflags "-X github.com/nmcginn/mtga-draft-assistant/internal/version.Version=v1.2.3".
var Version = "dev"

// Name is the product name: the binary, the window title's root, and the
// directory the cache and settings live under.
const Name = "mtga-draft-assistant"

// homepage is where an operator who sees our traffic can find out who we are.
const homepage = "https://github.com/nmcginn/mtga-draft-assistant"

// UserAgent is the User-Agent every outbound request carries. Scryfall
// requires one that names the application, and a service owner deciding
// whether our traffic is welcome deserves to know whose it is (decision 9).
func UserAgent() string {
	return Name + "/" + Version + " (+" + homepage + ")"
}
