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
// It also includes an LFU caching mechanism using the tokens as keys.  The
// cache also expires tokens as they expire.
package m2mauth

import (
	"context"
	"time"

	"bg/base_def"
	"bg/cloud_models/appliancedb"

	"github.com/bluele/gcache"
	"github.com/dgrijalva/jwt-go"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	"github.com/satori/uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/status"

	"github.com/grpc-ecosystem/go-grpc-middleware/auth"
)

// Middleware is a container for data associated with an instance of
// the m2mauth gRPC middleware.  It maintains a reference to the
// applianceDB as well as the authentication cache.
type Middleware struct {
	applianceDB appliancedb.DataStore
	authCache   gcache.Cache
}

type authCacheEntry struct {
	ClientID  string
	Token     *jwt.Token
	CloudUUID uuid.UUID
}

type authorizedJWTContextKeyType struct{}

var authorizedJWTContextKey = authorizedJWTContextKeyType{}

const (
	jwtExpDuration time.Duration = base_def.BEARER_JWT_EXPIRY_SECS * time.Second
	jwtExpLeeway   time.Duration = jwtExpDuration / 10
)

func (m *Middleware) cacheGet(authJWT string) (*authCacheEntry, error) {
	ce, err := m.authCache.Get(authJWT)
	if err != nil {
		return nil, err
	}
	cacheEnt := ce.(*authCacheEntry)
	return cacheEnt, nil
}

func (m *Middleware) cacheSet(cacheEnt *authCacheEntry) {
	claims := cacheEnt.Token.Claims.(*jwt.StandardClaims)
	if claims.ExpiresAt == 0 {
		panic("exp claim required")
	}

	expTime := time.Unix(claims.ExpiresAt, 0)
	duration := time.Until(expTime)
	err := m.authCache.SetWithExpire(cacheEnt.Token.Raw, cacheEnt, duration)
	// This gcache routine cannot fail unless you use its "serialization"
	// feature, which we don't.
	if err != nil {
		panic("unexpected failure to SetWithExpire")
	}
}

//
// authFunc implements the guts of the cloud M2M authentication
//
// Here we use two facts provided by the client:
// - The clientid, which is the registry "address" of the device
// - The authorization: bearer <jwt> token, which proves the client's identity
//
func (m *Middleware) authFunc(ctx context.Context) (context.Context, error) {
	// Get the bearer token from the authorization: header
	authJWT, err := grpc_auth.AuthFromMD(ctx, "bearer")
	if err != nil {
		return nil, err
	}
	if authJWT == "" {
		return nil, status.Errorf(codes.Unauthenticated, "Request unauthenticated; empty bearer")
	}

	// Get the client Appliance's ID from the clientid header.
	clientID := metautils.ExtractIncoming(ctx).Get("clientid")
	if clientID == "" {
		return nil, status.Errorf(codes.Unauthenticated, "Request unauthenticated; missing clientid")
	}

	// Fast-path: check the token cache for this JWT, to see if we've seen
	// it before.  If so, recheck the validity of the clientID and the
	// claims.  If the clientID doesn't match, something odd has happened.
	// The slow path below should take care of things.
	cacheEnt, err := m.cacheGet(authJWT)
	if err == nil {
		if clientID == cacheEnt.ClientID {
			if err = cacheEnt.Token.Claims.Valid(); err != nil {
				grpclog.Warningf("Cached JWT isn't valid: %s", err)
				m.authCache.Remove(authJWT)
				return nil, status.Errorf(codes.Unauthenticated, "Invalid JWT Claims (could be expired)")
			}
			newCtx := context.WithValue(ctx, authorizedJWTContextKey, cacheEnt.Token)
			md := metautils.ExtractIncoming(newCtx).Add("clouduuid", cacheEnt.CloudUUID.String())
			newCtx = md.ToIncoming(newCtx)
			return newCtx, nil
		}
		grpclog.Warningf("JWT found with unexpected clientID! %s != %s",
			clientID, cacheEnt.ClientID)
	}

	applianceID, err := m.applianceDB.ApplianceIDByClientID(ctx, clientID)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid applianceDB Device: %v", err)
	}

	keys, err := m.applianceDB.KeysByUUID(ctx, applianceID.CloudUUID)
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
		token, err := jwt.ParseWithClaims(authJWT, &jwt.StandardClaims{}, func(token *jwt.Token) (interface{}, error) {
			now := time.Now()
			claims := token.Claims.(*jwt.StandardClaims)
			// We require the presentation of the exp claim
			if !claims.VerifyExpiresAt(now.Unix(), true) {
				return nil, status.Errorf(codes.Unauthenticated,
					"missing or expired exp claim: now=%v claims=%v",
					now.Unix(), claims)
			}

			// Expiration of claim is max one hour (with a small leeway period)
			claimExpUnix := time.Unix(claims.ExpiresAt, 0)
			if claimExpUnix.Sub(now) > jwtExpDuration+jwtExpLeeway {
				return nil, status.Errorf(codes.Unauthenticated,
					"exp claim %v > %v ahead", claimExpUnix,
					jwtExpDuration+jwtExpLeeway)
			}

			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, status.Errorf(codes.Unauthenticated,
					"Unexpected signing method: %v", token.Header["alg"])
			}
			return pk, nil
		})
		if token != nil && token.Valid {
			m.cacheSet(&authCacheEntry{
				ClientID:  clientID,
				Token:     token,
				CloudUUID: applianceID.CloudUUID,
			})
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

const cacheSize = 4096

// New creates a new instance of Middleware, with an associated auth cache
func New(applianceDB appliancedb.DataStore) *Middleware {
	authCache := gcache.New(cacheSize).LFU().Build()
	return &Middleware{
		applianceDB,
		authCache,
	}
}

// StreamServerInterceptor returns an interceptor which performs JWT-based
// client authentication.  Its main job is to bind applianceDB to the auth
// function.
func (m *Middleware) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return grpc_auth.StreamServerInterceptor(func(ctx context.Context) (context.Context, error) {
		return m.authFunc(ctx)
	})
}

// UnaryServerInterceptor returns an interceptor which performs JWT-based
// client authentication.  Its main job is to bind applianceDB to the auth
// function.
func (m *Middleware) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return grpc_auth.UnaryServerInterceptor(func(ctx context.Context) (context.Context, error) {
		return m.authFunc(ctx)
	})
}
