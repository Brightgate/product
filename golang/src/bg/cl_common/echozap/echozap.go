/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package echozap

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dhduvall/gcloudzap"
	"github.com/labstack/echo"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func ffRemoteIP(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return c.RealIP(), nil
}

func ffID(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	id := req.Header.Get(echo.HeaderXRequestID)
	if id == "" {
		id = res.Header().Get(echo.HeaderXRequestID)
	}
	return id, nil
}

func ffTimeUnix(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return strconv.FormatInt(time.Now().Unix(), 10), nil
}

func ffTimeUnixNano(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return strconv.FormatInt(time.Now().UnixNano(), 10), nil
}

func ffTimeRFC3339(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}

func ffTimeRFC3339Nano(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return time.Now().Format(time.RFC3339Nano), nil
}

// func ffTimeCustom()?

func ffHost(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return req.Host, nil
}

func ffURI(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return req.RequestURI, nil
}

func ffMethod(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return req.Method, nil
}

func ffPath(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	p := req.URL.Path
	if p == "" {
		p = "/"
	}
	return p, nil
}

func ffProtocol(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return req.Proto, nil
}

func ffReferer(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return req.Referer(), nil
}

func ffUserAgent(c echo.Context, req *http.Request, res *echo.Response) (string, error) {
	return req.UserAgent(), nil
}

// StringFieldFunc is the signature for functions used by Logger to convert a
// field name into a string.
type StringFieldFunc func(c echo.Context, req *http.Request, res *echo.Response) (string, error)

var coreStringFields = map[string]StringFieldFunc{
	"remote_ip":         ffRemoteIP,
	"id":                ffID,
	"time_unix":         ffTimeUnix,
	"time_unix_nano":    ffTimeUnixNano,
	"time_rfc3339":      ffTimeRFC3339,
	"time_rfc3339_nano": ffTimeRFC3339Nano,
	"host":              ffHost,
	"uri":               ffURI,
	"method":            ffMethod,
	"path":              ffPath,
	"protocol":          ffProtocol,
	"referer":           ffReferer,
	"user_agent":        ffUserAgent,
}

func ffStatus(c echo.Context, req *http.Request, res *echo.Response) (int, error) {
	return res.Status, nil
}

// IntFieldFunc is the signature for functions used by Logger to convert a field
// name into an int.
type IntFieldFunc func(c echo.Context, req *http.Request, res *echo.Response) (int, error)

var coreIntFields = map[string]IntFieldFunc{
	"status": ffStatus,
}

func ffBytesIn(c echo.Context, req *http.Request, res *echo.Response) (int64, error) {
	cl := req.Header.Get(echo.HeaderContentLength)
	if cl == "" {
		return 0, nil
	}
	return strconv.ParseInt(cl, 10, 64)
}

func ffBytesOut(c echo.Context, req *http.Request, res *echo.Response) (int64, error) {
	return res.Size, nil
}

// Int64FieldFunc is the signature for functions used by Logger to convert a
// field name into an int64.
type Int64FieldFunc func(c echo.Context, req *http.Request, res *echo.Response) (int64, error)

var coreInt64Fields = map[string]Int64FieldFunc{
	"bytes_in":  ffBytesIn,
	"bytes_out": ffBytesOut,
}

func cookieFunc(name string) StringFieldFunc {
	return func(c echo.Context, _ *http.Request, _ *echo.Response) (string, error) {
		// The only error we'd get is if the cookie didn't exist, and
		// there's no point in logging that error.
		var val string
		if cookie, err := c.Cookie(name); err == nil {
			val = cookie.Value
		}
		return val, nil
	}
}

// CookieField returns a Field definition to extract value of the named cookie
// from the echo.Context.  If an argument is provided, it will name the field,
// which otherwise defaults to the name of the cookie.
func CookieField(name string, args ...string) Field {
	fieldName := name
	if len(args) > 0 {
		fieldName = args[0]
	}
	return Field{
		Name: fieldName,
		Data: cookieFunc(name),
	}
}

func headerFunc(name string) StringFieldFunc {
	return func(_ echo.Context, req *http.Request, _ *echo.Response) (string, error) {
		return req.Header.Get(name), nil
	}
}

// HeaderField returns a Field definition to extract the value of the named
// header from the request.  If an argument is provided, it will name the field,
// which otherwise defaults to the name of the header.
func HeaderField(name string, args ...string) Field {
	fieldName := name
	if len(args) > 0 {
		fieldName = args[0]
	}
	return Field{
		Name: fieldName,
		Data: headerFunc(name),
	}
}

func formFunc(name string) StringFieldFunc {
	return func(c echo.Context, _ *http.Request, _ *echo.Response) (string, error) {
		return c.FormValue(name), nil
	}
}

// FormField returns a Field definition to extract the value of the form field
// value from the echo.Context.  If an argument is provided, it will name the
// field, which otherwise defaults to the name of the form field.
func FormField(name string, args ...string) Field {
	fieldName := name
	if len(args) > 0 {
		fieldName = args[0]
	}
	return Field{
		Name: fieldName,
		Data: formFunc(name),
	}
}

func queryFunc(name string) StringFieldFunc {
	return func(c echo.Context, _ *http.Request, _ *echo.Response) (string, error) {
		return c.QueryParam(name), nil
	}
}

// QueryField returns a Field definition to extract the value of the query
// parameter from the echo.Context.  If an argument is provided, it will name
// the field, which otherwise defaults to the name of the parameter.
func QueryField(name string, args ...string) Field {
	fieldName := name
	if len(args) > 0 {
		fieldName = args[0]
	}
	return Field{
		Name: fieldName,
		Data: queryFunc(name),
	}
}

type specialFieldFunc func()

// CoreField returns a Field definition of one of the core fields (see
// echo/middleware.LoggerConfig.Format).  If an argument is provided, it will
// name the field, which otherwise defaults to the name of the core field.
func CoreField(name string, args ...string) Field {
	fieldName := name
	if len(args) > 0 {
		fieldName = args[0]
	}

	var ff interface{}
	var ok bool
	ff, ok = coreStringFields[name]
	if !ok {
		ff, ok = coreIntFields[name]
	}
	if !ok {
		ff, ok = coreInt64Fields[name]
	}

	// We need to make sure all core field names end up with a non-nil data
	// source.
	if !ok {
		switch name {
		case "latency", "latency_human", "error":
			ff = func() {}
			ok = true
		}
	}

	// We don't want to fail on nil data sources as early as possible,
	// rather than waiting for a request.  Ideally, this would be a
	// compilation error, but probably not in Go.
	if !ok {
		panic(fmt.Sprintf("unknown core field '%s'", name))
	}

	return Field{
		Name: fieldName,
		Data: ff,
	}
}

// DefaultFields represents the default fields that echo logs.
var DefaultFields = []Field{
	CoreField("time_rfc3339_nano", "time"),
	CoreField("id"),
	CoreField("remote_ip"),
	CoreField("host"),
	CoreField("method"),
	CoreField("uri"),
	CoreField("user_agent"),
	CoreField("status"),
	CoreField("error"),
	CoreField("latency"),
	CoreField("latency_human"),
	CoreField("bytes_in"),
	CoreField("bytes_out"),
}

// Field is a tuple mapping a field name to its data, kept as an element for a
// list in order to preserve ordering.
type Field struct {
	// The name of the field, as to be passed to zap.
	Name string
	// The value of the field, as to be passed to zap.  May be a string
	// constant, or any of the *FieldFunc types defined in this module.
	Data interface{}
}

// This mirrors the stackTracer interface in pkg/errors.
type stackTracer interface {
	StackTrace() errors.StackTrace
}

// Logger is an echo middleware that logs requests using a zap logger.  It is
// based on the LoggerWithConfig middleware bundled with echo.
func Logger(log *zap.Logger, requestedFields []Field) echo.MiddlewareFunc {
	if len(requestedFields) == 0 {
		requestedFields = DefaultFields
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			mwerr := next(c)
			if mwerr != nil {
				c.Error(mwerr)
			}
			stop := time.Now()

			req := c.Request()
			res := c.Response()

			fields := make([]zap.Field, 0, len(requestedFields))

			for _, field := range requestedFields {
				var err error
				switch v := field.Data.(type) {
				// Nil needs to be first, or any of the other
				// nilable types would try to handle it.
				case nil:
					panic(fmt.Sprintf("nil data source for field name '%s'", field.Name))
				case string:
					fields = append(fields, zap.String(field.Name, v))
				case StringFieldFunc:
					var val string
					if val, err = v(c, req, res); err == nil {
						fields = append(fields, zap.String(field.Name, val))
					}
				case IntFieldFunc:
					var val int
					if val, err = v(c, req, res); err == nil {
						fields = append(fields, zap.Int(field.Name, val))
					}
				case Int64FieldFunc:
					var val int64
					if val, err = v(c, req, res); err == nil {
						fields = append(fields, zap.Int64(field.Name, val))
					}

				// Some special cases that don't fit the mold of
				// the rest
				default:
					switch field.Name {
					case "latency":
						fields = append(fields, zap.Duration(field.Name, stop.Sub(start)))
					case "latency_human":
						fields = append(fields, zap.String(field.Name, stop.Sub(start).String()))
					case "error":
						fields = append(fields, zap.Error(mwerr))
					default:
						log.Error("Unknown field name or type",
							zap.String("field_name", field.Name),
							zap.String("field_type", fmt.Sprintf("%T", v)))
					}
				}

				if err != nil {
					log.Error("Error retrieving or converting value for field",
						zap.String("field_name", field.Name),
						zap.Error(err))
				}
			}

			// This will put the location in the "internal" error of
			// an echo.HTTPError into the log.  This may still be in
			// echo code if, for example, we (or the framework) just
			// return echo.ErrNotFound.
			//
			// Note that redirects aren't errors; neither they nor
			// successes end up with "correct" caller sites.
			if echoErr, ok := mwerr.(*echo.HTTPError); ok {
				if sErr, ok := echoErr.Internal.(stackTracer); ok {
					f := zap.Field{
						Key:       gcloudzap.CallerKey,
						Type:      gcloudzap.CallerType,
						Interface: sErr,
					}
					fields = append(fields, f)
				}
			}

			// Is there some way to populate the stackdriver
			// httpRequest structure?  Should we?
			n := res.Status
			statusText := http.StatusText(n)
			if statusText != "" {
				statusText = " " + statusText
			}
			switch {
			case n >= 500:
				msg := fmt.Sprintf("Server error (%d%s): %s", n, statusText, req.RequestURI)
				log.Error(msg, fields...)
			case n >= 400:
				msg := fmt.Sprintf("Client error (%d%s): %s", n, statusText, req.RequestURI)
				log.Warn(msg, fields...)
			case n >= 300:
				msg := fmt.Sprintf("Redirection (%d%s): %s", n, statusText, req.RequestURI)
				log.Info(msg, fields...)
			default:
				msg := fmt.Sprintf("Success (%d%s): %s", n, statusText, req.RequestURI)
				log.Info(msg, fields...)
			}

			return nil
		}
	}
}
