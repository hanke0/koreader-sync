package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	addr := "127.0.0.0:9200"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	db, err := Open("koreader-sync.sqlite")
	if err != nil {
		log.Fatal(err)
	}
	globalDB = db
	log.Println("listen and server at", addr)
	if err := http.ListenAndServe(addr, serveMux); err != nil {
		log.Fatal(err)
	}
}

type Database struct {
	*sql.DB
}

func Open(path string) (*Database, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	d := &Database{db}
	if err := d.initTable(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

type User struct {
	ID       int
	Name     string
	Password string
	Salt     string
}

type Progress struct {
	User       int    `json:"user,omitempty"`
	Document   string `json:"document,omitempty"`
	Percentage int    `json:"percentage,omitempty"`
	Progress   string `json:"progress,omitempty"`
	Device     string `json:"device,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
}

type Error struct {
	HTTPCode int    `json:"-"`
	Code     int    `json:"code"`
	Message  string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("http:%d, code:%d, message:%s", e.HTTPCode, e.Code, e.Message)
}

var (
	ErrNoUser = &Error{
		HTTPCode: http.StatusUnauthorized,
		Code:     http.StatusUnauthorized,
		Message:  "No User",
	}
	ErrIncorrectPassword = &Error{
		HTTPCode: http.StatusUnauthorized,
		Code:     http.StatusUnauthorized,
		Message:  "Unauthorized",
	}
	ErrUserExists = &Error{
		HTTPCode: http.StatusPaymentRequired,
		Code:     2002,
		Message:  "User Exists",
	}
)

func (d *Database) initTable() error {
	_, err := d.Exec(`
CREATE TABLE IF NOT EXISTS users (
	name text,
	password text,
	salt text,
	primary key (name)
)
	`)
	if err != nil {
		return err
	}
	_, err = d.Exec(`
	CREATE TABLE IF NOT EXISTS progress (
		user int,
		document text,
		percentage int,
		progress text,
		device text,
		device_id text,
		timestamp int,
		primary key (user, document)
	)
		`)
	if err != nil {
		return err
	}
	return nil
}

func (d *Database) GetUser(ctx context.Context, name string) (*User, error) {
	rows, err := d.DB.QueryContext(ctx, "SELECT rowid, name, password, salt FROM users WHERE name = ?", name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNoUser
	}
	var u User
	if err := rows.Scan(&u.ID, &u.Name, &u.Password, &u.Salt); err != nil {
		return nil, err
	}
	return &u, nil
}

func (d *Database) Auth(ctx context.Context, name, password string) (int, error) {
	u, err := d.GetUser(ctx, name)
	if err != nil {
		return 0, err
	}
	h := hmac.New(sha256.New, []byte(u.Salt))
	b := h.Sum([]byte(password))
	p := base64.StdEncoding.EncodeToString(b)
	if p != u.Password {
		return 0, ErrNoUser
	}

	if u.Password != password {
		return 0, ErrIncorrectPassword
	}
	return u.ID, nil
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func (d *Database) CreateUser(ctx context.Context, name, password string) error {
	_, err := d.GetUser(ctx, name)
	if err != nil && err != ErrNoUser {
		return err
	}
	if err == nil {
		return ErrUserExists
	}
	salt := RandStringRunes(8)
	h := hmac.New(sha256.New, []byte(salt))
	b := h.Sum([]byte(password))
	p := base64.StdEncoding.EncodeToString(b)
	_, err = d.DB.ExecContext(ctx, "INSERT INTO users (name, password, salt) VALUES (?, ?, ?)", name, p, salt)
	return err
}

func (d *Database) GetProgress(ctx context.Context, user int, document string) (*Progress, error) {
	rows, err := d.DB.QueryContext(ctx, `
	SELECT
	user, document, percentage, progress, device, device_id, timestamp
	FROM progress WHERE user = ? AND document = ?`, user, document)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return &Progress{}, nil
	}
	var p Progress
	if err := rows.Scan(&p.User, &p.Document, &p.Percentage, &p.Progress, &p.Device, &p.DeviceID, &p.Timestamp); err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *Database) UpdateProgress(ctx context.Context, p *Progress) error {
	_, err := d.DB.ExecContext(ctx, `
	REPLACE INTO progress
	(user, document, percentage, progress, device, device_id, timestamp)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	WHERE user = ? AND document = ?`,
		p.User, p.Document, p.Percentage, p.Progress, p.Device, p.DeviceID, p.Timestamp,
		p.User, p.Document)
	if err != nil {
		return err
	}
	return nil
}

var globalDB *Database

func writeJSONError(w http.ResponseWriter, r *http.Request, err error) {
	e, ok := err.(*Error)
	if !ok {
		e = &Error{
			Code:     http.StatusInternalServerError,
			Message:  "Internal Server Error",
			HTTPCode: http.StatusInternalServerError,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.HTTPCode)
	json.NewEncoder(w).Encode(e)
}

func auth(w http.ResponseWriter, r *http.Request) int {
	user := r.Header.Get("X-Auth-User")
	key := r.Header.Get("X-Auth-Key")
	id, err := globalDB.Auth(r.Context(), user, key)
	if err != nil {
		writeJSONError(w, r, err)
		return 0
	}
	return id
}

func decodeBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	err := json.NewDecoder(r.Body).Decode(v)
	if err != nil {
		writeJSONError(w, r, &Error{
			HTTPCode: http.StatusBadRequest,
			Code:     2003,
			Message:  "Bad request",
		})
		return false
	}
	return true
}

var serveMux = http.NewServeMux()

type UserForm struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func routeUsersCreate(w http.ResponseWriter, r *http.Request) {
	var u UserForm
	if !decodeBody(w, r, &u) {
		return
	}
	err := globalDB.CreateUser(r.Context(), u.Username, u.Password)
	if err != nil {
		writeJSONError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"username": u.Username,
	})
}

func routeUsersAuth(w http.ResponseWriter, r *http.Request) {
	if auth(w, r) == 0 {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"authorized": "OK",
	})
}

func routeSyncProgress(w http.ResponseWriter, r *http.Request) {
	uid := auth(w, r)
	if uid == 0 {
		return
	}
	var p Progress
	if !decodeBody(w, r, &p) {
		return
	}
	p.User = uid
	p.Timestamp = time.Now().Unix()
	err := globalDB.UpdateProgress(r.Context(), &p)
	if err != nil {
		writeJSONError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(map[string]interface{}{
		"document":  p.Document,
		"timestamp": p.Timestamp,
	})
}

func routeGetProgress(w http.ResponseWriter, r *http.Request) {
	uid := auth(w, r)
	if uid == 0 {
		return
	}
	p, err := globalDB.GetProgress(r.Context(), uid, r.PathValue("document"))
	if err != nil {
		writeJSONError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(p)
}

func routeHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(map[string]string{
		"state": "OK",
	})
}

type responseWrap struct {
	http.ResponseWriter
	StatusCode int
	n          int
}

func (w *responseWrap) WriteHeader(code int) {
	w.StatusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWrap) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.n += n
	return n, err
}

func handle(pattern string, h http.HandlerFunc) {
	serveMux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWrap{w, http.StatusOK, 0}
		h(ww, r)
		log.Printf("%s %s %d %d %v", r.Method, r.URL.Path, ww.StatusCode, ww.n, time.Since(start))
	})
}

func init() {
	handle("POST /users/create", routeUsersCreate)
	handle("GET /users/auth", routeUsersAuth)
	handle("PUT /syncs/progress", routeSyncProgress)
	handle("GET /syncs/progress/{document}", routeGetProgress)
	handle("GET /healthcheck", routeHealthCheck)
}
