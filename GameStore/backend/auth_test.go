package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type memoryUserStore struct {
	mu    sync.Mutex
	next  int64
	users map[int64]User
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{next: 1, users: make(map[int64]User)}
}

func (store *memoryUserStore) CreateUser(_ context.Context, username, email, passwordHash string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, user := range store.users {
		if user.Username == username {
			return User{}, ErrDuplicateUsername
		}
		if user.Email == email {
			return User{}, ErrDuplicateEmail
		}
	}
	now := time.Now()
	user := User{
		ID:           store.next,
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         "member",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	store.next++
	store.users[user.ID] = user
	return user, nil
}

func (store *memoryUserStore) FindByLogin(_ context.Context, login string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, user := range store.users {
		if user.Username == login || user.Email == login {
			return user, nil
		}
	}
	return User{}, ErrUserNotFound
}

func (store *memoryUserStore) FindByID(_ context.Context, id int64) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	user, ok := store.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (store *memoryUserStore) remove(id int64) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.users, id)
}

func newAuthTestAPI() (api, *memoryUserStore) {
	users := newMemoryUserStore()
	return api{
		users:     users,
		jwtSecret: []byte("test-secret-that-is-long-enough-for-hmac"),
	}, users
}

func postJSON(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func registerTestUser(t *testing.T, handler http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(t, handler, "/api/auth/register", `{
		"username":"PlayerOne",
		"email":"PlayerOne@example.com",
		"password":"secure-password-123",
		"confirmPassword":"secure-password-123"
	}`)
}

func TestRegisterCreatesMemberWithBcryptPassword(t *testing.T) {
	server, users := newAuthTestAPI()
	response := registerTestUser(t, server.routes())

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body)
	}
	var body struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode registration response: %v", err)
	}
	if body.User.Username != "playerone" || body.User.Email != "playerone@example.com" {
		t.Fatalf("user = %#v, want normalized username and email", body.User)
	}
	if body.User.Role != "member" {
		t.Fatalf("role = %q, want member", body.User.Role)
	}
	if body.User.PasswordHash != "" {
		t.Fatal("registration response must not expose the password hash")
	}
	stored, err := users.FindByLogin(context.Background(), "playerone")
	if err != nil {
		t.Fatalf("find created user: %v", err)
	}
	if stored.PasswordHash == "secure-password-123" {
		t.Fatal("password was stored as plain text")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("secure-password-123")); err != nil {
		t.Fatalf("stored hash does not match password: %v", err)
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	server, _ := newAuthTestAPI()
	if response := registerTestUser(t, server.routes()); response.Code != http.StatusCreated {
		t.Fatalf("initial registration status = %d, want %d", response.Code, http.StatusCreated)
	}
	response := postJSON(t, server.routes(), "/api/auth/register", `{
		"username":"PLAYERONE",
		"email":"another@example.com",
		"password":"secure-password-123",
		"confirmPassword":"secure-password-123"
	}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "username_taken") {
		t.Fatalf("status/body = %d %s, want username conflict", response.Code, response.Body)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	server, _ := newAuthTestAPI()
	if response := registerTestUser(t, server.routes()); response.Code != http.StatusCreated {
		t.Fatalf("initial registration status = %d, want %d", response.Code, http.StatusCreated)
	}
	response := postJSON(t, server.routes(), "/api/auth/register", `{
		"username":"anotherplayer",
		"email":"PLAYERONE@EXAMPLE.COM",
		"password":"secure-password-123",
		"confirmPassword":"secure-password-123"
	}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "email_taken") {
		t.Fatalf("status/body = %d %s, want email conflict", response.Code, response.Body)
	}
}

func TestRegisterRejectsMismatchedPasswords(t *testing.T) {
	server, users := newAuthTestAPI()
	response := postJSON(t, server.routes(), "/api/auth/register", `{
		"username":"playerone",
		"email":"playerone@example.com",
		"password":"secure-password-123",
		"confirmPassword":"different-password"
	}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "password_mismatch") {
		t.Fatalf("status/body = %d %s, want password mismatch", response.Code, response.Body)
	}
	if len(users.users) != 0 {
		t.Fatal("mismatched registration must not create an account")
	}
}

func TestRegisterValidatesUsernameEmailAndPassword(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "invalid username",
			body: `{"username":"no spaces","email":"player@example.com","password":"secure-password-123","confirmPassword":"secure-password-123"}`,
		},
		{
			name: "invalid email",
			body: `{"username":"playerone","email":"not-an-email","password":"secure-password-123","confirmPassword":"secure-password-123"}`,
		},
		{
			name: "short password",
			body: `{"username":"playerone","email":"player@example.com","password":"short","confirmPassword":"short"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, users := newAuthTestAPI()
			response := postJSON(t, server.routes(), "/api/auth/register", test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body)
			}
			if len(users.users) != 0 {
				t.Fatal("invalid registration must not create an account")
			}
		})
	}
}

func TestRegisterRejectsSelfAssignedAdminRole(t *testing.T) {
	server, users := newAuthTestAPI()
	response := postJSON(t, server.routes(), "/api/auth/register", `{
		"username":"playerone",
		"email":"playerone@example.com",
		"password":"secure-password-123",
		"confirmPassword":"secure-password-123",
		"role":"admin"
	}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if len(users.users) != 0 {
		t.Fatal("role escalation attempt must not create an account")
	}
}

func TestLoginAcceptsUsernameOrEmailAndSetsHttpOnlyCookie(t *testing.T) {
	server, _ := newAuthTestAPI()
	server.cookieSecure = true
	if response := registerTestUser(t, server.routes()); response.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, want %d", response.Code, http.StatusCreated)
	}

	for _, identifier := range []string{"PLAYERONE", "PLAYERONE@EXAMPLE.COM"} {
		response := postJSON(t, server.routes(), "/api/auth/login", `{
			"identifier":"`+identifier+`",
			"password":"secure-password-123"
		}`)
		if response.Code != http.StatusOK {
			t.Fatalf("login with %q status = %d, want %d: %s", identifier, response.Code, http.StatusOK, response.Body)
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != authCookieName || !cookies[0].HttpOnly || !cookies[0].Secure {
			t.Fatalf("login cookies = %#v, want a Secure, HttpOnly session cookie", cookies)
		}
		if cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("SameSite = %v, want Strict", cookies[0].SameSite)
		}
		if strings.Contains(response.Body.String(), cookies[0].Value) {
			t.Fatal("login response must not expose the JWT in its body")
		}
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	server, _ := newAuthTestAPI()
	if response := registerTestUser(t, server.routes()); response.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, want %d", response.Code, http.StatusCreated)
	}
	response := postJSON(t, server.routes(), "/api/auth/login", `{
		"identifier":"playerone",
		"password":"incorrect-password"
	}`)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "invalid_credentials") {
		t.Fatalf("status/body = %d %s, want invalid credentials", response.Code, response.Body)
	}
}

func TestMeValidatesTokenAndReloadsUser(t *testing.T) {
	server, users := newAuthTestAPI()
	if response := registerTestUser(t, server.routes()); response.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, want %d", response.Code, http.StatusCreated)
	}
	login := postJSON(t, server.routes(), "/api/auth/login", `{
		"identifier":"playerone",
		"password":"secure-password-123"
	}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d: %s", login.Code, http.StatusOK, login.Body)
	}
	cookie := login.Result().Cookies()[0]

	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"username":"playerone"`) {
		t.Fatalf("me status/body = %d %s, want authenticated user", response.Code, response.Body)
	}

	users.remove(1)
	response = httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("deleted account status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestMeRejectsMissingAndInvalidTokens(t *testing.T) {
	server, _ := newAuthTestAPI()
	handler := server.routes()
	for _, cookieValue := range []string{"", "not-a-jwt"} {
		request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		if cookieValue != "" {
			request.AddCookie(&http.Cookie{Name: authCookieName, Value: cookieValue})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("cookie %q status = %d, want %d", cookieValue, response.Code, http.StatusUnauthorized)
		}
	}
}

func TestLogoutClearsSessionCookie(t *testing.T) {
	server, _ := newAuthTestAPI()
	response := postJSON(t, server.routes(), "/api/auth/logout", "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v, want expired session cookie", cookies)
	}
}

func TestGuestCannotAccessProtectedMeEndpoint(t *testing.T) {
	server, _ := newAuthTestAPI()
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestUserNotFoundErrorIsStable(t *testing.T) {
	server, _ := newAuthTestAPI()
	_, err := server.users.FindByID(context.Background(), 1)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}
