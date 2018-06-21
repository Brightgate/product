/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 *
 */

// Package m2mauth implements GRPC middleware to authenticate
// GRPC clients using IoT-style (i.e. Machine-to-Machine) JWT-based public key
// authentication.  The appliance holds a private key.  We hold the
// corresponding public key.  The appliance indicates its identity ("clientid")
// and provides a signed statement; we look up the appliance by clientid, and
// get the associated public key(s).  If one of the keys authenticates the
// client, the RPC can proceed.
//
// This also includes a *very* simple caching mechanism which greatly improves
// performance, assuming the client system holds open a single session.  If
// client systems open many sessions, auth performance will suffer; improving
// the mechanism (perhaps to use an LRU cache) is a future project.
package m2mauth

import (
	"context"

	"bg/cloud_models/appliancedb"

	"github.com/dgrijalva/jwt-go"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/satori/uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"

	"github.com/grpc-ecosystem/go-grpc-middleware/auth"
)

type authCacheEntry struct {
	Token     *jwt.Token
	CloudUUID uuid.UUID
}

var authCache = make(map[string]*authCacheEntry)

type authorizedJWTContextKeyType struct{}

var authorizedJWTContextKey = authorizedJWTContextKeyType{}

func reinitAuthCache() {
	authCache = make(map[string]*authCacheEntry)
}

//
// authFunc implements the guts of the cloud M2M authentication
//
// Here we use two facts provided by the client:
// - The clientid, which is the registry "address" of the device
// - The authorization: bearer <jwt> token, which proves the client's identity
//
func authFunc(ctx context.Context, applianceDB appliancedb.DataStore) (context.Context, error) {
	// Get the bearer token from the authorization: header
	authJWT, err := grpc_auth.AuthFromMD(ctx, "bearer")
	if err != nil {
		return nil, err
	}

	// Get the client Appliance's ID from the clientid header.
	clientID := metautils.ExtractIncoming(ctx).Get("clientid")
	if clientID == "" {
		return nil, status.Errorf(codes.Unauthenticated, "Request unauthenticated; missing clientid")
	}

	// Fast-path: check the token cache for this clientID to see if we've
	// seen this JWT before.  If so, we recheck the validity of the claims.
	// If the incoming JWT doesn't match the cached JWT, punt to the slow
	// path.  The presumption here is that we'll consistently see the same
	// JWT over and over from a particular appliance.  If that turns out
	// not to be true, this will need to be restructured, perhaps by using
	// the JWT as the primary key (in which case cleaning up JWTs
	// periodically will also be needed).
	if cacheEnt, ok := authCache[clientID]; ok {
		if cacheEnt.Token.Raw == authJWT {
			if err = cacheEnt.Token.Claims.Valid(); err != nil {
				grpclog.Warningf("Cached JWT isn't valid: %s", err)
				delete(authCache, clientID)
				return nil, status.Errorf(codes.Unauthenticated, "Invalid JWT Claims (could be expired)")
			}
			newCtx := context.WithValue(ctx, authorizedJWTContextKey, cacheEnt.Token)
			md := metautils.ExtractIncoming(newCtx).Add("clouduuid", cacheEnt.CloudUUID.String())
			newCtx = md.ToIncoming(newCtx)
			return newCtx, nil
		}
	}

	applianceID, err := applianceDB.ApplianceIDByClientID(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid applianceDB Device: %v", err)
	}

	keys, err := applianceDB.KeysByUUID(ctx, applianceID.CloudUUID)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "couldn't get keys for %v: %v", applianceID, err)
	}

	// Walk through the public keys looking for one which validates the JWT
	for n, cred := range keys {
		if cred.Format != "RS256_X509" {
			grpclog.Warningf("Unexpected invalid credential format %s for %s", cred.Format, clientID)
			continue
		}
		pk, err := jwt.ParseRSAPublicKeyFromPEM([]byte(cred.Key))
		if err != nil {
			grpclog.Warningf("Unparseable invalid registry key #%v for %s: %s", n, clientID, err)
			continue
		}
		token, err := jwt.Parse(authJWT, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, status.Errorf(codes.Unauthenticated,
					"Unexpected signing method: %v", token.Header["alg"])
			}
			return pk, nil
		})
		if token != nil && token.Valid {
			authCache[clientID] = &authCacheEntry{
				Token:     token,
				CloudUUID: applianceID.CloudUUID,
			}
			newCtx := context.WithValue(ctx, authorizedJWTContextKey, token)
			md := metautils.ExtractIncoming(newCtx).Add("clouduuid", applianceID.CloudUUID.String())
			newCtx = md.ToIncoming(newCtx)
			grpclog.Infof("Authenticated %s with %s key %d", clientID, applianceID, n)
			return newCtx, nil
		} else if ve, ok := err.(*jwt.ValidationError); ok {
			if ve.Errors&jwt.ValidationErrorSignatureInvalid != 0 {
				// try the next signature
				continue
			}
		}
		// Remaining errors indicate a problem with the JWT itself.
		grpclog.Warningf("Failing authentication attempt: %v", err)
		return nil, status.Errorf(codes.Unauthenticated, "JWT claims invalid")
	}
	return nil, status.Errorf(codes.Unauthenticated, "No keys validate the signature")
}

// StreamServerInterceptor returns an interceptor which performs JWT-based
// client authentication.  Its main job is to bind applianceDB to the auth
// function.
func StreamServerInterceptor(applianceDB appliancedb.DataStore) grpc.StreamServerInterceptor {
	return grpc_auth.StreamServerInterceptor(func(ctx context.Context) (context.Context, error) {
		return authFunc(ctx, applianceDB)
	})
}

// UnaryServerInterceptor returns an interceptor which performs JWT-based
// client authentication.  Its main job is to bind applianceDB to the auth
// function.
func UnaryServerInterceptor(applianceDB appliancedb.DataStore) grpc.UnaryServerInterceptor {
	return grpc_auth.UnaryServerInterceptor(func(ctx context.Context) (context.Context, error) {
		return authFunc(ctx, applianceDB)
	})
}
