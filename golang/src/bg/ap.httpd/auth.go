/*
 * Copyright 2018 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"encoding/json"
	"io"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/gorilla/mux"
)

const (
	// Note that the naming of the cookie is significant.  The
	// __Host- prefix asserts that some of the security attributes
	// we desire for our cookies are also True.  See
	// https://tools.ietf.org/html/draft-ietf-httpbis-rfc6265bis-05#page-14
	sessionCookieName = "__Host-com.brightgate.appliance-login"
)

func makeApplianceAuthRouter() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/providers", providersHandler).Methods("GET")
	router.HandleFunc("/userid", userIDHandler).Methods("GET")
	router.HandleFunc("/site/login", siteLoginHandler).Methods("POST")
	router.HandleFunc("/logout", logoutHandler).Methods("GET")
	return router
}

func providersHandler(w http.ResponseWriter, r *http.Request) {
	providers := struct {
		Mode      string   `json:"mode"`
		Providers []string `json:"providers"`
	}{
		Mode:      "local",
		Providers: []string{"password"},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(providers); err != nil {
		panic(err)
	}
}

// POST login () -> (...)
// POST uid, userPassword
func siteLoginHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		slog.Infof("cannot parse form: %v", err)
		http.Error(w, "bad request", 400)
		return
	}

	// Must have user and password.
	uids, present := r.Form["uid"]
	if !present || len(uids) == 0 {
		slog.Infof("incomplete form, uid")
		http.Error(w, "bad request", 400)
		return
	}

	uid := uids[0]
	if len(uids) > 1 {
		slog.Infof("multiple uids in form submission: %v", uids)
		http.Error(w, "bad request", 400)
	}

	userPasswords, present := r.Form["userPassword"]
	if !present || len(userPasswords) == 0 {
		slog.Infof("incomplete form, userPassword")
		http.Error(w, "bad request", 400)
		return
	}

	userPassword := userPasswords[0]
	if len(userPasswords) > 1 {
		slog.Infof("multiple userPasswords in form submission: %v", userPasswords)
		http.Error(w, "bad request", 400)
	}

	// Retrieve user record
	ui, err := config.GetUser(uid)
	if err != nil {
		slog.Infof("login for '%s' denied: %v", uid, err)
		http.Error(w, "login denied", 401)
		return
	}

	if ui.SelfProvisioning {
		slog.Infof("login for '%s' denied: self provisioned user", uid, err)
		http.Error(w, "login denied: cloud-self-provisioned users may not login", 401)
		return
	}

	cmp := bcrypt.CompareHashAndPassword([]byte(ui.Password),
		[]byte(userPassword))
	if cmp != nil {
		slog.Infof("demo login for '%s' denied: password comparison", uid)
		http.Error(w, "login denied", 401)
		return
	}

	filling := map[string]string{
		"uid": uid,
	}

	if encoded, err := cutter.Encode(sessionCookieName, filling); err == nil {
		cookie := &http.Cookie{
			Name:  sessionCookieName,
			Value: encoded,
			Path:  "/",
			// Default lifetime is 30 days; we're using 7
			MaxAge: 86400 * 7,
			// Only send cookie over HTTPS
			Secure: true,
			// Inaccessible to JS Document.Cookie APIs
			HttpOnly: true,
			// Cookies will only be sent in a first-party context
			// and not be sent along with requests initiated by
			// third party websites.
			SameSite: http.SameSiteStrictMode,
		}

		if cookie.String() == "" {
			slog.Infof("cookie is empty and will be dropped: %v -> %v", cookie, cookie.String())
		}

		slog.Infof("setting cookie %v", cookie.String())
		http.SetCookie(w, cookie)

	} else {
		slog.Infof("cookie encoding failed: %v", err)
	}

	io.WriteString(w, "OK login\n")
}

// GET logout () -> (...)
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	var value map[string]string

	// XXX Should only logout if logged in.
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		value = make(map[string]string)
		if err = cutter.Decode(sessionCookieName, cookie.Value, &value); err == nil {
			slog.Infof("Logging out '%s'", value["uid"])
		} else {
			slog.Infof("Could not decode cookie")
			http.Error(w, "bad request", 400)
			return
		}
	} else {
		// No cookie defined.
		slog.Infof("Could not find cookie for logout")
		http.Error(w, "bad request", 400)
		return
	}

	filling := map[string]string{
		"uid": "",
	}

	if encoded, err := cutter.Encode(sessionCookieName, filling); err == nil {
		cookie := &http.Cookie{
			Name:   sessionCookieName,
			Value:  encoded,
			Path:   "/",
			MaxAge: -1,
			// Only send cookie over HTTPS
			Secure: true,
			// Inaccessible to JS Document.Cookie APIs
			HttpOnly: true,
			// Cookies will only be sent in a first-party context
			// and not be sent along with requests initiated by
			// third party websites.
			SameSite: http.SameSiteStrictMode,
		}
		http.SetCookie(w, cookie)
	} else {
		slog.Infof("failed to encode cookie: %s", err)
		http.Error(w, "bad request", 400)
	}
}

type daUserID struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	PhoneNumber     string `json:"phoneNumber"`
	Name            string `json:"name"`
	Organization    string `json:"organization"`
	SelfProvisioned bool   `json:"selfProvisioned"`
}

// GET /userid
func userIDHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	uid := getRequestUID(r)
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := config.GetUser(uid)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	resp := &daUserID{
		Username:        user.UID,
		Email:           user.Email,
		PhoneNumber:     user.TelephoneNumber,
		Name:            user.DisplayName,
		Organization:    "",
		SelfProvisioned: user.SelfProvisioning,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		panic(err)
	}
}

func getRequestUID(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		// No cookie.
		return ""
	}

	value := make(map[string]string)
	if err = cutter.Decode(sessionCookieName, cookie.Value, &value); err != nil {
		slog.Infof("request contains undecryptable cookie value: %v", err)
		return ""
	}

	// Lookup uid.
	uid := value["uid"]

	// Retrieve user node.
	ui, err := config.GetUser(uid)
	if err != nil {
		slog.Infof("demo login for '%s' denied: %v", uid, err)
		return ""
	}

	// Accounts with empty passwords can't be logged into.
	if ui.Password == "" {
		slog.Infof("demo login for '%s' denied: no password", uid)
		return ""
	}

	return ui.UID
}

// cookieAuthMiddleware implements an HTTP middleware which will forbid
// requests which lack a cookie with uid present.  Future evolutions could add
// the uid and role to the request's context.
func cookieAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := getRequestUID(r)
		if uid == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		slog.Infof("%s [uid '%s']", r.RequestURI, uid)
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}

