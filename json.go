/*
Package jsonware has a middleware setup for doing RESTful reqeuests with net/http.
It makes it easy to unobtrusively serialize and deserialize json in a type safe
manner, and handle errors from the internal handlers including logging.
*/
package jsonware

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
)

type Thing struct {
	Name string `json:"name"`
}

var count = 0

/*
JSONHandler handles json api endpoint restful requests. It can be constructed
by passing a suitable function into the JSON function.

JSONHandler
*/
type JSONHandler struct {
	logger io.Writer
	fn     reflect.Value
	in     reflect.Type
}

// Log sets the JSONHandler's logging io.Writer for writing out cloaked errors.
func (j *JSONHandler) Log(logger io.Writer) *JSONHandler {
	j.logger = logger
	return j
}

/*
JSONErr can be used in a JSONHandler to override the error mechanism in
JSONHandler's ServeHTTP method. If a status is set it will obey it,
otherwise it will assume 200 OK. The error message will be relayed to the
client. If you wish to use a general server error, simply return error from
the handler.

	func handler(w http.ResponseWriter, r *http.Request) {
		return nil, nil // No response from ServeHTTP, use w to create one.
	}

	func handler(w http.ResponseWriter, r *http.Request) {
		return nil, errors.New("hi") // 500 Response with cloaked+logged error.
	}

	func handler(w http.ResponseWriter, r *http.Request) {
		return nil, JSONErr{error: errors.New("hi")} // 200 Response with error output to client
	}

	func handler(w http.ResponseWriter, r *http.Request) {
		return nil, JSONErr{400, errors.New("hi")} // 400 Response with error output to client
	}
*/
type JSONErr struct {
	Status int
	Err    error
}

// Error returns Error() from the internal error.
func (j JSONErr) Error() string {
	return j.Err.Error()
}

// ServeHTTP serves an http response, see JSONHandler documentation for details.
func (j JSONHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Ensure request accepts json
	ah := r.Header.Get("Accept")
	if !strings.Contains(ah, "*/*") && !strings.Contains(ah, "application/json") {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "this endpoint only responds to json-accepting clients")
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Ensure request follows REST principles.
	deserialize := j.fn.Type().NumIn() == 3
	switch {
	case deserialize && !isDataMethod(r.Method):
		fallthrough
	case !deserialize && isDataMethod(r.Method):
		writeJSONError(w, j.logger, JSONErr{
			http.StatusBadRequest,
			fmt.Errorf("invalid http method to this endpoint: %s", r.Method),
		})
		return
	}

	// Set up arguments for handler call.
	in := []reflect.Value{
		reflect.ValueOf(w), reflect.ValueOf(r),
	}
	var deserializeTo reflect.Value
	if deserialize {
		switch j.in.Kind() {
		case reflect.Slice, reflect.Map:
			deserializeTo = reflect.New(j.in)
			in = append(in, deserializeTo.Elem())
		case reflect.Ptr:
			deserializeTo = reflect.New(j.in.Elem())
			in = append(in, deserializeTo)
		}
	}

	// Do json deserialization of body.
	if deserialize {
		dec := json.NewDecoder(r.Body)
		defer r.Body.Close()

		if err := dec.Decode(deserializeTo.Interface()); err != nil {
			writeJSONError(w, j.logger, JSONErr{
				http.StatusBadRequest,
				fmt.Errorf("could not deserialize json request body"),
			})
			return
		}
	}

	out := j.fn.Call(in)

	// Handle error return value
	if !out[1].IsNil() {
		writeJSONError(w, j.logger, out[1].Interface().(error))
		return
	}

	// Serialize the interface{} return value
	if !out[0].IsNil() {
		enc := json.NewEncoder(w)
		if err := enc.Encode(out[0].Interface()); err != nil {
			writeJSONError(w, j.logger, JSONErr{
				http.StatusInternalServerError,
				fmt.Errorf("problem preparing response"),
			})
			return
		}
	}
}

// isDataMethod returns true if method is one of POST PUT or PATCH
func isDataMethod(method string) bool {
	return method == "POST" || method == "PATCH" || method == "PUT"
}

// jsonSanitizer does basic sanitization (except unicode) of json values.
var jsonSanitizer = strings.NewReplacer(
	`"`, `\"`, `\`, `\\`, `/`, `\/`, "\x08", "\\b",
	"\x12", "\\f", "\n", "\\n", "\r", "\\r", "\t", "\\t",
)

// writeJSONError writes an error out to the response.
func writeJSONError(w http.ResponseWriter, logger io.Writer, err error) {
	switch e := err.(type) {
	case JSONErr:
		if e.Status != 0 {
			w.WriteHeader(e.Status)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		fmt.Fprintf(w, `{ "error": "%v" }`, jsonSanitizer.Replace(e.Error()))
	default:
		if logger != nil {
			fmt.Fprintf(logger, "internal error: %v", err)
		}
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{ "error": "an internal server error occurred" }`)
	}
}

/*
JSON changes a function into a JSONHandler.
Acceptable forms of the input function:

	GET (Note: all variant return types also work with POST/PATCH/PUT
	func Fn(w http.ResponseWriter, r *http.Request) (interface{}, error)
	func Fn(w http.ResponseWriter, r *http.Request) (*MyStruct, error)
	func Fn(w http.ResponseWriter, r *http.Request) ([]*MyStruct, error)
	func Fn(w http.ResponseWriter, r *http.Request) (map[string]*MyStruct, error)

	POST/PUT/PATCH
	func Fn(w http.ResponseWriter, r *http.Request, m *MyStruct) (interface{}, error)
	func Fn(w http.ResponseWriter, r *http.Request, m []*MyStruct) (interface{}, error)
	func Fn(w http.ResponseWriter, r *http.Request, m map[string]*MyStruct) (interface{}, error)
*/
func JSON(fn interface{}) *JSONHandler {
	typ := reflect.TypeOf(fn)
	if typ.Kind() != reflect.Func {
		panic("Can only register functions.")
	}

	var p1, p2, p3 reflect.Type

	switch typ.NumIn() {
	case 3:
		p3 = typ.In(2)
		if p3.Kind() != reflect.Ptr && p3.Kind() != reflect.Map && p3.Kind() != reflect.Slice {
			panic("Third argument must be an *object, map, or slice")
		}

		fallthrough
	case 2:
		p1, p2 = typ.In(0), typ.In(1)
		if "http.ResponseWriter" != p1.String() {
			panic("First argument must be an http.ResponseWriter")
		}

		if "*http.Request" != p2.String() {
			panic("Second argument must be a *http.Request")
		}
	default:
		panic("Handler must have 2-3 arguments: ResponseWriter, Request, [Object]")
	}

	if typ.NumOut() != 2 {
		panic("Handler must have two returns: *object or interface{}, and error")
	}

	o1, o2 := typ.Out(0), typ.Out(1)

	if "interface {}" != o1.String() && o1.Kind() != reflect.Ptr && o1.Kind() != reflect.Slice && o1.Kind() != reflect.Map {
		panic("First return must be an empty *object, map, slice or interface{}")
	}

	if "error" != o2.String() {
		panic("Second return must be an error")
	}

	return &JSONHandler{fn: reflect.ValueOf(fn), in: p3}
}
