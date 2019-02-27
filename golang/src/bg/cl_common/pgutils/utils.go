/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package pgutils

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh/terminal"
)

// CensorPassword replaces the password in a Postgres connection string with a
// dummy password, suitable for logging.
func CensorPassword(connInfo string) string {
	dummy := "********"

	if strings.HasPrefix(connInfo, "postgres://") ||
		strings.HasPrefix(connInfo, "postgresql://") {
		theURL, _ := url.Parse(connInfo)

		// See if the query string has the password.
		q := theURL.Query()
		if q.Get("password") != "" {
			q.Set("password", dummy)
		}
		theURL.RawQuery = q.Encode()

		// See if the password is in front of the host.  We can't actually
		// test this due to https://github.com/lib/pq/issues/796
		if _, pwset := theURL.User.Password(); pwset {
			theURL.User = url.UserPassword(theURL.User.Username(), dummy)
		}

		// Force the censored password to show up as literal asterisks,
		// not URL-encoded ones.
		return strings.Replace(theURL.String(),
			"password="+url.QueryEscape(dummy), "password="+dummy, -1)
	}

	// This won't work with passwords containing spaces.  Ideally we'd use
	// https://github.com/lib/pq/pull/375
	re := regexp.MustCompile(`\bpassword=[^ ]*`)
	return re.ReplaceAllString(connInfo, "password="+dummy)
}

// HasPassword checks the connection URI to see if it specifies the password.
func HasPassword(connInfo string) bool {
	if strings.HasPrefix(connInfo, "postgres://") ||
		strings.HasPrefix(connInfo, "postgresql://") {
		theURL, _ := url.Parse(connInfo)

		// See if the query string has the password.
		q := theURL.Query()
		return q.Get("password") != ""
	}

	re := regexp.MustCompile(`\bpassword=[^ ]*`)
	return re.MatchString(connInfo)
}

// AddPassword adds the password to the connection URI.
func AddPassword(connInfo, password string) string {
	if strings.HasPrefix(connInfo, "postgres://") ||
		strings.HasPrefix(connInfo, "postgresql://") {
		theURL, _ := url.Parse(connInfo)

		q := theURL.Query()
		q.Set("password", password)
		theURL.RawQuery = q.Encode()
		return theURL.String()
	}
	return connInfo
}

// PasswordPrompt prompts at the terminal for a password (if the given URI
// doesn't have one) and returns a URI with the password added.
func PasswordPrompt(dbURI string) (string, error) {
	if !HasPassword(dbURI) {
		fmt.Print("Enter DB password: ")
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return "", err
		}
		dbURI = AddPassword(dbURI, string(bytePassword))
	}
	return dbURI, nil
}
