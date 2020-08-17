/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

func hasParameter(connInfo, name string) bool {
	if strings.HasPrefix(connInfo, "postgres://") ||
		strings.HasPrefix(connInfo, "postgresql://") {
		theURL, _ := url.Parse(connInfo)

		// See if the query string has the password.
		q := theURL.Query()
		return q.Get(name) != ""
	}

	re := regexp.MustCompile(fmt.Sprintf(`\b%s=[^ ]*`, name))
	return re.MatchString(connInfo)
}

// HasPassword checks the connection URI to see if it specifies the password.
func HasPassword(connInfo string) bool {
	return hasParameter(connInfo, "password")
}

// HasUsername checks the connection URI to see if it specifies the username.
func HasUsername(connInfo string) bool {
	return hasParameter(connInfo, "user")
}

func addParameter(connInfo, name, value string) string {
	if strings.HasPrefix(connInfo, "postgres://") ||
		strings.HasPrefix(connInfo, "postgresql://") {
		theURL, _ := url.Parse(connInfo)

		q := theURL.Query()
		q.Set(name, value)
		theURL.RawQuery = q.Encode()
		return theURL.String()
	}
	return connInfo
}

// AddPassword adds the password to the connection URI.
func AddPassword(connInfo, password string) string {
	return addParameter(connInfo, "password", password)
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

// AddUsername adds the username to the connection URI.
func AddUsername(connInfo, username string) string {
	return addParameter(connInfo, "user", username)
}

// AddApplication adds the application name to the connection URI.
func AddApplication(connInfo, app string) string {
	return addParameter(connInfo, "application_name", app)
}

// AddTimezone adds the timezone to the connection URI.
func AddTimezone(connInfo, zone string) string {
	return addParameter(connInfo, "timezone", zone)
}

// AddConnectTimeout adds the connection timeout (as an integer number of
// seconds, represented as a string) to the connection URI.
func AddConnectTimeout(connInfo, timeout string) string {
	return addParameter(connInfo, "connect_timeout", timeout)
}

