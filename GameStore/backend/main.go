package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Game struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Genre       string    `json:"genre"`
	Price       float64   `json:"price"`
	ReleaseDate time.Time `json:"release_date"`
	ImageURL    string    `json:"image_url"`
}

type gameStore interface {
	ListGames(context.Context, string) ([]Game, error)
}

type databaseHealth interface {
	PingContext(context.Context) error
}

type postgresStore struct {
	db *sql.DB
}

func (store postgresStore) ListGames(ctx context.Context, query string) ([]Game, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, title, description, genre, price, release_date, image_url
		FROM games
		WHERE $1 = '' OR title ILIKE '%' || $1 || '%' OR genre ILIKE '%' || $1 || '%'
		ORDER BY title
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	games := make([]Game, 0)
	for rows.Next() {
		var game Game
		if err := rows.Scan(
			&game.ID,
			&game.Title,
			&game.Description,
			&game.Genre,
			&game.Price,
			&game.ReleaseDate,
			&game.ImageURL,
		); err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return games, nil
}

type api struct {
	games        gameStore
	health       databaseHealth
	users        userStore
	jwtSecret    []byte
	cookieSecure bool
}

func (server api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", server.getHealth)
	mux.HandleFunc("GET /api/games", server.getGames)
	server.registerAuthRoutes(mux)
	return mux
}

func (server api) getHealth(w http.ResponseWriter, r *http.Request) {
	if err := server.health.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (server api) getGames(w http.ResponseWriter, r *http.Request) {
	games, err := server.games.ListGames(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		log.Printf("list games: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "Could not load games.", "")
		return
	}
	writeJSON(w, http.StatusOK, games)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL must be set")
	}
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) < 32 {
		return errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	cookieSecure := strings.EqualFold(os.Getenv("APP_ENV"), "production")
	if configuredSecure := os.Getenv("AUTH_COOKIE_SECURE"); configuredSecure != "" {
		value, err := strconv.ParseBool(configuredSecure)
		if err != nil {
			return errors.New("AUTH_COOKIE_SECURE must be true or false")
		}
		if cookieSecure && !value {
			return errors.New("AUTH_COOKIE_SECURE cannot be false in production")
		}
		cookieSecure = value
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 10*time.Second)
	err = db.PingContext(pingCtx)
	cancelPing()
	if err != nil {
		return err
	}
	migrationCtx, cancelMigrations := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelMigrations()
	if err := runMigrations(migrationCtx, db); err != nil {
		return err
	}

	server := api{
		games:        postgresStore{db: db},
		health:       db,
		users:        postgresUserStore{db: db},
		jwtSecret:    jwtSecret,
		cookieSecure: cookieSecure,
	}
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	log.Printf("GameStore API listening on %s", address)
	return http.ListenAndServe(address, server.routes())
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
