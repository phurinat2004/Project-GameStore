package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const (
	authCookieName = "gamestore_session"
	tokenLifetime  = time.Hour
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrDuplicateUsername = errors.New("username already exists")
	ErrDuplicateEmail    = errors.New("email already exists")
	usernamePattern      = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type userStore interface {
	CreateUser(context.Context, string, string, string) (User, error)
	FindByLogin(context.Context, string) (User, error)
	FindByID(context.Context, int64) (User, error)
}

type postgresUserStore struct {
	db *sql.DB
}

func (store postgresUserStore) CreateUser(ctx context.Context, username, email, passwordHash string) (User, error) {
	var user User
	err := store.db.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash, role)
		VALUES ($1, $2, $3, 'member')
		RETURNING id, username, email, password_hash, role, created_at, updated_at
	`, username, email, passwordHash).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			switch postgresError.ConstraintName {
			case "users_username_unique", "users_username_lower_unique":
				return User{}, ErrDuplicateUsername
			case "users_email_unique", "users_email_lower_unique":
				return User{}, ErrDuplicateEmail
			}
		}
		return User{}, err
	}
	return user, nil
}

func (store postgresUserStore) FindByLogin(ctx context.Context, login string) (User, error) {
	var user User
	err := store.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, created_at, updated_at
		FROM users
		WHERE lower(username) = $1 OR lower(email) = $1
	`, login).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return user, err
}

func (store postgresUserStore) FindByID(ctx context.Context, id int64) (User, error) {
	var user User
	err := store.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, created_at, updated_at
		FROM users
		WHERE id = $1
	`, id).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.PasswordHash,
		&user.Role,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return user, err
}

type tokenClaims struct {
	jwt.RegisteredClaims
}

type authContextKey struct{}

func (server api) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", server.register)
	mux.HandleFunc("POST /api/auth/login", server.login)
	mux.HandleFunc("POST /api/auth/logout", server.logout)
	mux.Handle("GET /api/auth/me", server.requireAuth(http.HandlerFunc(server.me)))
}

type registerRequest struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
}

func (server api) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "Please provide valid JSON.", "")
		return
	}

	username := strings.ToLower(strings.TrimSpace(request.Username))
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !usernamePattern.MatchString(username) {
		writeAPIError(w, http.StatusBadRequest, "invalid_username", "Username must be 3–32 characters and contain only letters, numbers, and underscores.", "username")
		return
	}
	if !validEmail(email) {
		writeAPIError(w, http.StatusBadRequest, "invalid_email", "Enter a valid email address.", "email")
		return
	}
	if len(request.Password) < 8 || len(request.Password) > 72 {
		writeAPIError(w, http.StatusBadRequest, "invalid_password", "Password must be between 8 and 72 bytes.", "password")
		return
	}
	if request.Password != request.ConfirmPassword {
		writeAPIError(w, http.StatusBadRequest, "password_mismatch", "Passwords do not match.", "confirmPassword")
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash registration password: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Unable to create your account right now.", "")
		return
	}
	user, err := server.users.CreateUser(r.Context(), username, email, string(passwordHash))
	if errors.Is(err, ErrDuplicateUsername) {
		writeAPIError(w, http.StatusConflict, "username_taken", "That username is already in use.", "username")
		return
	}
	if errors.Is(err, ErrDuplicateEmail) {
		writeAPIError(w, http.StatusConflict, "email_taken", "That email address is already in use.", "email")
		return
	}
	if err != nil {
		log.Printf("create user: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Unable to create your account right now.", "")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]User{"user": user})
}

func validEmail(email string) bool {
	if len(email) > 254 {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && strings.Contains(email, "@")
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

func (server api) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil || strings.TrimSpace(request.Identifier) == "" || request.Password == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "Enter your username or email and password.", "")
		return
	}

	user, err := server.users.FindByLogin(r.Context(), strings.ToLower(strings.TrimSpace(request.Identifier)))
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		log.Printf("find login account: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Unable to sign in right now.", "")
		return
	}
	if errors.Is(err, ErrUserNotFound) || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)) != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "Username/email or password is incorrect.", "")
		return
	}

	token, expiresAt, err := server.newToken(user.ID)
	if err != nil {
		log.Printf("create login token: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Unable to sign in right now.", "")
		return
	}
	server.setSessionCookie(w, token, expiresAt)
	writeJSON(w, http.StatusOK, map[string]User{"user": user})
}

func (server api) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   server.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (server api) me(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(authContextKey{}).(User)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]User{"user": user})
}

func (server api) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(authCookieName)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "Sign in to continue.", "")
			return
		}
		claims := &tokenClaims{}
		token, err := jwt.ParseWithClaims(
			cookie.Value,
			claims,
			func(token *jwt.Token) (any, error) {
				if token.Method != jwt.SigningMethodHS256 {
					return nil, fmt.Errorf("unexpected signing method %q", token.Method.Alg())
				}
				return server.jwtSecret, nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer("gamestore"),
			jwt.WithExpirationRequired(),
			jwt.WithTimeFunc(time.Now),
		)
		if err != nil || !token.Valid {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "Your session is invalid or expired. Please sign in again.", "")
			return
		}

		userID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || userID < 1 {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "Your session is invalid or expired. Please sign in again.", "")
			return
		}
		user, err := server.users.FindByID(r.Context(), userID)
		if errors.Is(err, ErrUserNotFound) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "Your account is no longer available.", "")
			return
		}
		if err != nil {
			log.Printf("load authenticated user: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "Unable to verify your session right now.", "")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, user)))
	})
}

func (server api) newToken(userID int64) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(tokenLifetime)
	claims := tokenClaims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer:    "gamestore",
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	value, err := token.SignedString(server.jwtSecret)
	return value, expiresAt, err
}

func (server api) setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   server.cookieSecure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt,
		MaxAge:   int(math.Ceil(time.Until(expiresAt).Seconds())),
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}

func writeAPIError(w http.ResponseWriter, status int, code, message, field string) {
	detail := map[string]string{"code": code, "message": message}
	if field != "" {
		detail["field"] = field
	}
	writeJSON(w, status, map[string]any{"error": detail})
}
