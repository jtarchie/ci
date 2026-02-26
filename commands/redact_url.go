package commands

import "net/url"

// redactURL returns the URL string with any password replaced by "xxxxx",
// so it is safe to include in log output.
func redactURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}

	return u.String()
}
