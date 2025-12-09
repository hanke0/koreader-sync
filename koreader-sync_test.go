package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)

func TestHTTP(t *testing.T) {
	GetNow = func() time.Time { return testNow }
	file := filepath.Join(t.TempDir(), "sqlite.db")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{t.Name(), "-db", file, "-addr", addr.String()}
	go main()
	time.Sleep(time.Second) // wait for server start
	testHTTPUser(t, addr.String())
	testProgress(t, addr.String())
}

func requestAddr(method, addr, path, body, user, key string) (*http.Response, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("http://%s%s", addr, path), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{}
	if user != "" {
		req.Header.Set("X-Auth-User", user)
	}
	if key != "" {
		req.Header.Set("X-Auth-Key", key)
	}
	return http.DefaultClient.Do(req)
}

func simpleTestPost(t *testing.T, addr, path, body, user, key string, expectBody map[string]any) {
	t.Helper()
	rsp, err := requestAddr("POST", addr, path, body, user, key)
	if err != nil {
		t.Fatal(err)
	}
	assertRsp(t, rsp, expectBody)
}

func simpleTestPut(t *testing.T, addr, path, body, user, key string, expectBody map[string]any) {
	t.Helper()
	rsp, err := requestAddr("PUT", addr, path, body, user, key)
	if err != nil {
		t.Fatal(err)
	}
	assertRsp(t, rsp, expectBody)
}

func simpleTestGet(t *testing.T, addr, path, user, key string, expectBody map[string]any) {
	t.Helper()
	rsp, err := requestAddr("GET", addr, path, "", user, key)
	if err != nil {
		t.Fatal(err)
	}
	assertRsp(t, rsp, expectBody)
}

func assertRsp(t *testing.T, rsp *http.Response, expectBody map[string]any) {
	t.Helper()
	switch rsp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	default:
		t.Fatalf("unexpected status code: %d, rsp=%+v", rsp.StatusCode, rsp)
	}
	b, err := io.ReadAll(rsp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var rspBody map[string]any
	if err := json.Unmarshal(b, &rspBody); err != nil {
		t.Fatal(err, string(b))
	}
	if !reflect.DeepEqual(expectBody, rspBody) {
		t.Fatalf("expect '%+v', got '%+v', rsp-body=%s", expectBody, rspBody, string(b))
	}
}

func testHTTPUser(t *testing.T, addr string) {
	body := `{"username":"test","password":"test"}`
	simpleTestPost(t, addr, "/users/create", body, "", "", map[string]any{"username": "test"})

	simpleTestGet(t, addr, "/users/auth", "test", "test", map[string]any{"authorized": "OK"})
}

func testProgress(t *testing.T, addr string) {
	body := `{"document":"test","percentage":0.1,"progress":"test","device":"123","device_id":"123","timestamp":111}`
	expect := map[string]any{"document": "test", "timestamp": float64(testNow.Unix())}
	simpleTestPut(t, addr, "/syncs/progress", body, "test", "test", expect)
	simpleTestPut(t, addr, "/syncs/progress", body, "test", "test", expect)

	expect = map[string]any{
		"percentage": 0.1,
		"progress":   "test",
		"device":     "123",
		"device_id":  "123",
		"document":   "test",
		"timestamp":  float64(testNow.Unix()),
	}
	simpleTestGet(t, addr, "/syncs/progress/test", "test", "test", expect)

	rows, err := globalDB.Query("select * from progress_history")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	for rows.Next() {
		n++
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	if n != 2 {
		t.Fatalf("expect 2, got %d", n)
	}
	rows.Close()
}
