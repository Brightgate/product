/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/gorilla/mux"
)

const (
	cookieName = "com.brightgate.appliance"
)

func makeApplianceAuthRouter() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/userid", userIDHandler).Methods("GET")
	router.HandleFunc("/appliance/login", applianceLoginHandler).Methods("POST")
	router.HandleFunc("/logout", logoutHandler).Methods("GET")
	return router
}

// POST login () -> (...)
// POST uid, userPassword[, totppass]
func applianceLoginHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Printf("cannot parse form: %v\n", err)
		http.Error(w, "bad request", 400)
		return
	}

	// Must have user and password.
	uids, present := r.Form["uid"]
	if !present || len(uids) == 0 {
		log.Printf("incomplete form, uid\n")
		http.Error(w, "bad request", 400)
		return
	}

	uid := uids[0]
	if len(uids) > 1 {
		log.Printf("multiple uids in form submission: %v\n", uids)
		http.Error(w, "bad request", 400)
	}

	userPasswords, present := r.Form["userPassword"]
	if !present || len(userPasswords) == 0 {
		log.Printf("incomplete form, userPassword\n")
		http.Error(w, "bad request", 400)
		return
	}

	userPassword := userPasswords[0]
	if len(userPasswords) > 1 {
		log.Printf("multiple userPasswords in form submission: %v\n", userPasswords)
		http.Error(w, "bad request", 400)
	}

	// Retrieve user record
	ui, err := config.GetUser(uid)
	if err != nil {
		log.Printf("demo login for '%s' denied: %v\n", uid, err)
		http.Error(w, "login denied", 401)
		return
	}

	cmp := bcrypt.CompareHashAndPassword([]byte(ui.Password),
		[]byte(userPassword))
	if cmp != nil {
		log.Printf("demo login for '%s' denied: password comparison\n", uid)
		http.Error(w, "login denied", 401)
		return
	}

	// XXX How would 2FA work?  If TOTP defined for this user, send
	// back 2FA required?

	filling := map[string]string{
		"uid": uid,
	}

	if encoded, err := cutter.Encode(cookieName, filling); err == nil {
		cookie := &http.Cookie{
			Name:  cookieName,
			Value: encoded,
			Path:  "/",
			// Default lifetime is 30 days.
		}

		if cookie.String() == "" {
			log.Printf("cookie is empty and will be dropped: %v -> %v\n", cookie, cookie.String())
		}

		log.Printf("setting cookie %v\n", cookie.String())
		http.SetCookie(w, cookie)

	} else {
		log.Printf("cookie encoding failed: %v\n", err)
	}

	io.WriteString(w, "OK login\n")
}

// GET logout () -> (...)
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	var value map[string]string

	// XXX Should only logout if logged in.
	if cookie, err := r.Cookie(cookieName); err == nil {
		value = make(map[string]string)
		if err = cutter.Decode(cookieName, cookie.Value, &value); err == nil {
			log.Printf("Logging out '%s'\n", value["uid"])
		} else {
			log.Printf("Could not decode cookie\n")
			http.Error(w, "bad request", 400)
			return
		}
	} else {
		// No cookie defined.
		log.Printf("Could not find cookie for logout\n")
		http.Error(w, "bad request", 400)
		return
	}

	filling := map[string]string{
		"uid": "",
	}

	if encoded, err := cutter.Encode(cookieName, filling); err == nil {
		cookie := &http.Cookie{
			Name:   cookieName,
			Value:  encoded,
			MaxAge: -1,
		}
		http.SetCookie(w, cookie)
	}
}

// GET /userid
func userIDHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	uid := getRequestUID(r)
	if uid == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}

	b, err := json.Marshal(uid)
	if err != nil {
		log.Printf("failed to json marshal uid: %v\n", err)
		return
	}
	_, _ = w.Write(b)
}

func getRequestUID(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		// No cookie.
		return ""
	}

	value := make(map[string]string)
	if err = cutter.Decode(cookieName, cookie.Value, &value); err != nil {
		log.Printf("request contains undecryptable cookie value: %v\n", err)
		return ""
	}

	// Lookup uid.
	uid := value["uid"]

	// Retrieve user node.
	ui, err := config.GetUser(uid)
	if err != nil {
		log.Printf("demo login for '%s' denied: %v\n", uid, err)
		return ""
	}

	// Accounts with empty passwords can't be logged into.
	if ui.Password == "" {
		log.Printf("demo login for '%s' denied: no password\n", uid)
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
		log.Printf("%s [uid '%s']\n", r.RequestURI, uid)
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}
