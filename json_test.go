package jsonware

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testType struct {
	Name string `json:"name"`
}

type testController struct {
	toReturn string
}

// param *
func testHandler1(w http.ResponseWriter, r *http.Request, t *testType) (interface{}, error) {
	return &testType{"hi"}, nil
}

// method instead of function
func (s *testController) testHandler2(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	return &testType{s.toReturn}, nil
}

// return *
func testHandler3(w http.ResponseWriter, r *http.Request, t *testType) (*testType, error) {
	return &testType{"hi"}, nil
}

// param slice
func testHandler4(w http.ResponseWriter, r *http.Request, t []*testType) (interface{}, error) {
	return &testType{"hi"}, nil
}

// param map
func testHandler5(w http.ResponseWriter, r *http.Request, t map[string]*testType) (interface{}, error) {
	return &testType{"hi"}, nil
}

// return slice
func testHandler6(w http.ResponseWriter, r *http.Request) ([]*testType, error) {
	return []*testType{{"hi"}}, nil
}

// return map
func testHandler7(w http.ResponseWriter, r *http.Request) (map[string]*testType, error) {
	return map[string]*testType{"hi": &testType{"hi"}}, nil
}

// Params Arity
func badHandler1() (interface{}, error) { return nil, nil }

// Return arity
func badHandler2(w http.ResponseWriter, r *http.Request) error { return nil }

// 1st arg
func badHandler3(w int, r *http.Request) (interface{}, error) { return nil, nil }

// 2nd arg
func badHandler4(w http.ResponseWriter, r int) (interface{}, error) { return nil, nil }

// 3rd arg
func badHandler5(w http.ResponseWriter, r *http.Request, t testType) (interface{}, error) {
	return nil, nil
}

// 1st return
func badHandler6(w http.ResponseWriter, r *http.Request) (testType, error) { return testType{}, nil }

// 2nd return
func badHandler7(w http.ResponseWriter, r *http.Request) (interface{}, int) { return nil, 5 }

// 500 error
func errHandler1(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	return nil, errors.New("error occurred")
}

// 200 JSONErr
func errHandler2(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	return nil, JSONErr{Err: errors.New("validation error")}
}

// handled json error
func errHandler3(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	return nil, JSONErr{Status: http.StatusBadRequest, Err: errors.New("ugly request")}
}

// handled json error with serialized reason
func errHandler4(w http.ResponseWriter, r *http.Request) (interface{}, error) {
	return nil, JSONErr{
		Status: http.StatusBadRequest,
		Err:    errors.New("ugly request"),
		Reason: map[string]string{"problem": "occurred"},
	}
}

func TestJSON_Serializing(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		handler interface{}
		method  string
		status  int
		reqbody string
		resbody string
	}{
		{testHandler1, "POST", 400, `{ "name"asonetd!!: "hi" }`, "could not deserialize"},
		{testHandler1, "POST", 200, `{ "name": "hi" }`, "hi"},
		{(&testController{"hello"}).testHandler2, "GET", 200, "", `{"name":"hello"}`},
		{testHandler3, "POST", 200, `{ "name": "hi" }`, `hi`},
		{testHandler4, "POST", 200, `[{ "name": "hi" }]`, `hi`},
		{testHandler5, "POST", 200, `{ "friend": { "name": "hi" }}`, `hi`},
		{testHandler6, "GET", 200, ``, `[{"name":"hi"}]`},
		{testHandler7, "GET", 200, ``, `{"hi":{"name":"hi"}}`},
	}

	for i, test := range tests {
		res := httptest.NewRecorder()
		req, _ := http.NewRequest(test.method, "/", bytes.NewBufferString(test.reqbody))
		req.Header = http.Header{"Accept": []string{"*/*"}}

		j := JSON(test.handler)
		j.ServeHTTP(res, req)

		if res.Code != test.status {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected status: %d, got: %d", test.status, res.Code)
		}

		if b := res.Body.String(); !strings.Contains(b, test.resbody) {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected body: %s, got: %s", test.resbody, b)
		}
	}
}

func TestJSON_RequestFilter(t *testing.T) {
	t.Parallel()

	normHeader := http.Header{
		"Accept": []string{"*/*"},
	}
	badAccept := http.Header{
		"Accept": []string{"application/xml"},
	}

	var tests = []struct {
		handler interface{}
		method  string
		status  int
		headers http.Header
		resbody string
	}{
		{testHandler1, "GET", 400, badAccept, "json-accepting"},
		{testHandler1, "GET", 400, normHeader, "invalid http method"},
		{testHandler1, "POST", 200, normHeader, "hi"},
		{testHandler1, "PUT", 200, normHeader, "hi"},
		{testHandler1, "PATCH", 200, normHeader, "hi"},
		{(&testController{"hello"}).testHandler2, "GET", 200, normHeader, "hello"},
		{(&testController{"hello"}).testHandler2, "POST", 400, normHeader, "invalid http method"},
		{(&testController{"hello"}).testHandler2, "PUT", 400, normHeader, "invalid http method"},
		{(&testController{"hello"}).testHandler2, "PATCH", 400, normHeader, "invalid http method"},
	}

	for i, test := range tests {
		res := httptest.NewRecorder()
		req, _ := http.NewRequest(test.method, "/", bytes.NewBufferString(`{ "name": "hi" }`))
		req.Header = test.headers

		j := JSON(test.handler)
		j.ServeHTTP(res, req)

		if res.Code != test.status {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected status: %d, got: %d", test.status, res.Code)
		}

		if b := res.Body.String(); !strings.Contains(b, test.resbody) {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected body: %s, got: %s", test.resbody, b)
		}
	}
}

func TestJSON_Errors(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		handler interface{}
		status  int
		resbody string
		log     string
	}{
		{errHandler1, 500, "an internal server error", "internal error: error occurred"},
		{errHandler2, 200, "validation error", ""},
		{errHandler3, 400, "ugly request", ""},
		{errHandler4, 400, `{"error":"ugly request","reason":{"problem":"occurred"}`, ""},
	}

	log := &bytes.Buffer{}

	for i, test := range tests {
		res := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/", nil)
		req.Header = http.Header{"Accept": []string{"*/*"}}

		log.Reset()
		j := JSON(test.handler).Log(log)
		j.ServeHTTP(res, req)

		if res.Code != test.status {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected status: %d, got: %d", test.status, res.Code)
		}

		if b := res.Body.String(); !strings.Contains(b, test.resbody) {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected body: %s, got: %s", test.resbody, b)
		}

		if l := log.String(); !strings.Contains(l, test.log) {
			t.Errorf("Test: %d", i)
			t.Errorf("Expected error: %s, got: %s", test.log, l)
		}
	}
}

func TestJSON_Panics(t *testing.T) {
	t.Parallel()

	var tests = []struct {
		handler     interface{}
		shouldPanic bool
	}{
		{testHandler1, false},
		{(&testController{"hello"}).testHandler2, false},
		{testHandler3, false},
		{testHandler4, false},
		{testHandler5, false},
		{testHandler6, false},
		{testHandler7, false},
		{badHandler1, true},
		{badHandler2, true},
		{badHandler3, true},
		{badHandler4, true},
		{badHandler5, true},
		{badHandler6, true},
		{badHandler7, true},
		{5, true},
	}

	for _, test := range tests {
		if didPanic, msg := testPanic(test.handler); didPanic != test.shouldPanic {
			t.Errorf("Test: %T", test.handler)
			t.Errorf("Expected didPanic: %v, but got: %v", test.shouldPanic, didPanic)
			if didPanic {
				t.Errorf("Panic msg: %s", msg)
			}
		}
	}
}

func testPanic(handler interface{}) (didPanic bool, msg string) {
	defer func() {
		err := recover()
		didPanic = nil != err
		if didPanic {
			msg = err.(string)
		}
	}()

	JSON(handler)
	return
}
